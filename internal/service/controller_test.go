package service

import (
	"context"
	"opcua-ai-studio/internal/simulator"
	"testing"
	"time"
)

func TestDemoWriteGuardReadbackAndStaleConfirmation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := New(Options{RecordDir: t.TempDir()})
	defer c.Close()
	if err := c.StartDemo(ctx); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot()
	if len(snap.Results) != 1 || snap.Results[0].Value.Integer != "42" {
		t.Fatalf("%+v", snap.Results)
	}
	req := WriteRequest{Point: Point{simulator.CounterNode, "Int32", 1}, Value: "123", Generation: snap.Generation}
	if err := c.Write(ctx, req, false); err == nil {
		t.Fatal("write allowed without approval")
	}
	if err := c.Write(ctx, req, true); err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.Readback != "一致" || s.Results[0].Value.Integer != "123" {
		t.Fatalf("%+v", s)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Target = snap.DemoTarget
	if err := c.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, req, true); err == nil {
		t.Fatal("old connection confirmation accepted")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestPollingStopsAndBrowseWorks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := New(Options{RecordDir: t.TempDir()})
	defer c.Close()
	if err := c.StartDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Browse(ctx, "ns=2;i=85"); err != nil {
		t.Fatal(err)
	}
	if len(c.Snapshot().Nodes) < 5 {
		t.Fatal("missing demo nodes")
	}
	if err := c.StartPolling(ctx, []Point{{simulator.CounterNode, "Auto", 1}}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	c.StopPolling()
	if c.Snapshot().Polling {
		t.Fatal("polling flag stuck")
	}
}
