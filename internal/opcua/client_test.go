package opcua

import (
	"context"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/simulator"
	"testing"
	"time"
)

func TestLoopbackBrowseReadWriteAndPartialFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := Dial(ctx, s.Endpoint, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	nodes, err := c.Browse(ctx, "ns=2;i=85")
	if err != nil || len(nodes) < 5 {
		t.Fatalf("%+v %v", nodes, err)
	}
	a, _ := device.ParseAddress(simulator.CounterNode, "Int32", 1)
	bad, _ := device.ParseAddress("ns=2;s=Missing", "Auto", 1)
	rr, err := c.Read(ctx, []device.Address{a, bad})
	if err != nil || len(rr) != 2 || rr[0].Value.Integer != "42" || rr[1].Quality != "BAD" {
		t.Fatalf("%+v %v", rr, err)
	}
	v, _ := device.ParseValue(a, "123")
	wr, err := c.Write(ctx, a, v)
	if err != nil || wr.State != "CONFIRMED" {
		t.Fatalf("%+v %v", wr, err)
	}
	rr, err = c.Read(ctx, []device.Address{a})
	if err != nil || rr[0].Value.Integer != "123" {
		t.Fatalf("%+v %v", rr, err)
	}
	ro, _ := device.ParseAddress("ns=2;s=ReadOnly", "Int32", 1)
	wr, err = c.Write(ctx, ro, v)
	if err == nil || wr.State != "REJECTED" {
		t.Fatalf("readonly %+v %v", wr, err)
	}
}
func TestSecuritySelectionDoesNotDowngrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := simulator.Start(ctx, "opc.tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	o := DefaultOptions()
	o.Policy = "Basic256Sha256"
	o.Mode = "SignAndEncrypt"
	if c, err := Dial(ctx, s.Endpoint, o); err == nil {
		c.Close()
		t.Fatal("secure selection downgraded to None")
	}
}
