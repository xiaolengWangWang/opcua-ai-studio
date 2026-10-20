package ui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
)

func TestEndpointIsCompletedAndRemembered(t *testing.T) {
	for in, want := range map[string]string{
		" 192.168.1.10 ":              "opc.tcp://192.168.1.10:4840",
		"plc-01:49320":                "opc.tcp://plc-01:49320",
		"opc.tcp://192.168.1.10":      "opc.tcp://192.168.1.10:4840",
		"opc.tcp://host:4840/UA/Demo": "opc.tcp://host:4840/UA/Demo",
		"opc.tcp://host/UA/Demo":      "opc.tcp://host:4840/UA/Demo",
		"http://192.168.1.10":         "http://192.168.1.10",
		"":                            "",
	} {
		if got := normalizeEndpoint(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
	d := searchDesktop(t)
	for _, e := range []string{"opc.tcp://a:4840", "opc.tcp://b:4840", "opc.tcp://a:4840"} {
		d.rememberEndpoint(e)
	}
	if got := strings.Join(d.endpointHistory(), ","); got != "opc.tcp://a:4840,opc.tcp://b:4840" {
		t.Fatalf("history: %s", got)
	}
	d.target.SetText("10.0.0.5")
	if cfg, err := d.config(); err != nil || cfg.Target != "opc.tcp://10.0.0.5:4840" || d.target.Text != cfg.Target {
		t.Fatalf("config: %+v %v", cfg, err)
	}
}

func TestResultsSortByColumnAndHighlightChanges(t *testing.T) {
	d := searchDesktop(t)
	d.sortBy(4)
	if got := visibleNodes(d); got != "ns=3;i=1007,ns=2;s=热力站.一次供水温度,ns=2;s=热力站.一次回水温度,ns=2;s=热力站.循环泵启停" {
		t.Fatalf("values ascending, numbers first: %s", got)
	}
	if d.sortMark(4) != " ▲" {
		t.Fatal("sort mark")
	}
	d.sortBy(4)
	if !strings.HasPrefix(visibleNodes(d), "ns=2;s=热力站.循环泵启停,") {
		t.Fatalf("descending: %s", visibleNodes(d))
	}
	d.sortBy(5)
	if !strings.HasPrefix(visibleNodes(d), "ns=2;s=热力站.一次回水温度,") {
		t.Fatalf("BAD sorts first by quality: %s", visibleNodes(d))
	}
	d.sortBy(5)
	d.sortBy(5)
	if d.sortCol != -1 || d.sortMark(5) != "" {
		t.Fatal("third click does not clear the sort")
	}
	// Polling brings a new value for one point.
	rr := append([]device.ReadResult(nil), d.snapshot.Results...)
	rr[3].Value = device.Value{Type: "Int32", Integer: "8"}
	d.refresh(service.Snapshot{Revision: 200, State: "connected", Results: rr})
	for row, i := range d.visible {
		changed := d.pointStyle(row, 4) == widget.HighImportance
		if changed != (i == 3) {
			t.Fatalf("row %d (result %d) highlighted %v", row, i, changed)
		}
	}
	if d.pointCell(0, 6) == "" && !d.snapshot.Results[d.visible[0]].Time.IsZero() {
		t.Fatal("time column empty")
	}
}

func TestBooleanSetValueAndCurrentValue(t *testing.T) {
	d := searchDesktop(t)
	d.selectResult(d.snapshot.Results[2])
	if d.singleKind.Selected != "Boolean" || d.writeValue.Visible() || !d.writeChoice.Visible() || d.writeChoice.Selected != "false" {
		t.Fatalf("Boolean control point: kind %s, choice %q", d.singleKind.Selected, d.writeChoice.Selected)
	}
	d.writeChoice.SetSelected("true")
	if d.writeValue.Text != "true" {
		t.Fatalf("choice not used as value: %q", d.writeValue.Text)
	}
	d.selectResult(d.snapshot.Results[0])
	if !d.writeValue.Visible() || d.writeChoice.Visible() || d.writeValue.Text != "true" {
		t.Fatalf("read-only point replaced the set value: %q", d.writeValue.Text)
	}
}

func TestPacketsFilterSearchAndStats(t *testing.T) {
	d := searchDesktop(t)
	now := time.Now()
	ok := device.ReadResult{Quality: "GOOD", Value: device.Value{Type: "Int32", Integer: "42"}}
	d.refresh(service.Snapshot{Revision: 300, State: "connected", Packets: []device.Event{
		{Time: now, RequestID: 1, Dir: device.DirTX, Operation: "READ", Status: device.StatusSent, Targets: []device.Address{{NodeID: "ns=2;s=Counter"}}},
		{Time: now, RequestID: 1, Dir: device.DirRX, Operation: "READ", Status: device.StatusSuccess, RTT: 4 * time.Millisecond, Targets: []device.Address{{NodeID: "ns=2;s=Counter"}}, Results: []device.EventResult{{Read: &ok}}},
		{Time: now, RequestID: 2, Dir: device.DirRX, Operation: "WRITE", Status: device.StatusException, RTT: 8 * time.Millisecond, Error: "BadUserAccessDenied", Targets: []device.Address{{NodeID: "ns=2;s=ReadOnly"}}},
	}})
	if len(d.packets) != 3 || d.packets[0].RequestID != 2 || !strings.Contains(d.packetStats.Text, "响应平均 6.0 ms，最长 8.0 ms") {
		t.Fatalf("newest first with stats: %d %q", len(d.packets), d.packetStats.Text)
	}
	d.packetFilter.SetSelected("异常")
	if len(d.packets) != 1 || d.packetCell(0, 4) != "异常" || d.packetStyle(0, 4) != widget.DangerImportance {
		t.Fatalf("exceptions: %d", len(d.packets))
	}
	d.packetFilter.SetSelected("全部")
	d.packetSearch.SetText("counter")
	if len(d.packets) != 2 {
		t.Fatalf("search: %d", len(d.packets))
	}
	if detail := packetDetail(d.packets[0]); !strings.Contains(detail, "ns=2;s=Counter → GOOD = 42（Int32）") {
		t.Fatalf("detail: %s", detail)
	}
}
