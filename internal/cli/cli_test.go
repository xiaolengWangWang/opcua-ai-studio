package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/simulator"
	"testing"
	"time"
)

func TestCLIReadWriteAndWriteGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out, stderr bytes.Buffer
	if code := Run(ctx, []string{"-endpoint", s.Endpoint, "-json", "read", simulator.CounterNode}, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	var rr []device.ReadResult
	if err = json.Unmarshal(out.Bytes(), &rr); err != nil || rr[0].Value.Integer != "42" {
		t.Fatalf("%s %v", out.String(), err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"-endpoint", s.Endpoint, "-type", "Int32", "write", simulator.CounterNode, "123"}, &out, &stderr); code != 2 {
		t.Fatalf("write gate %d", code)
	}
	out.Reset()
	stderr.Reset()
	if code := Run(ctx, []string{"-endpoint", s.Endpoint, "-type", "Int32", "-allow-write", "-record", t.TempDir(), "write", simulator.CounterNode, "123"}, &out, &stderr); code != 0 {
		t.Fatalf("write %d %s", code, stderr.String())
	}
}
