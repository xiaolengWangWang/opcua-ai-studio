package opcua

import (
	"context"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"opcua-ai-studio/internal/device"
	"testing"
)

func TestClientTypeMismatchPreservesServerGoodQuality(t *testing.T) {
	_, ns, target := testServer(t, nil, server.EnableSecurity("None", ua.MessageSecurityModeNone))
	node := ns.AddNode(server.NewVariableNode(ua.NewStringNodeID(ns.ID(), "Counter"), "Counter", int32(42)))
	c, err := Dial(context.Background(), target, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, _ := device.ParseAddress(node.ID().String(), "String", 1)
	rr, err := c.Read(context.Background(), []device.Address{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr) != 1 || rr[0].Quality != "GOOD" || rr[0].StatusCode != 0 || rr[0].Value.Integer != "42" || rr[0].Error == "" {
		t.Fatalf("client mismatch overwrote server DataValue: %+v", rr)
	}
}
