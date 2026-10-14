package device

import (
	"fmt"
	"github.com/gopcua/opcua/ua"
	"regexp"
	"strings"
)

type DataType string
type Address struct {
	NodeID string   `json:"node_id"`
	Type   DataType `json:"type"`
	Count  int      `json:"count"`
}

var Types = []string{"Auto", "Boolean", "SByte", "Byte", "Int16", "UInt16", "Int32", "UInt32", "Int64", "UInt64", "Float", "Double", "String", "DateTime", "ByteString"}

func NormalizeType(kind DataType) DataType {
	for _, t := range Types {
		if strings.EqualFold(string(kind), t) {
			return DataType(t)
		}
	}
	return kind
}
func ParseAddress(text string, kind DataType, count int) (Address, error) {
	a := Address{NodeID: strings.TrimSpace(text), Type: NormalizeType(kind), Count: count}
	return a, a.Validate()
}
func (a Address) Validate() error {
	if a.NodeID == "" {
		return fmt.Errorf("请输入 NodeID，例如 ns=2;s=Counter")
	}
	if !regexp.MustCompile(`^(?:ns=[0-9]+;)?[isgb]=`).MatchString(a.NodeID) {
		return fmt.Errorf("NodeID 须明确指定 i=、s=、g= 或 b= 标识")
	}
	if _, err := ua.ParseNodeID(a.NodeID); err != nil {
		return fmt.Errorf("无效 NodeID %q: %w", a.NodeID, err)
	}
	if a.Count < 1 || a.Count > 65535 {
		return fmt.Errorf("元素数量须为 1..65535")
	}
	for _, t := range Types {
		if string(a.Type) == t {
			return nil
		}
	}
	return fmt.Errorf("不支持的 OPC UA 类型 %q", a.Type)
}
func (a Address) String() string { return a.NodeID }
