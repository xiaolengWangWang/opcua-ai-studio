package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/simulator"
)

func TestFullReadRecordsBrowsePathsAndSearchFindsNodes(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{
		"i=85":                 {{NodeID: "ns=2;s=Station1", DisplayName: "一号站", Class: "Object"}, {NodeID: "ns=2;s=Top", DisplayName: "Top", Class: "Variable"}},
		"ns=2;s=Station1":      {{NodeID: "ns=2;i=7", DisplayName: "供水温度", Class: "Variable"}, {NodeID: "ns=2;s=Station1.Pump", DisplayName: "循环泵", Class: "Object"}},
		"ns=2;s=Station1.Pump": {{NodeID: "ns=2;i=8", DisplayName: "启停", Class: "Variable"}},
	}}
	c := pointController(t, d)
	ctx := context.Background()
	if err := c.ReadAll(ctx, "i=85"); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, r := range c.Snapshot().Results {
		paths[r.Address.NodeID] = strings.Join(r.Path, "/")
	}
	if want := map[string]string{"ns=2;s=Top": "Objects", "ns=2;i=7": "Objects/一号站", "ns=2;i=8": "Objects/一号站/循环泵"}; fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("paths: %v", paths)
	}
	// A single verification keeps the path the full read recorded.
	if err := c.VerifyPoint(ctx, Point{"ns=2;i=8", "Auto", 1}); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.Snapshot().Results {
		if r.Address.NodeID == "ns=2;i=8" && len(r.Path) != 3 {
			t.Fatalf("verification lost the path: %v", r.Path)
		}
	}
	if err := c.SearchNodes(ctx, "i=85", func(n device.Node) bool { return strings.Contains(n.DisplayName, "泵") || n.NodeID == "ns=2;i=7" }); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	var found []string
	for _, n := range s.Nodes {
		found = append(found, n.NodeID)
	}
	if strings.Join(found, ",") != "ns=2;i=7,ns=2;s=Station1.Pump" || !strings.Contains(s.BrowseStatus, "浏览 5 个节点，找到 2 个") {
		t.Fatalf("search: %v %q", found, s.BrowseStatus)
	}
}

func TestReferencePointIsReadAfterConnecting(t *testing.T) {
	d := &pointDriver{tree: map[string][]device.Node{}}
	c := pointController(t, d)
	ctx := context.Background()
	if err := c.CheckReference(ctx); err == nil {
		t.Fatal("check without a reference point")
	}
	c.SetReference(&Point{"ns=2;s=Heartbeat", "Auto", 1})
	cfg := DefaultConfig()
	cfg.Target = "opc.tcp://127.0.0.1:4840"
	if err := c.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.Reference == nil || s.Reference.Address.NodeID != "ns=2;s=Heartbeat" || s.Reference.Quality != "GOOD" || len(s.Results) != 0 {
		t.Fatalf("reference not read apart from the results: %+v %d", s.Reference, len(s.Results))
	}
	c.SetReference(nil)
	if c.Snapshot().Reference != nil {
		t.Fatal("cleared reference still shown")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitState(t *testing.T, c *Controller, states ...string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		s := c.Snapshot()
		if slices.Contains(states, s.State) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %s, want %v: %s", s.State, states, s.LastError)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestServerInfoAndAutomaticReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	srv, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	endpoint := srv.Endpoint
	// An outage closes the live connection and fails new ones until it ends.
	var mu sync.Mutex
	var down bool
	var live []*opcua.Client
	outage := func(on bool) {
		mu.Lock()
		defer mu.Unlock()
		down = on
		for _, client := range live {
			if on {
				client.Close()
			}
		}
	}
	dial := func(ctx context.Context, target string, o opcua.Options) (device.Driver, error) {
		mu.Lock()
		defer mu.Unlock()
		if down {
			return nil, errors.New("网络不可达")
		}
		client, err := opcua.Dial(ctx, target, o)
		if err != nil {
			return nil, err
		}
		live = append(live, client)
		return client, nil
	}
	c := New(Options{RecordDir: t.TempDir(), Dial: dial})
	defer c.Close()
	cfg := DefaultConfig()
	cfg.Target = endpoint
	cfg.AutoReconnect = true
	c.SetReference(&Point{simulator.CounterNode, "Auto", 1})
	if err := c.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.Server == nil || s.Server.Endpoint != endpoint || s.Server.State != "Running" || !slices.Contains(s.Server.Namespaces, "urn:opcua-ai-studio:demo") {
		t.Fatalf("server info: %+v", s.Server)
	}
	if s.Reference == nil || s.Reference.Value.Integer != "42" {
		t.Fatalf("reference: %+v", s.Reference)
	}
	if err := c.Read(ctx, []Point{{simulator.CounterNode, "Auto", 1}}); err != nil {
		t.Fatal(err)
	}
	if err := c.StartPolling(ctx, []Point{{simulator.CounterNode, "Auto", 1}}, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	outage(true)
	waitState(t, c, "reconnecting", "error")
	if s = c.Snapshot(); len(s.Results) != 1 {
		t.Fatal("a lost connection discarded the results")
	}
	time.Sleep(3 * time.Second)
	if s = c.Snapshot(); s.State == "connected" || !strings.Contains(fmt.Sprint(s.Logs), "自动重连") {
		t.Fatalf("connected during the outage: %s %v", s.State, s.Logs)
	}
	outage(false)
	waitState(t, c, "connected")
	// The server information, reference point and polling follow the connection.
	deadline := time.Now().Add(10 * time.Second)
	for s = c.Snapshot(); s.Reference == nil || s.Server == nil || !s.Polling; s = c.Snapshot() {
		if time.Now().After(deadline) {
			t.Fatalf("reconnect did not restore reference, server info or polling: %+v %+v %v", s.Reference, s.Server, s.Polling)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(s.Results) != 1 {
		t.Fatal("reconnect lost the results")
	}
	// Disconnecting ends the session for good.
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if s = c.Snapshot(); s.State != "disconnected" {
		t.Fatalf("reconnected after disconnecting: %s", s.State)
	}
}

// openDriver keeps its socket open while requests time out, as on a silent
// network drop.
type openDriver struct{ pointDriver }

func (*openDriver) IsClosed() bool { return false }

func TestRepeatedTimeoutsMarkTheConnectionLost(t *testing.T) {
	d := &openDriver{pointDriver{tree: map[string][]device.Node{}}}
	c := New(Options{RecordDir: t.TempDir(), Dial: func(context.Context, string, opcua.Options) (device.Driver, error) { return d, nil }})
	defer c.Close()
	cfg := DefaultConfig()
	cfg.Target = "opc.tcp://127.0.0.1:4840"
	ctx := context.Background()
	if err := c.Connect(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	read := func() { c.Read(ctx, []Point{{"ns=2;s=Tag", "Auto", 1}}) }
	d.readErr = context.DeadlineExceeded
	read()
	d.readErr = nil
	read()
	d.readErr = context.DeadlineExceeded
	read()
	if s := c.Snapshot(); s.State != "connected" {
		t.Fatalf("timeouts apart from each other dropped the connection: %s", s.State)
	}
	read()
	if s := c.Snapshot(); s.State != "error" || !strings.Contains(s.LastError, "连续 2 次未响应") {
		t.Fatalf("two timeouts in a row kept the connection: %s %s", s.State, s.LastError)
	}
}
