package opcua

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
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
	ScanWorkers    = 256
	probeWorkers   = 32
	ConnectTimeout = 500 * time.Millisecond
	// DialRate paces connection attempts, so a large search does not flood
	// the network stack, a proxy or the endpoint security software.
	DialRate = 800
)

// Found is an OPC UA server found by Discover.
type Found struct {
	URL       string // the address that answered, or the URL a discovery server registered
	Name      string // application name
	URI       string // application URI
	Endpoints []Endpoint
	Error     string // why the endpoints of a registered server could not be read
}

// Network is an IPv4 network of this computer to search.
type Network struct {
	Interface string
	Prefix    string // such as 10.3.150.0/23, or this computer's address alone
	Hosts     int
	Primary   bool // the network of the default route
	Virtual   bool // a VPN, proxy tunnel or virtual machine adapter
}

// virtualNames mark adapters whose addresses lead into a VPN, a proxy tunnel
// or a virtual machine: scanning through them floods that software.
var virtualNames = []string{"tun", "tap", "vpn", "wireguard", "zerotier", "tailscale", "oray", "vethernet", "vmware", "virtualbox", "hyper-v", "wsl", "docker", "loopback", "bluetooth", "蓝牙"}

// maxLocalBits bounds a local network to /22, 1022 hosts; a larger one is
// searched as the /24 around this computer.
const maxLocalBits = 22

// LocalNetworks lists this computer's IPv4 networks with their real masks,
// the default route's first; link-local addresses and /31 or /32 routes,
// which VPNs use, are left out.
func LocalNetworks() []Network {
	primary := ""
	// No packet is sent: the system only picks the route to the address.
	if c, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		primary = c.LocalAddr().(*net.UDPAddr).IP.String()
		c.Close()
	}
	var out []Network
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(i.Name)
		virtual := slices.ContainsFunc(virtualNames, func(v string) bool { return strings.Contains(name, v) })
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLinkLocalUnicast() {
				continue
			}
			ip, _ := netip.AddrFromSlice(n.IP.To4())
			bits, _ := n.Mask.Size()
			if bits > 30 {
				continue
			}
			if bits < maxLocalBits {
				bits = 24
			}
			p := netip.PrefixFrom(ip, bits).Masked()
			out = append(out, Network{Interface: i.Name, Prefix: p.String(), Hosts: 1<<(32-bits) - 2, Primary: ip.String() == primary && !virtual, Virtual: virtual})
		}
	}
	// The default route's network first, then real adapters, then virtual ones.
	slices.SortStableFunc(out, func(a, b Network) int {
		rank := func(n Network) int {
			switch {
			case n.Primary:
				return 0
			case !n.Virtual:
				return 1
			}
			return 2
		}
		return rank(a) - rank(b)
	})
	return out
}

// DefaultHosts is the network a search starts with: the default route's, else
// the first real adapter's, else this computer alone.
func DefaultHosts(nets []Network) string {
	for _, n := range nets {
		if !n.Virtual {
			return n.Prefix
		}
	}
	return "127.0.0.1"
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
// asks each open port for its endpoints while the scan goes on, and asks each
// server found for the servers registered with it, as a local discovery
// server lists them. progress reports the addresses tried out of the total;
// found receives each server as soon as it is known.
func Discover(ctx context.Context, hosts []string, ports []int, timeout time.Duration, progress func(done, total int), found func(Found)) ([]Found, error) {
	if len(hosts)*len(ports) > maxScanTargets {
		return nil, fmt.Errorf("主机数 × 端口数超过 %d，请缩小网段或减少端口", maxScanTargets)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("超时须大于零")
	}
	// A closed or filtered port should not hold a worker for the full timeout;
	// a LAN host answers a connect within milliseconds.
	connect := min(timeout, ConnectTimeout)
	var mu, pmu sync.Mutex
	var servers []Found
	seen := map[string]bool{}
	done, total := 0, len(hosts)*len(ports)
	// Every scanned address is kept: PLCs of one model often share an
	// application URI. A registered server is dropped when its URI was
	// found already, as a discovery server lists itself under its host name.
	publish := func(f Found, registered bool) {
		mu.Lock()
		defer mu.Unlock()
		if registered && f.URI != "" && slices.ContainsFunc(servers, func(g Found) bool { return g.URI == f.URI }) {
			return
		}
		servers = append(servers, f)
		if found != nil {
			found(f)
		}
	}
	// Endpoints are read while the scan goes on, so servers show up at once.
	opened := make(chan string, ScanWorkers)
	var registered []string
	var probes sync.WaitGroup
	for range probeWorkers {
		probes.Go(func() {
			for u := range opened {
				f := describe(ctx, u, timeout)
				if f.Error != "" {
					continue
				}
				publish(f, false)
				// Servers registered with a discovery server may listen elsewhere.
				apps, err := findServers(ctx, u, timeout)
				if err != nil {
					continue
				}
				mu.Lock()
				for _, app := range apps {
					for _, d := range app.DiscoveryURLs {
						if ValidateEndpoint(d) == nil && !seen[d] {
							seen[d] = true
							registered = append(registered, d)
						}
					}
				}
				mu.Unlock()
			}
		})
	}
	targets := make(chan string)
	var scans sync.WaitGroup
	for range min(ScanWorkers, total) {
		scans.Go(func() {
			dialer := net.Dialer{Timeout: connect}
			for addr := range targets {
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err == nil {
					conn.Close()
					u := "opc.tcp://" + addr
					mu.Lock()
					seen[u] = true
					mu.Unlock()
					opened <- u
				}
				// Progress has its own lock: a slow callback must not hold up
				// the servers being recorded.
				pmu.Lock()
				done++
				if progress != nil {
					progress(done, total)
				}
				pmu.Unlock()
			}
		})
	}
	// Port by port, so the usual 4840 servers on every host come first.
	pace := time.NewTicker(time.Second / DialRate)
	defer pace.Stop()
feed:
	for _, p := range ports {
		for _, h := range hosts {
			select {
			case <-pace.C:
			case <-ctx.Done():
				break feed
			}
			select {
			case targets <- net.JoinHostPort(h, strconv.Itoa(p)):
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(targets)
	scans.Wait()
	close(opened)
	probes.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Registered servers are listed even when their endpoints cannot be read.
	jobs := make(chan string)
	var late sync.WaitGroup
	for range min(probeWorkers, len(registered)) {
		late.Go(func() {
			for u := range jobs {
				publish(describe(ctx, u, timeout), true)
			}
		})
	}
	for _, u := range registered {
		jobs <- u
	}
	close(jobs)
	late.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(servers, func(a, b Found) int { return CompareURL(a.URL, b.URL) })
	return servers, nil
}

// CompareURL orders endpoints by IP address and port numerically, so
// 192.168.1.9 comes before 192.168.1.10; host names follow addresses.
func CompareURL(a, b string) int {
	key := func(s string) (netip.Addr, int, string) {
		u, err := url.Parse(s)
		if err != nil {
			return netip.Addr{}, 0, s
		}
		port, _ := strconv.Atoi(u.Port())
		ip, _ := netip.ParseAddr(u.Hostname())
		return ip, port, u.Hostname()
	}
	ipA, portA, hostA := key(a)
	ipB, portB, hostB := key(b)
	switch {
	case ipA.IsValid() != ipB.IsValid():
		if ipA.IsValid() {
			return -1
		}
		return 1
	case ipA != ipB:
		if ipA.IsValid() {
			return ipA.Compare(ipB)
		}
		return strings.Compare(hostA, hostB)
	}
	if c := strings.Compare(hostA, hostB); c != 0 {
		return c
	}
	return portA - portB
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
