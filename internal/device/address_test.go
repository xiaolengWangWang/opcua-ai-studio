package device

import (
	"github.com/gopcua/opcua/ua"
	"testing"
)

func TestNodeIDPreservesCaseAndPunctuation(t *testing.T) {
	a, err := ParseAddress("ns=2;s=Plant.Motor,Speed", "Int32", 1)
	if err != nil || a.NodeID != "ns=2;s=Plant.Motor,Speed" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestInvalidNodeIDsAndTypes(t *testing.T) {
	for _, id := range []string{"", "DB1.DBD0", "ns=999999;i=1", "ns=2;i=-1"} {
		if _, err := ParseAddress(id, "Int32", 1); err == nil {
			t.Errorf("accepted %q", id)
		}
	}
	if _, err := ParseAddress("i=2258", "unsupported", 1); err == nil {
		t.Fatal("accepted unknown type")
	}
}

func TestTypedValuesPreservePrecisionAndUnicode(t *testing.T) {
	for _, tc := range []struct{ kind, input string }{{"UInt64", "18446744073709551615"}, {"Int64", "-9223372036854775808"}, {"String", "供水温度,正常"}} {
		a, err := ParseAddress("ns=2;s=Value", DataType(tc.kind), 1)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ParseValue(a, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := v.Native()
		if err != nil {
			t.Fatal(err)
		}
		got, err := FromNative(raw)
		if err != nil || !ValueEqual(v, got) {
			t.Fatalf("%+v %+v %v", v, got, err)
		}
	}
}

func TestArraysAndRangeChecks(t *testing.T) {
	a, _ := ParseAddress("ns=2;s=Values", "Int16", 3)
	v, err := ParseValue(a, "[1,-2,32767]")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Elements) != 3 || v.Elements[1].Integer != "-2" {
		t.Fatalf("%+v", v)
	}
	for _, s := range []string{"[1,2]", "[1,2,32768]"} {
		if _, err := ParseValue(a, s); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	for _, kind := range []DataType{"Float", "Double"} {
		a, _ := ParseAddress("i=1", kind, 1)
		if _, err := ParseValue(a, "NaN"); err == nil {
			t.Fatal("accepted NaN")
		}
	}
}

func TestByteArrayRemainsAnOPCUAArray(t *testing.T) {
	a, _ := ParseAddress("ns=2;s=Bytes", "Byte", 3)
	v, err := ParseValue(a, "[1,2,255]")
	if err != nil {
		t.Fatal(err)
	}
	native, err := v.Native()
	if err != nil {
		t.Fatal(err)
	}
	variant, err := ua.NewVariant(native)
	if err != nil {
		t.Fatal(err)
	}
	if variant.Type() != ua.TypeIDByte || !variant.Has(ua.VariantArrayValues) {
		t.Fatalf("array encoded as %v", variant.Type())
	}
}

func TestSnapshotClonePreservesEmptyArrays(t *testing.T) {
	v, err := FromNative([]int32{})
	if err != nil {
		t.Fatal(err)
	}
	e := Event{Results: []EventResult{{Read: &ReadResult{Value: v}}}}
	got := e.Clone().Results[0].Read.Value
	if got.Elements == nil || !ValueEqual(v, got) {
		t.Fatalf("empty array became scalar: %+v", got)
	}
}
