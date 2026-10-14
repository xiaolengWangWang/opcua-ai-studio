package device

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Value struct {
	Type     DataType `json:"type"`
	Integer  string   `json:"integer,omitempty"`
	Boolean  bool     `json:"boolean,omitempty"`
	Real     float64  `json:"real,omitempty"`
	Text     string   `json:"text,omitempty"`
	Raw      []byte   `json:"raw,omitempty"`
	Elements []Value  `json:"elements,omitempty"`
}

func (v Value) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": v.Type}
	if v.Elements != nil {
		m["elements"] = v.Elements
	} else {
		switch v.Type {
		case "Boolean":
			m["boolean"] = v.Boolean
		case "Float", "Double":
			m["real"] = v.Real
		case "String", "DateTime":
			m["text"] = v.Text
		case "ByteString":
			m["raw"] = v.Raw
		case "Null":
			m["value"] = nil
		default:
			m["integer"] = v.Integer
		}
	}
	return json.Marshal(m)
}
func ParseValue(a Address, text string) (Value, error) {
	v := Value{Type: a.Type}
	if err := a.Validate(); err != nil {
		return v, err
	}
	if a.Type == "Auto" {
		return v, fmt.Errorf("写入须选择明确的数据类型")
	}
	if a.Count > 1 {
		var parts []json.RawMessage
		if err := json.Unmarshal([]byte(text), &parts); err != nil {
			return v, fmt.Errorf("数组须使用 JSON，例如 [1,2,3]: %w", err)
		}
		if len(parts) != a.Count {
			return v, fmt.Errorf("须提供 %d 个数组元素", a.Count)
		}
		one := a
		one.Count = 1
		for _, p := range parts {
			s := string(p)
			if a.Type == "String" || a.Type == "DateTime" || a.Type == "ByteString" {
				if err := json.Unmarshal(p, &s); err != nil {
					return v, fmt.Errorf("此类型的数组元素须为 JSON 字符串")
				}
			}
			e, err := ParseValue(one, s)
			if err != nil {
				return v, err
			}
			v.Elements = append(v.Elements, e)
		}
	} else {
		var err error
		switch a.Type {
		case "Boolean":
			v.Boolean, err = strconv.ParseBool(strings.TrimSpace(text))
		case "Float", "Double":
			v.Real, err = strconv.ParseFloat(strings.TrimSpace(text), 64)
		case "String":
			v.Text = text
		case "DateTime":
			var tm time.Time
			tm, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
			v.Text = tm.UTC().Format(time.RFC3339Nano)
		case "ByteString":
			v.Raw, err = hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(text), "0x"))
		default:
			v.Integer = strings.TrimSpace(text)
		}
		if err != nil {
			return v, fmt.Errorf("无效写入值: %w", err)
		}
	}
	_, err := v.Native()
	return v, err
}
func (v Value) Native() (any, error) {
	if v.Elements != nil {
		zero, err := (Value{Type: v.Type, Integer: "0", Text: "1970-01-01T00:00:00Z"}).Native()
		if err != nil {
			return nil, err
		}
		a := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(zero)), len(v.Elements), len(v.Elements))
		for i, e := range v.Elements {
			if e.Type != v.Type || e.Elements != nil {
				return nil, fmt.Errorf("数组元素类型不一致")
			}
			x, err := e.Native()
			if err != nil {
				return nil, err
			}
			a.Index(i).Set(reflect.ValueOf(x))
		}
		if v.Type == "Byte" {
			return ua.ByteArray(a.Interface().([]uint8)), nil
		}
		return a.Interface(), nil
	}
	signed := func(bits int) (int64, error) { return strconv.ParseInt(v.Integer, 10, bits) }
	unsigned := func(bits int) (uint64, error) { return strconv.ParseUint(v.Integer, 10, bits) }
	switch v.Type {
	case "Boolean":
		return v.Boolean, nil
	case "SByte":
		n, e := signed(8)
		return int8(n), e
	case "Byte":
		n, e := unsigned(8)
		return uint8(n), e
	case "Int16":
		n, e := signed(16)
		return int16(n), e
	case "UInt16":
		n, e := unsigned(16)
		return uint16(n), e
	case "Int32":
		n, e := signed(32)
		return int32(n), e
	case "UInt32":
		n, e := unsigned(32)
		return uint32(n), e
	case "Int64":
		return signed(64)
	case "UInt64":
		return unsigned(64)
	case "Float", "Double":
		if math.IsNaN(v.Real) || math.IsInf(v.Real, 0) || (v.Type == "Float" && math.IsInf(float64(float32(v.Real)), 0)) {
			return nil, fmt.Errorf("浮点值超出范围或不是有限数")
		}
		if v.Type == "Float" {
			return float32(v.Real), nil
		}
		return v.Real, nil
	case "String":
		return v.Text, nil
	case "DateTime":
		return time.Parse(time.RFC3339Nano, v.Text)
	case "ByteString":
		return append([]byte{}, v.Raw...), nil
	case "Null":
		return nil, nil
	}
	return nil, fmt.Errorf("不支持的类型 %q", v.Type)
}
func FromNative(x any) (Value, error) {
	v := Value{}
	switch n := x.(type) {
	case nil:
		v.Type = "Null"
	case bool:
		v.Type = "Boolean"
		v.Boolean = n
	case int8:
		v.Type = "SByte"
		v.Integer = strconv.FormatInt(int64(n), 10)
	case uint8:
		v.Type = "Byte"
		v.Integer = strconv.FormatUint(uint64(n), 10)
	case int16:
		v.Type = "Int16"
		v.Integer = strconv.FormatInt(int64(n), 10)
	case uint16:
		v.Type = "UInt16"
		v.Integer = strconv.FormatUint(uint64(n), 10)
	case int32:
		v.Type = "Int32"
		v.Integer = strconv.FormatInt(int64(n), 10)
	case uint32:
		v.Type = "UInt32"
		v.Integer = strconv.FormatUint(uint64(n), 10)
	case int64:
		v.Type = "Int64"
		v.Integer = strconv.FormatInt(n, 10)
	case uint64:
		v.Type = "UInt64"
		v.Integer = strconv.FormatUint(n, 10)
	case float32:
		v.Type = "Float"
		v.Real = float64(n)
	case float64:
		v.Type = "Double"
		v.Real = n
	case string:
		v.Type = "String"
		v.Text = n
	case time.Time:
		v.Type = "DateTime"
		v.Text = n.UTC().Format(time.RFC3339Nano)
	case []byte:
		v.Type = "ByteString"
		v.Raw = append([]byte{}, n...)
	default:
		r := reflect.ValueOf(x)
		if r.Kind() != reflect.Slice {
			return v, fmt.Errorf("暂不支持 OPC UA 值 %T", x)
		}
		v.Elements = make([]Value, r.Len())
		for i := 0; i < r.Len(); i++ {
			e, err := FromNative(r.Index(i).Interface())
			if err != nil {
				return v, err
			}
			v.Elements[i] = e
			v.Type = e.Type
		}
		if r.Len() == 0 {
			e, err := FromNative(reflect.Zero(r.Type().Elem()).Interface())
			if err != nil {
				return v, err
			}
			v.Type = e.Type
		}
	}
	if _, err := v.Native(); err != nil {
		return v, err
	}
	return v, nil
}
func ValueEqual(a, b Value) bool {
	x, e := a.Native()
	if e != nil {
		return false
	}
	y, e := b.Native()
	return e == nil && reflect.DeepEqual(x, y)
}
func FormatValue(v Value) string {
	if v.Elements != nil {
		parts := make([]string, len(v.Elements))
		for i, e := range v.Elements {
			parts[i] = FormatValue(e)
			if e.Type == "String" || e.Type == "DateTime" || e.Type == "ByteString" {
				parts[i] = strconv.Quote(parts[i])
			}
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	switch v.Type {
	case "Boolean":
		return strconv.FormatBool(v.Boolean)
	case "Float", "Double":
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case "String", "DateTime":
		return v.Text
	case "ByteString":
		return hex.EncodeToString(v.Raw)
	case "Null":
		return "null"
	default:
		return v.Integer
	}
}
