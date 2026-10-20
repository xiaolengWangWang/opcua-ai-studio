package opcua_test

import (
	"context"
	"net"
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
	if nets := opcua.LocalNetworks(); len(nets) == 0 || nets[0] != "127.0.0.1" {
		t.Fatalf("local networks: %v", nets)
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
	calls, started := 0, time.Now()
	found, err := opcua.Discover(ctx, []string{"127.0.0.1"}, []int{p, q}, 3*time.Second, func(done, total int) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovery took %v", time.Since(started))
	if len(found) != 1 || found[0].URL != srv.Endpoint || found[0].Name == "" || len(found[0].Endpoints) == 0 || found[0].Endpoints[0].Policy != "None" {
		t.Fatalf("found: %+v", found)
	}
	if calls != 2 {
		t.Fatalf("progress reported %d times", calls)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := opcua.Discover(cancelled, []string{"127.0.0.1"}, []int{p}, time.Second, nil); err == nil {
		t.Fatal("cancelled search returned results")
	}
}
