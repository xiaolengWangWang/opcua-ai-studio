package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
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
	// The address and port fields make the Endpoint; a pasted or chosen
	// Endpoint fills both, keeping its path.
	d.host.SetText("10.0.0.5")
	d.port.SetText("49320")
	if cfg, err := d.config(); err != nil || cfg.Target != "opc.tcp://10.0.0.5:49320" || d.endpoint() != cfg.Target {
		t.Fatalf("config: %+v %v", cfg, err)
	}
	d.host.SetText("opc.tcp://plc-01:53530/OPCUA/Sim")
	if d.host.Text != "plc-01" || d.port.Text != "53530" || d.endpoint() != "opc.tcp://plc-01:53530/OPCUA/Sim" {
		t.Fatalf("pasted endpoint: %q %q %q", d.host.Text, d.port.Text, d.endpoint())
	}
	d.host.SetText("10.0.0.6")
	if d.endpoint() != "opc.tcp://10.0.0.6:53530" {
		t.Fatalf("a typed address kept the old path: %q", d.endpoint())
	}
	d.host.SetText("a:4840")
	if d.host.Text != "a" || d.port.Text != "4840" || !slices.Contains(historyOptions([]string{"opc.tcp://a:4840"}), "a:4840") {
		t.Fatalf("recent endpoint option: %q %q", d.host.Text, d.port.Text)
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

func TestMenuMirrorsButtonsAndToggles(t *testing.T) {
	d := searchDesktop(t)
	names := map[string]*fyne.MenuItem{}
	for _, m := range d.mainMenu.Items {
		for _, mi := range m.Items {
			names[m.Label+"/"+mi.Label] = mi
		}
	}
	for _, want := range []string{"文件/导出 CSV…", "连接/搜索服务器…", "连接/连接数测试…", "读取/全量读取", "视图/趋势视图", "帮助/检查更新", "文件/退出"} {
		if names[want] == nil {
			t.Fatalf("menu item %s missing", want)
		}
	}
	d.controls()
	if names["连接/断开"].Disabled != d.disconnectBtn.Disabled() || names["读取/停止读取"].Disabled != d.stopReadBtn.Disabled() {
		t.Fatal("menu does not mirror the buttons")
	}
	names["视图/卡片视图"].Action()
	if d.viewMode.Selected != "卡片" || !d.cards.Visible() || d.table.Visible() || !names["视图/卡片视图"].Checked {
		t.Fatal("view menu")
	}
	names["连接/连接后自动全量读取"].Action()
	if !d.autoFullRead.Checked || !names["连接/连接后自动全量读取"].Checked {
		t.Fatal("toggle menu")
	}
	names["视图/显示左侧操作栏"].Action()
	if d.split.Offset > 0.02 || names["视图/显示左侧操作栏"].Checked {
		t.Fatal("side column not folded")
	}
}

func TestTrendViewKeepsHistoryPerPoint(t *testing.T) {
	d := searchDesktop(t)
	d.viewMode.SetSelected("趋势")
	rr := d.snapshot.Results
	for i := 0; i < 3; i++ {
		rr = append([]device.ReadResult(nil), rr...)
		rr[0].Value = device.Value{Type: "Double", Real: 90 + float64(i)}
		d.refresh(service.Snapshot{Revision: uint64(400 + i), State: "connected", Results: rr})
	}
	if h := d.history["ns=2;s=热力站.一次供水温度"]; len(h) != 4 || h[3] != 92 {
		t.Fatalf("history: %v", h)
	}
	if len(d.history["ns=2;s=热力站.一次回水温度"]) != 0 {
		t.Fatal("BAD point recorded")
	}
	// The trend shows the numeric points: the temperature and the counter.
	if len(d.trendRows) != 2 || !d.trends.Visible() {
		t.Fatalf("trend rows: %v", d.trendRows)
	}
	d.refresh(service.Snapshot{Revision: 410, State: "connected", Results: rr})
	if len(d.history["ns=2;s=热力站.一次供水温度"]) != 4 {
		t.Fatal("an unchanged list added a sample")
	}
}

func TestWriteButtonFollowsTheChosenPoint(t *testing.T) {
	d := searchDesktop(t)
	d.controls()
	if !d.writeSelectedBtn.Disabled() {
		t.Fatal("write enabled without a chosen point")
	}
	d.selectResult(d.snapshot.Results[2])
	if d.writeSelectedBtn.Disabled() {
		t.Fatal("control point not writable from the results")
	}
	d.selectResult(d.snapshot.Results[0])
	if !d.writeSelectedBtn.Disabled() {
		t.Fatal("read-only point offered for writing")
	}
}
