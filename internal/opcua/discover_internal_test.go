package opcua

import (
	"net/netip"
	"strings"
	"testing"
)

// Hosts on an adapter's network dial from that adapter; only hosts that
// answered or whose ARP answered are tried on the other ports.
func TestOnlyLiveHostsGetTheOtherPorts(t *testing.T) {
	wlan := link{name: "WLAN", addr: netip.MustParseAddr("10.3.150.143"), prefix: netip.MustParsePrefix("10.3.150.0/23")}
	tun := link{name: "neko-tun", addr: netip.MustParseAddr("172.19.0.1"), prefix: netip.MustParsePrefix("172.19.0.0/28"), virtual: true}
	hosts := []string{"10.3.150.1", "10.3.150.2", "10.3.151.9", "172.19.0.5", "192.168.1.5", "plc-01"}
	local := sources(hosts, []link{tun, wlan})
	if len(local) != 4 || local["10.3.151.9"].addr != wlan.addr || local["172.19.0.5"].addr != tun.addr {
		t.Fatalf("sources: %v", local)
	}
	table := map[netip.Addr]bool{netip.MustParseAddr("10.3.150.1"): true}
	live := liveHosts(hosts, map[string]bool{"10.3.151.9": true}, local, table)
	// 10.3.150.2 neither answered nor ARP; the virtual, remote and named
	// hosts have no ARP to judge them by.
	if want := "10.3.150.1,10.3.151.9,172.19.0.5,192.168.1.5,plc-01"; strings.Join(live, ",") != want {
		t.Fatalf("live: %v", live)
	}
	if got := liveHosts(hosts, nil, local, nil); len(got) != len(hosts) {
		t.Fatal("an unreadable neighbor table must keep every host")
	}
	t.Logf("neighbor table: %d resolved IPv4 addresses", len(neighborTable()))
}
