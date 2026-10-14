package ui

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"io"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/service"
	"strings"
	"testing"
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
	if len(d.rows) != 2 || d.rows[1][0] != "ns=2;s=Bad" {
		t.Fatalf("BAD filter includes another quality: %+v", d.rows)
	}
	d.addresses.SetText("ns=2;s=Good\nns=2;s=Bad")
	d.table.OnSelected(widget.TableCellID{Row: 1, Col: 0})
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
	d.browseTable.OnSelected(widget.TableCellID{Row: 1})
	d.browseTable.OnSelected(widget.TableCellID{Row: 1})
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
