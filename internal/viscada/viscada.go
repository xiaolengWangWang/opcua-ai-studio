// Package viscada converts OPC UA points to viSCADA device-class attributes,
// identified as namespace::identifierType::identifier::dataType, and reads and
// writes the platform's attribute workbook.
package viscada

import (
	"errors"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"opcua-ai-studio/internal/device"
	"strconv"
	"strings"
)

// Attribute is one row of the attribute sheet.
type Attribute struct {
	Row        int    // sheet row, from 1
	Identifier string // 属性标识
	Name       string // 属性名称
	Access     string // 读写模式: 1 只读, 2 读写, "" unknown
}

// The platform's types by OPC UA type, preferring the numeric IEC names over
// the bit-string ones (UINT over WORD).
var platformTypes = map[device.DataType]string{"Boolean": "BOOL", "SByte": "SINT", "Byte": "USINT", "Int16": "INT", "UInt16": "UINT", "Int32": "DINT", "UInt32": "UDINT", "Int64": "LINT", "UInt64": "ULINT", "Float": "REAL", "Double": "LREAL", "String": "STRING"}

// CHAR and WCHAR have no single OPC UA type, so they read as Auto.
var opcTypes = map[string]device.DataType{"BOOL": "Boolean", "SINT": "SByte", "USINT": "Byte", "BYTE": "Byte", "INT": "Int16", "UINT": "UInt16", "WORD": "UInt16", "DINT": "Int32", "UDINT": "UInt32", "DWORD": "UInt32", "LINT": "Int64", "ULINT": "UInt64", "LWORD": "UInt64", "REAL": "Float", "LREAL": "Double", "STRING": "String", "WSTRING": "String", "CHAR": "Auto", "WCHAR": "Auto"}

// Identifier formats a NodeID and its OPC UA type as an attribute identifier.
func Identifier(nodeID string, kind device.DataType) (string, error) {
	platform, ok := platformTypes[kind]
	if !ok {
		if kind == "" {
			return "", errors.New("数据类型未知")
		}
		return "", fmt.Errorf("viSCADA 不支持 OPC UA 类型 %s", kind)
	}
	n, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return "", fmt.Errorf("无效 NodeID %q: %w", nodeID, err)
	}
	kindCode, id := "i", strconv.FormatUint(uint64(n.IntID()), 10)
	switch n.Type() {
	case ua.NodeIDTypeString:
		kindCode, id = "s", n.StringID()
	case ua.NodeIDTypeGUID:
		kindCode, id = "g", n.StringID()
	case ua.NodeIDTypeByteString:
		kindCode, id = "b", n.StringID()
	}
	return fmt.Sprintf("%d::%s::%s::%s", n.Namespace(), kindCode, id, platform), nil
}

// ParseIdentifier returns the address of an attribute identifier. The
// identifier itself may contain "::", so the type is the last segment.
func ParseIdentifier(text string) (device.Address, error) {
	text = strings.TrimSpace(text)
	parts := strings.SplitN(text, "::", 3)
	cut := -1
	if len(parts) == 3 {
		cut = strings.LastIndex(parts[2], "::")
	}
	if cut < 0 {
		return device.Address{}, fmt.Errorf("属性标识 %q 须为 namespace::类型::标识::数据类型", text)
	}
	ns, kindCode, id, platform := parts[0], parts[1], parts[2][:cut], strings.ToUpper(parts[2][cut+2:])
	if _, err := strconv.ParseUint(ns, 10, 16); err != nil {
		return device.Address{}, fmt.Errorf("属性标识 %q 的命名空间须为数字", text)
	}
	if kindCode != "i" && kindCode != "s" && kindCode != "g" && kindCode != "b" {
		return device.Address{}, fmt.Errorf("属性标识 %q 的标识类型须为 i、s、g 或 b", text)
	}
	kind, ok := opcTypes[platform]
	if !ok {
		return device.Address{}, fmt.Errorf("属性标识 %q 的数据类型 %s 不受支持", text, platform)
	}
	return device.ParseAddress(fmt.Sprintf("ns=%s;%s=%s", ns, kindCode, id), kind, 1)
}

// FromResult describes a read point as an attribute; it fails for arrays and
// for types the platform cannot store.
func FromResult(r device.ReadResult) (Attribute, error) {
	if r.Value.Elements != nil || r.Address.Count > 1 {
		return Attribute{}, errors.New("viSCADA 属性不支持数组")
	}
	id, err := Identifier(r.Address.NodeID, r.Kind())
	if err != nil {
		return Attribute{}, err
	}
	a := Attribute{Identifier: id}
	if r.Writable() {
		a.Access = "2"
	} else if r.Access == "READ" {
		a.Access = "1"
	}
	return a, nil
}
