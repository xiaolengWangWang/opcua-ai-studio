package service

import (
	"context"
	"fmt"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/simulator"
	"testing"
	"time"
)

func loadController(t *testing.T, tags int) *Controller {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	sim, err := simulator.StartLoad(ctx, "opc.tcp://127.0.0.1:0", tags)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sim.Close() })
	c := New(Options{RecordDir: t.TempDir()})
	t.Cleanup(func() { c.Close() })
	cfg := DefaultConfig()
	cfg.Target = sim.Endpoint
	if err := c.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReadAllOf50000PointsAgainstTheSimulator(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	c := loadController(t, 50000)
	start := time.Now()
	if err := c.ReadAll(context.Background(), "ns=3;s=Load"); err != nil {
		t.Fatal(err)
	}
	t.Logf("browse, read and attributes of 50000 points: %v", time.Since(start))
	s := c.Snapshot()
	counts := map[string]int{}
	for _, r := range s.Results {
		counts["all"]++
		counts[r.Quality]++
		if r.Writable() {
			counts["writable"]++
		}
		if r.Quality == "BAD" && r.Kind() != "Double" {
			t.Fatalf("offline point lost its declared type: %+v", r)
		}
	}
	if counts["all"] != 50000 || counts["GOOD"] != 49000 || counts["BAD"] != 1000 || counts["writable"] != 5000 || !s.Results[0].Complete {
		t.Fatalf("results %d, counts %v, status %s", len(s.Results), counts, s.ScanStatus)
	}
	start = time.Now()
	for i := 0; i < 20; i++ {
		_ = c.Snapshot()
	}
	if elapsed := time.Since(start) / 20; elapsed > 5*time.Millisecond {
		t.Fatalf("snapshot of 50000 points takes %v", elapsed)
	}
}

func TestValidatePointsReportsMissingAndKeepsTheListAfterWrite(t *testing.T) {
	c := loadController(t, 200)
	ctx := context.Background()
	points := []Point{{"ns=3;s=Load.Station000.Tag00", "Double", 1}, {"ns=3;s=Load.Station000.Tag01", "Float", 1}, {"ns=3;s=Load.Missing", "Double", 1}}
	if err := c.ValidatePoints(ctx, "test.xlsx", points, []string{"设定", "温度", "缺失"}); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Results) != 3 || s.Results[0].DisplayName != "设定" || s.Results[0].Scope != "test.xlsx" || !s.Results[0].Writable() {
		t.Fatalf("imported names, scope or access lost: %+v", s.Results)
	}
	if s.Results[1].Execution != "TYPE_MISMATCH" || !s.Results[2].Missing() || s.Results[2].Kind() != "Double" {
		t.Fatalf("type mismatch or missing point hidden: %+v", s.Results[1:])
	}
	req := WriteRequest{Point: points[0], Value: "12.5", Generation: s.Generation}
	if err := c.Write(ctx, req, true); err != nil {
		t.Fatal(err)
	}
	s = c.Snapshot()
	if s.Readback != "一致" || len(s.Results) != 3 || s.Results[0].Value.Real != 12.5 || s.Results[0].DisplayName != "设定" || !s.Results[0].Writable() {
		t.Fatalf("write replaced the validated list: %+v", s.Results)
	}
}

func TestReadNodesReadsSelectedFoldersAndVariables(t *testing.T) {
	c := loadController(t, 300)
	roots := []device.Node{{NodeID: "ns=3;s=Load.Station001", Class: "Object"}, {NodeID: "ns=3;s=Load.Station002.Tag05", Class: "Variable"}, {NodeID: "ns=3;s=Load.Station001", Class: "Object"}}
	if err := c.ReadNodes(context.Background(), roots); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if len(s.Results) != 101 || s.Results[0].Address.NodeID != "ns=3;s=Load.Station002.Tag05" || s.Results[0].Scope != "ns=3;s=Load.Station001 等 2 个节点" {
		t.Fatalf("selected nodes: %d %+v", len(s.Results), s.Results[0])
	}
}

type slowDriver struct{ pointDriver }

// Read times out above 25 points, like a loaded server.
func (d *slowDriver) Read(ctx context.Context, aa []device.Address) ([]device.ReadResult, error) {
	if len(aa) > 25 {
		return nil, context.DeadlineExceeded
	}
	return d.pointDriver.Read(ctx, aa)
}

func TestReadBatchesShrinkAfterTimeouts(t *testing.T) {
	d := &slowDriver{}
	aa := make([]device.Address, 1200)
	for i := range aa {
		aa[i] = device.Address{NodeID: fmt.Sprintf("ns=2;s=N%d", i), Type: "Auto", Count: 1}
	}
	rr, err := readBatches(context.Background(), d, aa, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rr {
		if r.Quality != "GOOD" {
			t.Fatalf("timed out batch not retried: %+v", r)
		}
	}
}
