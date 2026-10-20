package ui

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
)

func searchDesktop(t *testing.T) *Desktop {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	d := NewDesktop(a, service.New(service.Options{RecordDir: t.TempDir()}), "test")
	t.Cleanup(func() { d.Shutdown() })
	d.refresh(service.Snapshot{Revision: 99, State: "connected", Target: "opc.tcp://127.0.0.1:4840", Results: []device.ReadResult{
		{Address: device.Address{NodeID: "ns=2;s=热力站.一次供水温度", Type: "Auto", Count: 1}, DisplayName: "一次供水温度", Quality: "GOOD", Value: device.Value{Type: "Double", Real: 95.5}, NodeAttributes: device.NodeAttributes{DataType: "Double", Access: "READ"}},
		{Address: device.Address{NodeID: "ns=2;s=热力站.一次回水温度", Type: "Auto", Count: 1}, DisplayName: "一次回水温度", Quality: "BAD", StatusCode: 0x80310000},
		{Address: device.Address{NodeID: "ns=2;s=热力站.循环泵启停", Type: "Auto", Count: 1}, DisplayName: "循环泵启停", Quality: "GOOD", Value: device.Value{Type: "Boolean"}, NodeAttributes: device.NodeAttributes{Access: "READ_WRITE"}},
		{Address: device.Address{NodeID: "ns=3;i=1007", Type: "Auto", Count: 1}, DisplayName: "Speed", Quality: "GOOD", Value: device.Value{Type: "Int32", Integer: "7"}, Path: []string{"Objects", "Line1"}},
	}})
	return d
}

func visibleNodes(d *Desktop) string {
	var nodes []string
	for row := range d.visible {
		nodes = append(nodes, d.pointCell(row, 0))
	}
	return strings.Join(nodes, ",")
}

func TestSearchFiltersRowsAndExportFollowsIt(t *testing.T) {
	d := searchDesktop(t)
	for _, tc := range []struct {
		search, mode, filter, want string
	}{
		{"温度", "模糊", "全部", "ns=2;s=热力站.一次供水温度,ns=2;s=热力站.一次回水温度"},
		{"热力站 供水", "模糊", "全部", "ns=2;s=热力站.一次供水温度"},
		{"温度", "模糊", "GOOD", "ns=2;s=热力站.一次供水温度"},
		{"line1 speed", "模糊", "全部", "ns=3;i=1007"},
		{"一次供水温度", "精确", "全部", "ns=2;s=热力站.一次供水温度"},
		{"温度", "精确", "全部", ""},
		{"2::s::热力站.循环泵启停::BOOL", "精确", "全部", "ns=2;s=热力站.循环泵启停"},
		{"ns＝3；i＝1007", "精确", "全部", "ns=3;i=1007"},
		{"热力站.循环泵启停", "精确", "全部", "ns=2;s=热力站.循环泵启停"},
	} {
		d.resultFilter.SetSelected(tc.filter)
		d.searchMode.SetSelected(tc.mode)
		d.search.SetText(tc.search)
		if got := visibleNodes(d); got != tc.want {
			t.Fatalf("%s %q in %s: %q", tc.mode, tc.search, tc.filter, got)
		}
	}
	d.resultFilter.SetSelected("全部")
	d.searchMode.SetSelected("模糊")
	d.search.SetText("温度")
	if !strings.HasPrefix(d.pointSummary.String(), "显示 2 / 共 4") {
		t.Fatalf("summary: %q", d.pointSummary.String())
	}
	var buf bytes.Buffer
	if err := exportPointsCSV(t.Context(), &buf, d.snapshot, d.query(), nil); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(buf.String(), string(rune(0xfeff))))).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][10] != "一次供水温度" {
		t.Fatalf("CSV export ignores the search: %v %v", rows, err)
	}
	if d.singleNode.Text != "" || len(d.singleKinds) != 2 || d.singleKinds["ns=2;s=热力站.一次供水温度"] != "Double" {
		t.Fatalf("single point options do not follow the search: %v", d.singleKinds)
	}
	d.singleNode.SetText("ns=2;s=热力站.一次供水温度")
	if d.singleKind.Selected != "Double" {
		t.Fatalf("choosing a listed point kept type %s", d.singleKind.Selected)
	}
}

func TestTreeGroupsByPathAndIdentifier(t *testing.T) {
	d := searchDesktop(t)
	d.viewMode.SetSelected("树状")
	if !d.tree.Visible() || d.table.Visible() {
		t.Fatal("tree view not shown")
	}
	tree := d.treeIndex
	if got := strings.Join(tree.children[""], ","); got != "\x00ns=2,\x00Objects" {
		t.Fatalf("top level: %q", got)
	}
	station := "\x00ns=2\x00热力站"
	if tree.names[station] != "热力站" || tree.points[station] != 3 || len(tree.children[station]) != 3 {
		t.Fatalf("station branch: %v %d", tree.children[station], tree.points[station])
	}
	leaf := tree.children["\x00Objects\x00Line1"][0]
	if tree.names[leaf] != "Speed" || tree.rows[leaf] != 3 {
		t.Fatalf("browse path leaf: %q %d", tree.names[leaf], tree.rows[leaf])
	}
	d.tree.OnSelected(leaf)
	if d.singleNode.Text != "ns=3;i=1007" || d.singleKind.Selected != "Int32" || !strings.Contains(d.singleResult.Text, "3::i::1007::DINT") {
		t.Fatalf("tree selection: %q %s %q", d.singleNode.Text, d.singleKind.Selected, d.singleResult.Text)
	}
	d.search.SetText("泵")
	if len(d.treeIndex.rows) != 1 {
		t.Fatalf("tree ignores the search: %v", d.treeIndex.rows)
	}
	if path, name := treePath(&device.ReadResult{Address: device.Address{NodeID: "ns=4;s=Channel1/Device1.Tag1"}}); strings.Join(path, "|") != "ns=4|Channel1|Device1" || name != "Tag1" {
		t.Fatalf("Kepware path: %v %s", path, name)
	}
}

func TestSinglePointTakesViscadaIdentifier(t *testing.T) {
	d := searchDesktop(t)
	d.singleNode.SetText(" 2::s::热力站.一次供水温度::real")
	p, identifier, err := d.singlePoint()
	if err != nil || p.Address != "ns=2;s=热力站.一次供水温度" || p.Type != "Float" || identifier != "2::s::热力站.一次供水温度::real" || d.singleKind.Selected != "Float" {
		t.Fatalf("%+v %q %v", p, identifier, err)
	}
	r := d.snapshot.Results[0]
	r.Execution = "TYPE_MISMATCH"
	d.showSingle(&r, identifier)
	if !strings.Contains(d.singleResult.Text, "不通过") || !strings.Contains(d.singleResult.Text, "建议属性标识：2::s::热力站.一次供水温度::LREAL") || d.singleResult.Importance != widget.DangerImportance || d.singleIdent != "2::s::热力站.一次供水温度::LREAL" {
		t.Fatalf("mismatch not reported: %q", d.singleResult.Text)
	}
	r.Execution = "READ"
	d.showSingle(&r, "2::s::热力站.一次供水温度::LREAL")
	if !strings.Contains(d.singleResult.Text, "通过") || !strings.Contains(d.singleResult.Text, "读写模式：1 只读") || d.singleResult.Importance != widget.SuccessImportance {
		t.Fatalf("good identifier: %q", d.singleResult.Text)
	}
	d.singleNode.SetText("ns=2;s=Line1::Speed")
	d.singleKind.SetSelected("Double")
	if p, identifier, err = d.singlePoint(); err != nil || p.Address != "ns=2;s=Line1::Speed" || identifier != "" {
		t.Fatalf("NodeID containing :: read as identifier: %+v %v", p, err)
	}
	d.addresses.SetText("ns=2;s=Counter\n3::s::Load.Station000.Tag00::LREAL")
	pp, err := d.points()
	if err != nil || len(pp) != 2 || pp[1].Address != "ns=3;s=Load.Station000.Tag00" || pp[1].Type != "Double" {
		t.Fatalf("list identifiers: %+v %v", pp, err)
	}
}

func TestProfilesNeverSavePasswords(t *testing.T) {
	d := searchDesktop(t)
	d.setEndpoint("opc.tcp://192.168.1.10:4840")
	d.username.SetText("operator")
	d.password.SetText("test-password")
	d.policy.SetSelected("None")
	d.autoReconnect.SetChecked(false)
	d.storeProfiles([]profile{{Name: "一号站", Endpoint: d.endpoint(), Policy: "None", Mode: "None", Timeout: "3s", Username: "operator"}, {Name: "二号站", Endpoint: "opc.tcp://192.168.1.20:49320", Policy: "None", Mode: "None", Username: "admin", AutoReconnect: true}}, "")
	if strings.Contains(d.app.Preferences().String("profiles"), "test-password") {
		t.Fatal("password saved in a profile")
	}
	d.profile.SetSelected("二号站")
	if d.endpoint() != "opc.tcp://192.168.1.20:49320" || d.username.Text != "admin" || d.password.Text != "" || !d.autoReconnect.Checked {
		t.Fatalf("profile not applied or password kept for another account: %q %q", d.endpoint(), d.password.Text)
	}
	d.password.SetText("another")
	d.profile.SetSelected("一号站")
	if d.timeout.Text != "3s" || d.password.Text != "" {
		t.Fatal("profile switch kept the password")
	}
}
