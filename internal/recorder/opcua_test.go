package recorder

import (
	"opcua-ai-studio/internal/device"
	"path/filepath"
	"testing"
	"time"
)

func TestOPCUANodeMetadataAndZeroValues(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.StartSession(device.ProtocolOPCUA, "opc.tcp://127.0.0.1:4840", 1)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := device.ParseAddress("ns=2;s=Enabled", "Boolean", 1)
	r.Record(id, device.Event{Time: time.Now(), Protocol: device.ProtocolOPCUA, Dir: device.DirRX, Operation: "READ", Results: []device.EventResult{{Read: &device.ReadResult{Address: a, Quality: "GOOD", Value: device.Value{Type: "Boolean", Boolean: false}}}}})
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(filepath.Join(filepath.Dir(r.path), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pp, err := r.Packets(id, 10)
	if err != nil || len(pp) != 1 || pp[0].Results[0].Read.Address.NodeID != a.NodeID || pp[0].Results[0].Read.Value.Boolean {
		t.Fatalf("%+v %v", pp, err)
	}
}
