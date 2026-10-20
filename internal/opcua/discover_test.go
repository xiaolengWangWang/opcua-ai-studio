package opcua_test

import (
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/simulator"
)

func TestParseHostsAndPorts(t *testing.T) {
	hosts, err := opcua.ParseHosts("192.168.1.0/30, plc-01；10.0.0.250-252 192.168.2.1-192.168.2.2")
	if want := "192.168.1.1,192.168.1.2,plc-01,10.0.0.250,10.0.0.251,10.0.0.252,192.168.2.1,192.168.2.2"; err != nil || strings.Join(hosts, ",") != want {
		t.Fatalf("%v %v", hosts, err)
	}
	if hosts, err = opcua.ParseHosts("192.168.1.0/24"); err != nil || len(hosts) != 254 || hosts[253] != "192.168.1.254" {
		t.Fatalf("/24: %d %v", len(hosts), err)
	}
	for _, bad := range []string{"", "10.0.0.0/16", "10.0.0.9-3", "opc.tcp://host:4840", "10.0.0.1-x"} {
		if _, err := opcua.ParseHosts(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	ports, err := opcua.ParsePorts(opcua.DefaultPorts + ", 4840-4842")
	if err != nil || len(ports) != 9 || ports[0] != 4840 || ports[len(ports)-1] != 4842 {
		t.Fatalf("%v %v", ports, err)
	}
	for _, bad := range []string{"", "0", "70000", "4850-4840", "opc"} {
		if _, err := opcua.ParsePorts(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	for _, n := range opcua.LocalNetworks() {
		if _, err := opcua.ParseHosts(n.Prefix); err != nil || n.Hosts < 2 || n.Hosts > 1022 {
			t.Fatalf("local network %+v: %v", n, err)
		}
	}
	if opcua.DefaultHosts(nil) != "127.0.0.1" || opcua.DefaultHosts([]opcua.Network{{Prefix: "172.19.0.0/28", Virtual: true}, {Prefix: "10.3.150.0/23"}}) != "10.3.150.0/23" {
		t.Fatal("default hosts must skip virtual adapters")
	}
}

func TestDiscoverFindsTheDemoServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.Endpoint, "opc.tcp://"))
	// A plain TCP service on another port must not be listed.
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	go func() {
		for {
			c, err := other.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	p, _ := strconv.Atoi(port)
	q := other.Addr().(*net.TCPAddr).Port
	calls, streamed, started := 0, 0, time.Now()
	found, err := opcua.Discover(ctx, []string{"127.0.0.1"}, []int{p, q}, 3*time.Second, func(done, total int) { calls++ }, func(opcua.Found) { streamed++ })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovery took %v", time.Since(started))
	if len(found) != 1 || found[0].URL != srv.Endpoint || found[0].Name == "" || len(found[0].Endpoints) == 0 || found[0].Endpoints[0].Policy != "None" {
		t.Fatalf("found: %+v", found)
	}
	if calls != 2 || streamed != 1 {
		t.Fatalf("progress reported %d times, %d servers streamed", calls, streamed)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := opcua.Discover(cancelled, []string{"127.0.0.1"}, []int{p}, time.Second, nil, nil); err == nil {
		t.Fatal("cancelled search returned results")
	}
}

func TestDiscoveryResultsSortByAddress(t *testing.T) {
	urls := []string{"opc.tcp://plc-02:4840", "opc.tcp://192.168.1.10:4840", "opc.tcp://192.168.1.9:49320", "opc.tcp://192.168.1.9:4840"}
	slices.SortFunc(urls, opcua.CompareURL)
	if want := "opc.tcp://192.168.1.9:4840,opc.tcp://192.168.1.9:49320,opc.tcp://192.168.1.10:4840,opc.tcp://plc-02:4840"; strings.Join(urls, ",") != want {
		t.Fatalf("%v", urls)
	}
}

// A search of a thousand hosts must finish, paced, with its progress complete;
// loopback addresses keep it off the network and away from any proxy.
func TestLargeSearchIsPacedAndFinishes(t *testing.T) {
	hosts, err := opcua.ParseHosts("127.0.4.0/22")
	if err != nil || len(hosts) != 1022 {
		t.Fatalf("%d hosts, %v", len(hosts), err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	last, started := 0, time.Now()
	found, err := opcua.Discover(ctx, hosts, []int{port}, 2*time.Second, func(done, total int) { last = done }, nil)
	elapsed := time.Since(started)
	if err != nil || len(found) != 0 || last != len(hosts) {
		t.Fatalf("found %v, progress %d of %d, %v", found, last, len(hosts), err)
	}
	if paced := time.Duration(len(hosts)) * time.Second / opcua.DialRate; elapsed < paced*9/10 || elapsed > 20*time.Second {
		t.Fatalf("%d attempts took %v, paced at %v", len(hosts), elapsed, paced)
	}
}
