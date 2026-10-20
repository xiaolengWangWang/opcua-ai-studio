package device

import "testing"

func TestFuzzyAndExactSearch(t *testing.T) {
	fields := []string{"ns=2;s=热力站.一次供水温度", "一次供水温度"}
	for _, tc := range []struct {
		search string
		exact  bool
		want   bool
	}{
		{"", false, true},
		{"  ", true, true},
		{"供水 热力站", false, true},
		{"供水 回水", false, false},
		{"NS=2", false, true},
		{"ns＝2；s＝热力", false, true},
		{"一次供水温度", true, true},
		{"供水温度", true, false},
		{" ns=2;s=热力站.一次供水温度 ", true, true},
		{"NS=2;S=热力站.一次供水温度", true, false},
	} {
		if got := Matcher(tc.search, tc.exact)(fields...); got != tc.want {
			t.Fatalf("%q exact=%v: got %v", tc.search, tc.exact, got)
		}
	}
	if !Matcher("泵；启停", false)("ns=2;s=泵；启停") {
		t.Fatal("full-width punctuation inside an identifier was converted")
	}
	for in, want := range map[string]string{"ns=2;s=Line1.Temp": "Line1.Temp", "i=85": "85", "ns=0;s=Pump;State": "Pump;State", "ns=4;g=550E8400-E29B-41D4-A716-446655440000": "550E8400-E29B-41D4-A716-446655440000"} {
		if got := Identifier(in); got != want {
			t.Fatalf("%s: %q", in, got)
		}
	}
}
