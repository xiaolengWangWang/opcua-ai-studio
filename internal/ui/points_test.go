package ui

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"io"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"strings"
	"testing"
	"time"
)

func TestCSVExportSeparatesQualityAndPreservesNodeIDs(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	d.snapshot = service.Snapshot{Target: "opc.tcp://127.0.0.1:4840", Results: []device.ReadResult{
		{Address: device.Address{NodeID: "ns=2;s=中文,点", Type: "String", Count: 1}, Value: device.Value{Type: "String", Text: "=1+1"}, Quality: "GOOD"},
		{Address: device.Address{NodeID: "ns=2;s=Missing", Type: "Auto", Count: 1}, Quality: "BAD", StatusCode: 0x80340000, Error: "节点不存在"},
		{Address: device.Address{NodeID: "ns=2;s=Uncertain", Type: "Auto", Count: 1}, Quality: "UNCERTAIN", StatusCode: 0x40000000},
	}}
	exporter, ok := any(d).(interface{ ExportPointsCSV(io.Writer, string) error })
	if !ok {
		t.Fatal("point CSV export is missing")
	}
	for _, tc := range []struct {
		filter string
		count  int
		node   string
	}{{"全部", 3, "ns=2;s=中文,点"}, {"GOOD", 1, "ns=2;s=中文,点"}, {"BAD", 1, "ns=2;s=Missing"}, {"UNCERTAIN", 1, "ns=2;s=Uncertain"}} {
		var b bytes.Buffer
		if err := exporter.ExportPointsCSV(&b, tc.filter); err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(b.Bytes(), []byte{0xef, 0xbb, 0xbf}) {
			t.Fatal("CSV requires UTF-8 BOM for Chinese Excel")
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(b.String(), "\ufeff"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != tc.count+1 || rows[1][1] != tc.node {
			t.Fatalf("%s: wrong exported results: %+v", tc.filter, rows)
		}
		if tc.filter == "GOOD" && rows[1][3] != "'=1+1" {
			t.Fatal("spreadsheet formula not escaped")
		}
	}
}

func TestQualityFilterSelectsTheVisibleBadPoint(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	if d.resultFilter == nil || d.singleNode == nil {
		t.Fatal("quality filter and single point entry are missing")
	}
	d.refresh(service.Snapshot{Revision: 99, State: "connected", Results: []device.ReadResult{
		{Address: device.Address{NodeID: "ns=2;s=Good", Type: "Auto", Count: 1}, Value: device.Value{Type: "Int32", Integer: "42"}, Quality: "GOOD"},
		{Address: device.Address{NodeID: "ns=2;s=Bad", Type: "Auto", Count: 1}, Quality: "BAD", Error: "unknown node"},
	}})
	d.resultFilter.SetSelected("BAD")
	if len(d.visible) != 1 || d.pointCell(0, 0) != "ns=2;s=Bad" {
		t.Fatalf("BAD filter includes another quality: %+v", d.visible)
	}
	d.addresses.SetText("ns=2;s=Good\nns=2;s=Bad")
	d.table.OnSelected(widget.TableCellID{Row: 0, Col: 0})
	if d.singleNode.Text != "ns=2;s=Bad" {
		t.Fatalf("selected hidden good row instead of visible bad row: %s", d.singleNode.Text)
	}
	if d.addresses.Text != "ns=2;s=Good\nns=2;s=Bad" {
		t.Fatal("selecting a single point destroyed batch list")
	}
}

func TestBrowseAddsWithoutReplacingOrDuplicatingTheList(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	d.addresses.SetText("ns=2;s=First")
	d.refresh(service.Snapshot{Revision: 99, Nodes: []device.Node{{NodeID: "ns=2;s=Second", Class: "Variable"}}})
	d.browseTable.OnSelected(widget.TableCellID{Row: 0, Col: 1})
	d.browseTable.OnSelected(widget.TableCellID{Row: 0, Col: 1})
	if d.addresses.Text != "ns=2;s=First\nns=2;s=Second" {
		t.Fatalf("list replaced or duplicate added: %q", d.addresses.Text)
	}
}

type failedWriter struct{ err error }

func (w failedWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestCSVPreserves64BitTextAndReportsWriteFailure(t *testing.T) {
	d := &Desktop{snapshot: service.Snapshot{Results: []device.ReadResult{{Address: device.Address{NodeID: "ns=2;s=Big"}, Value: device.Value{Type: "UInt64", Integer: "18446744073709551615"}, Quality: "GOOD"}}}}
	var buf bytes.Buffer
	if err := d.ExportPointsCSV(&buf, "全部"); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(buf.String(), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][3] != "'18446744073709551615" {
		t.Fatalf("64 bit integer can be rounded by spreadsheet: %q", rows[1][3])
	}
	fail := errors.New("disk full")
	if err := d.ExportPointsCSV(failedWriter{fail}, "全部"); !errors.Is(err, fail) {
		t.Fatalf("write error hidden: %v", err)
	}
}

func TestEnterWhileBusyKeepsTheCurrentStopAction(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	current, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.busy = 1
	d.checkCancel = cancel
	d.refresh(service.Snapshot{Revision: 99, State: "connected"})
	d.singleNode.SetText("ns=2;s=Counter")
	d.singleNode.OnSubmitted(d.singleNode.Text)
	d.stopReadBtn.OnTapped()
	if current.Err() != context.Canceled {
		t.Fatal("Enter replaced the running task's stop action")
	}
}

func TestCSVUsesEachRowsTaskCompleteness(t *testing.T) {
	d := &Desktop{snapshot: service.Snapshot{ScanStatus: "最新单点验证完成", Results: []device.ReadResult{
		{Address: device.Address{NodeID: "ns=2;s=First"}, Quality: "GOOD", TaskID: "old", Complete: true},
		{Address: device.Address{NodeID: "ns=2;s=Second"}, Quality: "UNKNOWN", Execution: "NOT_READ", TaskID: "partial", Complete: false},
	}}}
	var buf bytes.Buffer
	if err := d.ExportPointsCSV(&buf, "全部"); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(buf.String(), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][11] != "完成" || rows[2][11] != "未完成" {
		t.Fatalf("task status overwritten by latest single read: %+v", rows)
	}
}

func TestBrowseSelectionTogglesAndSelectsAll(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	d.refresh(service.Snapshot{Revision: 99, State: "connected", Nodes: []device.Node{{NodeID: "ns=2;s=A", Class: "Object"}, {NodeID: "ns=2;s=B", Class: "Variable"}}})
	list := d.addresses.Text
	d.browseTable.OnSelected(widget.TableCellID{Row: 1, Col: 0})
	if len(d.browseSelection) != 1 || d.browseCell(1, 0) != "■" || d.browseStyle(1, 0) != widget.HighImportance || d.addresses.Text != list {
		t.Fatalf("checkbox column did not only select: %+v %q", d.browseSelection, d.addresses.Text)
	}
	d.browseTable.OnSelected(widget.TableCellID{Row: 1, Col: 0})
	if len(d.browseSelection) != 0 {
		t.Fatal("second click did not clear the selection")
	}
	d.selectAllBtn.OnTapped()
	if len(d.browseSelection) != 2 || d.browseSelection[0].NodeID != "ns=2;s=A" || d.readSelectedBtn.Disabled() {
		t.Fatalf("select all: %+v", d.browseSelection)
	}
}

func TestFiltersFindMissingAndControlPoints(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	rr := []device.ReadResult{
		{Address: device.Address{NodeID: "ns=2;s=Missing", Type: "Float", Count: 1}, Quality: "BAD", StatusCode: 0x80340000},
		{Address: device.Address{NodeID: "ns=2;s=Set", Type: "Auto", Count: 1}, Quality: "GOOD", Value: device.Value{Type: "Double", Real: 1}, NodeAttributes: device.NodeAttributes{Access: "READ_WRITE"}},
	}
	d.refresh(service.Snapshot{Revision: 99, State: "connected", Results: rr})
	rr = append(rr, device.ReadResult{Address: device.Address{NodeID: "ns=2;s=热力站.供水温度", Type: "Auto", Count: 1}, Quality: "GOOD", Value: device.Value{Type: "Double", Real: 95.5}})
	d.refresh(service.Snapshot{Revision: 100, State: "connected", Results: rr})
	for filter, node := range map[string]string{"不存在": "ns=2;s=Missing", "可写（控制点位）": "ns=2;s=Set", "含中文": "ns=2;s=热力站.供水温度"} {
		d.resultFilter.SetSelected(filter)
		if len(d.visible) != 1 || d.pointCell(0, 0) != node {
			t.Fatalf("%s: %+v", filter, d.visible)
		}
	}
	d.resultFilter.SetSelected("不存在")
	d.table.OnSelected(widget.TableCellID{Row: 0, Col: 0})
	if d.kind.Selected != "Float" {
		t.Fatalf("imported type not kept for writing: %s", d.kind.Selected)
	}
	if d.pointCell(0, 1) != "Float" || d.pointStyle(0, 4) != widget.DangerImportance || pointRow(&rr[1])[2] != "读写" {
		t.Fatal("type or access column missing")
	}
}

func TestRefreshOf50000PointsIsCheap(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	c := service.New(service.Options{RecordDir: t.TempDir()})
	d := NewDesktop(a, c, "test")
	defer d.Shutdown()
	rr := make([]device.ReadResult, 50000)
	nodes := make([]device.Node, len(rr))
	for i := range rr {
		rr[i] = device.ReadResult{Address: device.Address{NodeID: fmt.Sprintf("ns=2;s=Tag%d", i), Type: "Auto", Count: 1}, Quality: "GOOD", Value: device.Value{Type: "Double", Real: float64(i)}}
		nodes[i] = device.Node{NodeID: rr[i].Address.NodeID, Class: "Variable"}
	}
	start := time.Now()
	for rev := uint64(100); rev < 110; rev++ {
		d.refresh(service.Snapshot{Revision: rev, State: "connected", Results: rr, Nodes: nodes})
	}
	if elapsed := time.Since(start) / 10; elapsed > 50*time.Millisecond {
		t.Fatalf("refresh of 50000 points takes %v", elapsed)
	}
	if len(d.visible) != 50000 || d.pointCell(49999, 0) != "ns=2;s=Tag49999" {
		t.Fatal("rows lost")
	}
}
