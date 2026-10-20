package opcua

import (
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi       = windows.NewLazySystemDLL("iphlpapi.dll")
	getIPNetTable2 = iphlpapi.NewProc("GetIpNetTable2")
	freeMibTable   = iphlpapi.NewProc("FreeMibTable")
)

// mibIPNetRow2 is MIB_IPNET_ROW2 from netioapi.h, 88 bytes.
type mibIPNetRow2 struct {
	address        [28]byte // SOCKADDR_INET; an IPv4 address is at bytes 4–7
	interfaceIndex uint32
	interfaceLUID  uint64
	physical       [32]byte
	physicalLength uint32
	state          int32 // NL_NEIGHBOR_STATE: 0 unreachable, 1 incomplete, 2 probe … 6 permanent
	flags          uint8
	_              [3]byte
	reachability   uint32
}

var _ [88]byte = [unsafe.Sizeof(mibIPNetRow2{})]byte{}

// neighborTable lists the IPv4 addresses whose ARP this computer has answered;
// nil when the table cannot be read.
var neighborTable = func() map[netip.Addr]bool {
	var table *uint32 // MIB_IPNET_TABLE2: the count, then the rows at offset 8
	if r, _, _ := getIPNetTable2.Call(windows.AF_INET, uintptr(unsafe.Pointer(&table))); r != 0 || table == nil {
		return nil
	}
	defer freeMibTable.Call(uintptr(unsafe.Pointer(table)))
	rows := unsafe.Slice((*mibIPNetRow2)(unsafe.Add(unsafe.Pointer(table), 8)), *table)
	out := make(map[netip.Addr]bool, len(rows))
	for _, r := range rows {
		if r.state >= 2 && r.physicalLength > 0 {
			out[netip.AddrFrom4([4]byte(r.address[4:8]))] = true
		}
	}
	return out
}
