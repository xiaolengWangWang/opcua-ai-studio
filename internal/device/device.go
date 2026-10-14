package device

import (
	"context"
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
