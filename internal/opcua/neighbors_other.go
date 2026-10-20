//go:build !windows

package opcua

import "net/netip"

// neighborTable is only read on Windows; without it every host is tried on
// every port.
var neighborTable = func() map[netip.Addr]bool { return nil }
