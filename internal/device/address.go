package device

import (
	"fmt"
	"github.com/gopcua/opcua/ua"
	"regexp"
	"strings"
	"unicode"
)

type DataType string
type Address struct {
	NodeID string   `json:"node_id"`
	Type   DataType `json:"type"`
	Count  int      `json:"count"`
}

var Types = []string{"Auto", "Boolean", "SByte", "Byte", "Int16", "UInt16", "Int32", "UInt32", "Int64", "UInt64", "Float", "Double", "String", "DateTime", "ByteString"}

var explicitNodeID = regexp.MustCompile(`^(?:ns=[0-9]+;)?[isgb]=`)

// fullWidthPrefix matches the ns= and s= fields typed with a Chinese input
// method, whose ＝ and ； would otherwise make the NodeID invalid.
var fullWidthPrefix = regexp.MustCompile(`^(?:ns[=＝][0-9]+[;；])?[isgb][=＝]`)

// HasChinese reports whether s contains Chinese characters, as the string
// identifiers of many Chinese servers do.
func HasChinese(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func NormalizeType(kind DataType) DataType {
	for _, t := range Types {
		if strings.EqualFold(string(kind), t) {
			return DataType(t)
		}
	}
	return kind
}
// halfWidthFields converts the ＝ and ； of the leading ns= and s= fields;
// the identifier is kept as typed.
func halfWidthFields(text string) string {
	if m := fullWidthPrefix.FindString(text); m != "" {
		return strings.NewReplacer("＝", "=", "；", ";").Replace(m) + text[len(m):]
	}
	return text
}

func ParseAddress(text string, kind DataType, count int) (Address, error) {
	text = halfWidthFields(strings.TrimLeft(text, " \t\r\n"))
	// Whitespace after s= belongs to the server's string identifier.
	if !strings.HasPrefix(text, "s=") && !strings.Contains(text, ";s=") {
		text = strings.TrimSpace(text)
	}
	if strings.HasPrefix(text, "s=") {
		text = "ns=0;" + text
	}
	a := Address{NodeID: text, Type: NormalizeType(kind), Count: count}
	if err := a.Validate(); err != nil {
		return a, err
	}
	n, _ := ua.ParseNodeID(a.NodeID)
	a.NodeID = FormatNodeID(n)
	return a, nil
}

// FormatNodeID keeps namespace zero explicit for strings: the SDK parser
// otherwise interprets a semicolon inside the identifier as a namespace field.
func FormatNodeID(n *ua.NodeID) string {
	if n.Type() == ua.NodeIDTypeString {
		return fmt.Sprintf("ns=%d;s=%s", n.Namespace(), n.StringID())
	}
	return n.String()
}
func (a Address) Validate() error {
	if a.NodeID == "" {
		return fmt.Errorf("请输入 NodeID，例如 ns=2;s=Counter")
	}
	if !explicitNodeID.MatchString(a.NodeID) {
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
