package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"opcua-ai-studio/internal/simulator"
)

// A connection, its run log, its requests and trend samples are kept in the
// record database and read back, also after the program closes.
func TestConnectionsLogsAndTrendsArePersisted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	c := New(Options{RecordDir: dir})
	defer c.Close()
	if err := c.StartDemo(ctx); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	at := time.Now()
	c.RecordSamples(map[string]Sample{simulator.CounterNode: {Time: at, Value: 42}})
	h, err := c.History([]string{simulator.CounterNode}, at.Add(-time.Second))
	if err != nil || len(h[simulator.CounterNode]) != 1 {
		t.Fatalf("trend: %v %v", h, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	c = New(Options{RecordDir: dir})
	defer c.Close()
	cc, err := c.Connections(10)
	if err != nil || len(cc) != 1 {
		t.Fatalf("connections: %+v %v", cc, err)
	}
	if got := cc[0]; !got.OK || got.Target != s.DemoTarget || got.Policy != "None" || got.Session != s.Session || got.End.IsZero() || !strings.Contains(got.Detail, "Namespaces") {
		t.Fatalf("connection: %+v", got)
	}
	lines, packets, err := c.SessionRecords(s.Session, 100)
	if err != nil || len(packets) == 0 {
		t.Fatalf("%d packets, %v", len(packets), err)
	}
	var log []string
	for _, l := range lines {
		log = append(log, l.Text)
	}
	if !strings.Contains(strings.Join(log, "\n"), "OPC UA 会话已连接") {
		t.Fatalf("log: %v", log)
	}
	if all, err := c.LogHistory(100); err != nil || len(all) < len(lines) {
		t.Fatalf("log history: %d %v", len(all), err)
	}
	// Reading another Endpoint's trend finds nothing.
	if h, err := c.History([]string{simulator.CounterNode}, at.Add(-time.Second)); err != nil || len(h[simulator.CounterNode]) != 0 {
		t.Fatalf("trend without a connection: %v %v", h, err)
	}
}
