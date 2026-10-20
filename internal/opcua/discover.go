package opcua

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// DefaultPorts are the usual OPC UA ports: the standard 4840 and 4841, WinCC
// 4862, WinCC Unified 4890, the Unified Automation demo 48010, KEPServerEX
// 49320, Prosys 53530 and the OPC Foundation reference and Ignition 62541.
const DefaultPorts = "4840, 4841, 4862, 4890, 48010, 49320, 53530, 62541"

// Discovery limits keep a scan within a few seconds and a /20 network.
const (
	maxScanHosts   = 4096
	maxScanTargets = 40000
	scanWorkers    = 256
	probeWorkers   = 16
)

// Found is an OPC UA server found by Discover.
type Found struct {
	URL       string // the address that answered, or the URL a discovery server registered
	Name      string // application name
	URI       string // application URI
	Endpoints []Endpoint
	Error     string // why the endpoints of a registered server could not be read
}

// LocalNetworks lists this computer, then the /24 network of each active IPv4
// interface; a larger network would take long to scan.
func LocalNetworks() []string {
	out := []string{"127.0.0.1"}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLinkLocalUnicast() {
				continue
			}
			p, _ := netip.AddrFromSlice(n.IP.To4())
			if s := netip.PrefixFrom(p, 24).Masked().String(); !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

func splitList(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune(" ,，;；\t\r\n", r) })
}

// ParseHosts reads hosts separated by spaces or commas: names, addresses,
// networks such as 192.168.1.0/24, and ranges such as 192.168.1.10-50.
func ParseHosts(text string) ([]string, error) {
	var out []string
	add := func(s string) error {
		if len(out) >= maxScanHosts {
			return fmt.Errorf("最多搜索 %d 台主机，请缩小网段", maxScanHosts)
		}
		out = append(out, s)
		return nil
	}
	for _, item := range splitList(text) {
		if p, err := netip.ParsePrefix(item); err == nil {
			if !p.Addr().Is4() || p.Bits() < 20 {
				return nil, fmt.Errorf("网段 %s 过大：仅支持 IPv4 /20 及更小的网段", item)
			}
			p = p.Masked()
			first, last := p.Addr(), p.Addr()
			for a := p.Addr(); p.Contains(a); a = a.Next() {
				last = a
			}
			// The network and broadcast addresses are not hosts.
			if p.Bits() <= 30 {
				first, last = first.Next(), last.Prev()
			}
			for a := first; a.Compare(last) <= 0; a = a.Next() {
				if err := add(a.String()); err != nil {
					return nil, err
				}
			}
			continue
		}
		// A host name such as plc-01 is no range.
		if from, to, ok := strings.Cut(item, "-"); ok && isIPv4(from) {
			start, _ := netip.ParseAddr(from)
			end, err := netip.ParseAddr(to)
			if err != nil {
				b := start.As4()
				n, e := strconv.ParseUint(to, 10, 8)
				if e != nil {
					return nil, fmt.Errorf("无效地址范围 %q，例如 192.168.1.10-50", item)
				}
				b[3] = byte(n)
				end = netip.AddrFrom4(b)
			}
			if !end.Is4() || end.Less(start) {
				return nil, fmt.Errorf("无效地址范围 %q", item)
			}
			for a := start; a.Compare(end) <= 0; a = a.Next() {
				if err := add(a.String()); err != nil {
					return nil, err
				}
			}
			continue
		}
		if strings.ContainsAny(item, "/:") {
			return nil, fmt.Errorf("无效主机 %q：请填写主机名、IPv4 地址、网段或范围", item)
		}
		if err := add(item); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请填写要搜索的主机或网段")
	}
	return out, nil
}

func isIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// ParsePorts reads ports separated by spaces or commas, and ranges such as 4840-4850.
func ParsePorts(text string) ([]int, error) {
	var out []int
	for _, item := range splitList(text) {
		from, to, ranged := strings.Cut(item, "-")
		if !ranged {
			to = from
		}
		a, err1 := strconv.Atoi(from)
		b, err2 := strconv.Atoi(to)
		if err1 != nil || err2 != nil || a < 1 || b > 65535 || b < a {
			return nil, fmt.Errorf("无效端口 %q", item)
		}
		for p := a; p <= b; p++ {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请填写要搜索的端口")
	}
	return out, nil
}

// Discover searches hosts for OPC UA servers: it connects to every port,
// asks each open port for its endpoints, and asks each server found for the
// servers registered with it, as a local discovery server lists them.
// progress reports the addresses tried out of the total.
func Discover(ctx context.Context, hosts []string, ports []int, timeout time.Duration, progress func(done, total int)) ([]Found, error) {
	if len(hosts)*len(ports) > maxScanTargets {
		return nil, fmt.Errorf("主机数 × 端口数超过 %d，请缩小网段或减少端口", maxScanTargets)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("超时须大于零")
	}
	// A closed or filtered port should not hold a worker for the full timeout.
	connect := min(timeout, 700*time.Millisecond)
	targets := make(chan string)
	var mu sync.Mutex
	var open []string
	done, total := 0, len(hosts)*len(ports)
	var wg sync.WaitGroup
	for range min(scanWorkers, total) {
		wg.Go(func() {
			dialer := net.Dialer{Timeout: connect}
			for addr := range targets {
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err == nil {
					conn.Close()
				}
				mu.Lock()
				if err == nil {
					open = append(open, addr)
				}
				done++
				if progress != nil {
					progress(done, total)
				}
				mu.Unlock()
			}
		})
	}
feed:
	for _, h := range hosts {
		for _, p := range ports {
			select {
			case targets <- net.JoinHostPort(h, strconv.Itoa(p)):
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(targets)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found []Found
	seen := map[string]bool{}
	probe := func(urls []string, registered bool) {
		jobs := make(chan string)
		var wg sync.WaitGroup
		for range min(probeWorkers, len(urls)) {
			wg.Go(func() {
				for u := range jobs {
					f := describe(ctx, u, timeout)
					mu.Lock()
					// A discovery server also lists itself, under its host name.
					known := registered && f.URI != "" && slices.ContainsFunc(found, func(g Found) bool { return g.URI == f.URI })
					if (f.Error == "" || registered) && !known {
						found = append(found, f)
					}
					mu.Unlock()
				}
			})
		}
		for _, u := range urls {
			jobs <- u
		}
		close(jobs)
		wg.Wait()
	}
	var urls []string
	for _, addr := range open {
		urls = append(urls, "opc.tcp://"+addr)
		seen["opc.tcp://"+addr] = true
	}
	probe(urls, false)
	// Servers registered with a discovery server may listen on other ports.
	var registered []string
	for _, f := range slices.Clone(found) {
		apps, err := findServers(ctx, f.URL, timeout)
		if err != nil {
			continue
		}
		for _, app := range apps {
			for _, u := range app.DiscoveryURLs {
				if ValidateEndpoint(u) == nil && !seen[u] {
					seen[u] = true
					registered = append(registered, u)
				}
			}
		}
	}
	probe(registered, true)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b Found) int { return strings.Compare(a.URL, b.URL) })
	return found, nil
}

func findServers(ctx context.Context, target string, timeout time.Duration) ([]*ua.ApplicationDescription, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return sdk.FindServers(ctx, target, sdk.DialTimeout(timeout), sdk.RequestTimeout(timeout))
}

// describe reads a server's endpoints; a port that is not OPC UA fails.
func describe(ctx context.Context, target string, timeout time.Duration) Found {
	f := Found{URL: target}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ee, err := sdk.GetEndpoints(ctx, target, sdk.DialTimeout(timeout), sdk.RequestTimeout(timeout))
	if err != nil {
		f.Error = err.Error()
		return f
	}
	for _, e := range ee {
		if f.Name == "" && e.Server != nil {
			f.URI = e.Server.ApplicationURI
			if e.Server.ApplicationName != nil {
				f.Name = e.Server.ApplicationName.Text
			}
		}
		v := Endpoint{URL: e.EndpointURL, Policy: strings.TrimPrefix(e.SecurityPolicyURI, ua.SecurityPolicyURIPrefix), Mode: modeName(e.SecurityMode)}
		for _, a := range e.UserIdentityTokens {
			v.Authentication = append(v.Authentication, a.TokenType.String())
		}
		f.Endpoints = append(f.Endpoints, v)
	}
	return f
}
