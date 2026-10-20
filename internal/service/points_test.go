package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/simulator"
	"testing"
)

type pointDriver struct {
	tree      map[string][]device.Node
	browseErr map[string]error
	readErr   error
	limit     int
	afterRead func()
}

func (d *pointDriver) Browse(_ context.Context, node string) ([]device.Node, error) {
	return d.tree[node], d.browseErr[node]
}
func (d *pointDriver) Read(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.readErr != nil {
		return nil, d.readErr
	}
	limit := d.limit
	if limit == 0 {
		limit = 100
	}
	if len(aa) > limit {
		return nil, ua.StatusBadTooManyOperations
	}
	rr := make([]device.ReadResult, len(aa))
	for i, a := range aa {
		rr[i] = device.ReadResult{Address: a, Quality: "GOOD", Value: device.Value{Type: "Int32", Integer: "42"}}
	}
	if d.afterRead != nil {
		d.afterRead()
	}
	return rr, nil
}

func TestReadAllCancellationRetainsCompletedAndUnreadPoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &pointDriver{tree: map[string][]device.Node{}, afterRead: cancel}
	for i := 0; i < 205; i++ {
		d.tree["i=85"] = append(d.tree["i=85"], device.Node{NodeID: fmt.Sprintf("ns=2;s=N%d", i), Class: "Variable"})
	}
	c := pointController(t, d)
	if err := c.ReadAll(ctx, "i=85"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	rr := c.Snapshot().Results
	if len(rr) != 205 {
		t.Fatalf("lost unread points: %d", len(rr))
	}
	for i, r := range rr {
		if r.Complete || r.Scope != "i=85" || r.TaskID == "" {
			t.Fatalf("incomplete scope lost: %+v", r)
		}
		if i < 100 && r.Quality != "GOOD" {
			t.Fatalf("completed point lost: %+v", r)
		}
		if i >= 100 && (r.Execution != "NOT_READ" || r.Quality != "UNKNOWN") {
			t.Fatalf("unread point fabricated quality: %+v", r)
		}
	}
}

func TestReadAllAdaptsToServerBatchLimit(t *testing.T) {
	d := &pointDriver{limit: 37, tree: map[string][]device.Node{}}
	for i := 0; i < 205; i++ {
		d.tree["i=85"] = append(d.tree["i=85"], device.Node{NodeID: fmt.Sprintf("ns=2;s=N%d", i), Class: "Variable"})
	}
	c := pointController(t, d)
	if err := c.ReadAll(context.Background(), "i=85"); err != nil {
		t.Fatal(err)
	}
	rr := c.Snapshot().Results
	if len(rr) != 205 {
		t.Fatalf("missing points: %d", len(rr))
	}
	for _, r := range rr {
		if r.Quality != "GOOD" || !r.Complete {
			t.Fatalf("failed result: %+v", r)
		}
	}
}

func TestReadAllPreservesStringIdentifierWhitespace(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{"i=85": {{NodeID: "ns=2;s=Pump", Class: "Variable"}, {NodeID: "ns=2;s=Pump ", Class: "Variable"}}}}
	c := pointController(t, d)
	if err := c.ReadAll(context.Background(), "i=85"); err != nil {
		t.Fatal(err)
	}
	rr := c.Snapshot().Results
	if len(rr) != 2 || rr[0].Address.NodeID != "ns=2;s=Pump" || rr[1].Address.NodeID != "ns=2;s=Pump " {
		t.Fatalf("distinct string identifiers collapsed: %+v", rr)
	}
}

func TestReadAllKeepsKnownNamesAfterBrowsingAnotherBranch(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{"i=85": {{NodeID: "ns=2;s=Pump", Class: "Variable", DisplayName: "泵状态"}}}}
	c := pointController(t, d)
	if err := c.ReadAll(context.Background(), "i=85"); err != nil {
		t.Fatal(err)
	}
	if err := c.Browse(context.Background(), "ns=2;s=Other"); err != nil {
		t.Fatal(err)
	}
	rr := c.Snapshot().Results
	if len(rr) != 1 || rr[0].DisplayName != "泵状态" {
		t.Fatalf("known name lost: %+v", rr)
	}
}
func (*pointDriver) Write(context.Context, device.Address, device.Value) (device.WriteResult, error) {
	return device.WriteResult{}, errors.New("unexpected write")
}
func (*pointDriver) Close() error { return nil }
func pointController(t *testing.T, d *pointDriver) *Controller {
	t.Helper()
	c := New(Options{RecordDir: t.TempDir(), Dial: func(context.Context, string, opcua.Options) (device.Driver, error) { return d, nil }})
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := DefaultConfig()
	cfg.Target = "opc.tcp://127.0.0.1:4840"
	if err := c.Connect(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReadAllTraversesNestedNodesWithoutDuplicatesAndBatches(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{
		"i=85":          {{NodeID: "ns=2;s=Folder", Class: "Object"}},
		"ns=2;s=Folder": {{NodeID: "i=85", Class: "Object"}},
	}}
	for i := 0; i < 1100; i++ {
		d.tree["ns=2;s=Folder"] = append(d.tree["ns=2;s=Folder"], device.Node{NodeID: fmt.Sprintf("ns=2;s=Tag%d", i), Class: "Variable", DisplayName: fmt.Sprintf("点%d", i)})
	}
	d.tree["i=85"] = append(d.tree["i=85"], d.tree["ns=2;s=Folder"][1])
	c := pointController(t, d)
	reader, ok := any(c).(interface {
		ReadAll(context.Context, string) error
	})
	if !ok {
		t.Fatal("recursive full read is missing")
	}
	if err := reader.ReadAll(context.Background(), "i=85"); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Results) != 1100 {
		t.Fatalf("want all 1100 unique variables, got %d", len(s.Results))
	}
	seen := map[string]bool{}
	for _, r := range s.Results {
		if seen[r.Address.NodeID] || r.Quality != "GOOD" {
			t.Fatalf("invalid result: %+v", r)
		}
		seen[r.Address.NodeID] = true
	}
}

func TestVerifyPointPreservesOtherResultsAndReportsBadNode(t *testing.T) {
	c := New(Options{RecordDir: t.TempDir()})
	defer c.Close()
	ctx := context.Background()
	if err := c.StartDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Read(ctx, []Point{{"ns=2;s=Counter", "Auto", 1}, {"ns=2;s=Temperature", "Auto", 1}}); err != nil {
		t.Fatal(err)
	}
	v, ok := any(c).(interface {
		VerifyPoint(context.Context, Point) error
	})
	if !ok {
		t.Fatal("single point verification is missing")
	}
	if err := v.VerifyPoint(ctx, Point{"ns=2;s=Missing", "Auto", 1}); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Results) != 3 || s.Results[0].Quality != "GOOD" || s.Results[1].Quality != "GOOD" || s.Results[2].Quality != "BAD" || s.Results[2].Error == "" {
		t.Fatalf("single verification lost results or bad status: %+v", s.Results)
	}
	if err := v.VerifyPoint(ctx, Point{"ns=2;s=Counter", "Auto", 1}); err != nil {
		t.Fatal(err)
	}
	if len(c.Snapshot().Results) != 3 {
		t.Fatal("verification duplicated the same node")
	}
}

func TestReadAllRetainsFailureResults(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{"i=85": {{NodeID: "ns=2;s=Tag", Class: "Variable"}}}, readErr: errors.New("connection lost")}
	c := pointController(t, d)
	r, ok := any(c).(interface {
		ReadAll(context.Context, string) error
	})
	if !ok {
		t.Fatal("recursive full read is missing")
	}
	if err := r.ReadAll(context.Background(), "i=85"); err == nil {
		t.Fatal("read failure hidden")
	}
	s := c.Snapshot()
	if len(s.Results) != 1 || s.Results[0].Quality != "UNKNOWN" || s.Results[0].Error == "" {
		t.Fatalf("failed point disappeared: %+v", s.Results)
	}
}

func TestChinesePointsReadWriteAndBrowse(t *testing.T) {
	c := New(Options{RecordDir: t.TempDir()})
	defer c.Close()
	ctx := context.Background()
	if err := c.StartDemo(ctx); err != nil {
		t.Fatal(err)
	}
	// Typed with a Chinese input method: full-width ＝ and ； in the fields.
	if err := c.Read(ctx, []Point{{"ns＝2；s＝热力站.一次供水温度", "Auto", 1}, {simulator.ChineseNodes[2], "Auto", 1}}); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Results) != 2 || s.Results[0].Address.NodeID != simulator.ChineseNodes[0] || s.Results[0].Value.Real != 95.5 || s.Results[1].Value.Text != "一号换热站" {
		t.Fatalf("Chinese points: %+v", s.Results)
	}
	pump := Point{simulator.ChineseNodes[1], "Boolean", 1}
	if err := c.Write(ctx, WriteRequest{Point: pump, Value: "true", Generation: s.Generation}, true); err != nil {
		t.Fatal(err)
	}
	if s = c.Snapshot(); s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" {
		t.Fatalf("write: %+v %s", s.WriteResult, s.Readback)
	}
	text := "一号换热站 正常运行，供水温度95℃"
	if err := c.Write(ctx, WriteRequest{Point: Point{"ns=2;s=Message", "String", 1}, Value: text, Generation: s.Generation}, true); err != nil {
		t.Fatal(err)
	}
	if s = c.Snapshot(); s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" {
		t.Fatalf("Chinese text: %+v %s", s.WriteResult, s.Readback)
	}
	// The server refuses a read-only Chinese point, which keeps its value.
	if err := c.Write(ctx, WriteRequest{Point: Point{simulator.ChineseNodes[2], "String", 1}, Value: "二号换热站", Generation: s.Generation}, true); err == nil {
		t.Fatal("read-only point accepted a write")
	}
	if s = c.Snapshot(); s.WriteResult.State != "REJECTED" {
		t.Fatalf("read-only write: %+v", s.WriteResult)
	}
	if err := c.ReadAll(ctx, "ns=2;i=85"); err != nil {
		t.Fatal(err)
	}
	names, writable, values := map[string]string{}, map[string]bool{}, map[string]string{}
	for _, r := range c.Snapshot().Results {
		names[r.Address.NodeID] = r.DisplayName
		writable[r.Address.NodeID] = r.Writable()
		values[r.Address.NodeID] = r.Value.Text
	}
	if values["ns=2;s=Message"] != text || values[simulator.ChineseNodes[2]] != "一号换热站" {
		t.Fatalf("values after writes: %v", values)
	}
	if !writable[simulator.ChineseNodes[1]] || writable[simulator.ChineseNodes[0]] || writable[simulator.ChineseNodes[2]] {
		t.Fatalf("access: %v", writable)
	}
	for _, n := range simulator.ChineseNodes {
		if _, ok := names[n]; !ok {
			t.Fatalf("full read missed %s: %v", n, names)
		}
	}
	if names[simulator.ChineseNodes[1]] != "循环泵启停" {
		t.Fatalf("display name: %q", names[simulator.ChineseNodes[1]])
	}
}
