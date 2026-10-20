package viscada

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"opcua-ai-studio/internal/device"
	"strings"
	"testing"
	"time"
)

func TestIdentifierRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		node string
		kind device.DataType
		want string
	}{
		{"ns=3;s=Substation.Primary.SupplyTemp", "Double", "3::s::Substation.Primary.SupplyTemp::LREAL"},
		{"ns=3;i=1007", "Int32", "3::i::1007::DINT"},
		{"i=2258", "DateTime", ""},
		{"ns=2;s=Line1::Speed ", "Float", "2::s::Line1::Speed ::REAL"},
		{"ns=4;g=550E8400-E29B-41D4-A716-446655440000", "Boolean", "4::g::550E8400-E29B-41D4-A716-446655440000::BOOL"},
		{"ns=2;i=5", "UInt16", "2::i::5::UINT"},
		{"ns=2;s=热力站.一次供水温度", "Double", "2::s::热力站.一次供水温度::LREAL"},
	} {
		got, err := Identifier(tc.node, tc.kind)
		if tc.want == "" {
			if err == nil {
				t.Fatalf("%s: unsupported type accepted: %s", tc.node, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, %v", tc.node, got, err)
		}
		a, err := ParseIdentifier(got)
		if err != nil || a.NodeID != tc.node || a.Type != tc.kind {
			t.Fatalf("%s: parsed back %+v, %v", got, a, err)
		}
	}
	for _, bad := range []string{"", "ns=2;s=Tag", "x::s::Tag::REAL", "2::q::Tag::REAL", "2::s::Tag::FLOAT", "2::s::Tag"} {
		if _, err := ParseIdentifier(bad); err == nil {
			t.Fatalf("invalid identifier %q accepted", bad)
		}
	}
	if a, err := ParseIdentifier(" 0::i::85::wchar "); err != nil || a.NodeID != "i=85" || a.Type != "Auto" {
		t.Fatalf("character type not read as Auto: %+v, %v", a, err)
	}
}

func TestFromResultFillsAccessAndRejectsArrays(t *testing.T) {
	r := device.ReadResult{Address: device.Address{NodeID: "ns=2;s=SetSpeed", Type: "Auto", Count: 1}, Quality: "BAD"}
	r.NodeAttributes = device.NodeAttributes{DataType: "Int64", Access: "READ_WRITE"}
	a, err := FromResult(r)
	if err != nil || a.Identifier != "2::s::SetSpeed::LINT" || a.Access != "2" {
		t.Fatalf("BAD control point lost its declared type or access: %+v, %v", a, err)
	}
	r.Access = "READ"
	if a, _ = FromResult(r); a.Access != "1" {
		t.Fatalf("read-only access: %+v", a)
	}
	r.Value = device.Value{Type: "Int32", Elements: []device.Value{{Type: "Int32"}}}
	if _, err = FromResult(r); err == nil {
		t.Fatal("array exported as a scalar attribute")
	}
}

func TestVerifyIdentifierAgainstServer(t *testing.T) {
	read := func(node string, kind device.DataType, r device.ReadResult) device.ReadResult {
		r.Address = device.Address{NodeID: node, Type: kind, Count: 1}
		return r
	}
	double := device.Value{Type: "Double", Real: 95.5}
	for _, tc := range []struct {
		id      string
		r       device.ReadResult
		problem string
		want    string
	}{
		{"2::s::热力站.一次供水温度::LREAL", read("ns=2;s=热力站.一次供水温度", "Double", device.ReadResult{Quality: "GOOD", Value: double}), "", "2::s::热力站.一次供水温度::LREAL"},
		{"2::s::Temp::real", read("ns=2;s=Temp", "Float", device.ReadResult{Quality: "GOOD", Execution: "TYPE_MISMATCH", Value: double}), "属性标识为 REAL，服务器为 Double，应为 LREAL", "2::s::Temp::LREAL"},
		{"2::i::5::WORD", read("ns=2;i=5", "UInt16", device.ReadResult{Quality: "GOOD", Value: device.Value{Type: "UInt16", Integer: "5"}}), "", "2::i::5::UINT"},
		{"2::s::Offline::LREAL", read("ns=2;s=Offline", "Double", device.ReadResult{Quality: "BAD", StatusCode: 0x80310000, NodeAttributes: device.NodeAttributes{DataType: "Double"}}), "", "2::s::Offline::LREAL"},
		{"2::s::Gone::BOOL", read("ns=2;s=Gone", "Boolean", device.ReadResult{Quality: "BAD", StatusCode: 0x80340000}), "不存在", ""},
		{"2::s::Samples::DINT", read("ns=2;s=Samples", "Int32", device.ReadResult{Quality: "GOOD", Value: device.Value{Type: "Int32", Elements: []device.Value{}}}), "数组", ""},
		{"0::i::2258::LINT", read("i=2258", "Int64", device.ReadResult{Quality: "GOOD", Value: device.Value{Type: "DateTime"}}), "DateTime", ""},
		{"2::s::Tag", device.ReadResult{}, "namespace::类型", ""},
	} {
		problem, want := Verify(tc.id, tc.r)
		if (tc.problem == "") != (problem == "") || !strings.Contains(problem, tc.problem) || want != tc.want {
			t.Fatalf("%s: problem %q, want %q", tc.id, problem, want)
		}
	}
	if !IsIdentifier(" 2::s::Tag::REAL") || IsIdentifier("ns=2;s=Line1::Speed") || IsIdentifier("x::s::Tag::REAL") {
		t.Fatal("identifier shape")
	}
}

func TestWorkbookRoundTripOf50000Rows(t *testing.T) {
	rows := make([]Attribute, 50000)
	for i := range rows {
		rows[i] = Attribute{Identifier: fmt.Sprintf("2::s::Station%03d.Tag<%d>&::LREAL", i/100, i), Access: []string{"1", "2", ""}[i%3]}
		if i%7 == 0 {
			rows[i].Identifier = fmt.Sprintf("2::s::换热站%03d.供水温度%d::LREAL", i/100, i)
		}
	}
	var buf bytes.Buffer
	start := time.Now()
	if err := WriteAttributes(context.Background(), &buf, rows, nil); err != nil {
		t.Fatal(err)
	}
	written := time.Since(start)
	start = time.Now()
	got, err := ReadAttributes(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("50000 rows: write %v, read %v, %d bytes", written, time.Since(start), buf.Len())
	if len(got) != len(rows) {
		t.Fatalf("rows: %d", len(got))
	}
	for i, a := range got {
		if a.Row != i+2 || a.Identifier != rows[i].Identifier || a.Access != rows[i].Access || a.Name != "" {
			t.Fatalf("row %d: %+v", i, a)
		}
	}
}

func TestReadsSharedStringsAndCancels(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="/xl/worksheets/a.xml" Type="x/worksheet"/><Relationship Id="rId2" Target="strings.xml" Type="x/sharedStrings"/></Relationships>`,
		"xl/strings.xml":             `<sst><si><t>属性标识</t></si><si><r><t>3::s::</t></r><r><t>Pump::BOOL</t></r><rPh><t>ポンプ</t></rPh></si><si><t>泵状态</t></si></sst>`,
		"xl/worksheets/a.xml":        `<worksheet><sheetData><row r="1"><c r="C1" t="s"><v>0</v></c><c t="str"><v>属性名称</v></c><c r="E1" t="inlineStr"><is><t>读写模式</t></is></c></row><row r="3"><c r="C3" t="s"><v>1</v></c><c r="D3" t="s"><v>2</v></c><c r="E3"><v>2.0</v></c></row><row r="4"><c r="D4" t="s"><v>2</v></c></row></sheetData></worksheet>`,
	} {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	z.Close()
	got, err := ReadAttributes(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil || len(got) != 1 || got[0] != (Attribute{Row: 3, Identifier: "3::s::Pump::BOOL", Name: "泵状态", Access: "2"}) {
		t.Fatalf("Excel-saved workbook: %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WriteAttributes(ctx, &bytes.Buffer{}, make([]Attribute, 10), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("write ignored cancellation: %v", err)
	}
}
