package device

import (
	"context"
	"github.com/gopcua/opcua/ua"
	"time"
)

type Protocol string

const ProtocolOPCUA Protocol = "OPC_UA"

// These identifiers preserve the recorder's ability to read historical databases.
const (
	ProtocolModbusTCP    Protocol = "MODBUS_TCP"
	ProtocolRTUOverTCP   Protocol = "RTU_OVER_TCP"
	ProtocolASCIIOverTCP Protocol = "ASCII_OVER_TCP"
	ProtocolModbusRTU    Protocol = "MODBUS_RTU"
	ProtocolModbusASCII  Protocol = "MODBUS_ASCII"
)

type ReadResult struct {
	Address     Address   `json:"address"`
	Value       Value     `json:"value"`
	Quality     string    `json:"quality"`
	StatusCode  uint32    `json:"status_code"`
	Time        time.Time `json:"time"`
	SourceTime  time.Time `json:"source_time,omitempty"`
	ServerTime  time.Time `json:"server_time,omitempty"`
	Error       string    `json:"error,omitempty"`
	Execution   string    `json:"execution,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	Complete    bool      `json:"scope_complete"`
	Path        []string  `json:"path,omitempty"` // browse path below the read root, when known
	NodeAttributes
}

// NodeAttributes describe a variable beyond its value: the declared type and
// this session's access, READ, READ_WRITE, WRITE or NONE; "" when unknown.
type NodeAttributes struct {
	DataType DataType `json:"data_type,omitempty"`
	Access   string   `json:"access,omitempty"`
}

// Writable reports whether the session may write the variable, a control point.
func (a NodeAttributes) Writable() bool { return a.Access == "READ_WRITE" || a.Access == "WRITE" }

// Missing reports whether the server says the node does not exist.
func (r ReadResult) Missing() bool {
	return r.StatusCode == uint32(ua.StatusBadNodeIDUnknown) || r.StatusCode == uint32(ua.StatusBadNodeIDInvalid)
}

// Kind is the point's OPC UA type: the value read, else the declared type,
// else the requested one; "" when none is known.
func (r ReadResult) Kind() DataType {
	switch {
	case r.Value.Type != "" && r.Value.Type != "Null":
		return r.Value.Type
	case r.DataType != "":
		return r.DataType
	case r.Address.Type != "Auto":
		return r.Address.Type
	}
	return ""
}

type WriteSegment struct {
	Address Address `json:"address"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
}
type WriteResult struct {
	Address    Address        `json:"address"`
	State      string         `json:"state"`
	StatusCode uint32         `json:"status_code"`
	Error      string         `json:"error,omitempty"`
	Segments   []WriteSegment `json:"segments,omitempty"`
}
type Node struct {
	NodeID      string `json:"node_id"`
	BrowseName  string `json:"browse_name"`
	DisplayName string `json:"display_name"`
	Class       string `json:"class"`
}
type Driver interface {
	Read(context.Context, []Address) ([]ReadResult, error)
	Write(context.Context, Address, Value) (WriteResult, error)
	Browse(context.Context, string) ([]Node, error)
	Close() error
}
