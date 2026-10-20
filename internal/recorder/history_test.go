package recorder

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opcua-ai-studio/internal/device"
)

// Steady values pack to a few bytes per sample and come back exactly, even
// when the clock steps back.
func TestSamplesPackSmallAndRoundTrip(t *testing.T) {
	start := time.UnixMilli(1_760_000_000_000)
	var ss []Sample
	for i := range blockSamples {
		ss = append(ss, Sample{start.Add(time.Duration(i) * 2 * time.Second), 95.5 + float64(i%3)*0.1})
	}
	ss[10].Time = ss[9].Time.Add(-time.Second)
	data := encodeSamples(ss)
	packed, codec, err := encodeData(data)
	if err != nil || codec != codecZlib {
		t.Fatalf("codec %d, %v", codec, err)
	}
	if per := float64(len(packed)) / float64(len(ss)); per > 3 {
		t.Fatalf("%.1f bytes per sample", per)
	}
	back, err := decodeSamples(data, ss[0].Time.UnixMilli(), len(ss))
	if err != nil || len(back) != len(ss) {
		t.Fatalf("%d samples, %v", len(back), err)
	}
	for i := range ss {
		want := ss[i].Time
		if i == 10 {
			want = ss[9].Time // a step back is stored as no step
		}
		if back[i].Value != ss[i].Value || !back[i].Time.Equal(want) {
			t.Fatalf("sample %d: %+v, want %+v", i, back[i], ss[i])
		}
	}
	if _, err := decodeSamples(data[:5], 0, 3); err == nil {
		t.Fatal("truncated samples decoded")
	}
}

// Trends, log lines, connections and complete bulk packets are kept in the
// database and read back after reopening.
func TestHistoryLogsConnectionsAndFullPacketsPersist(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := r
	t.Cleanup(func() { first.Close() })
	const target = "opc.tcp://plc:4840"
	start := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	for i := range 300 {
		at := start.Add(time.Duration(i) * 100 * time.Millisecond)
		r.Sample(target, map[string]Sample{"ns=2;s=A": {at, float64(i)}, "ns=2;s=B": {at, math.Pi}})
	}
	// The block still in memory is part of the history; the full one is
	// written within the writer's 200 ms.
	var h map[string][]Sample
	for range 20 {
		if h, err = r.History(target, []string{"ns=2;s=A", "ns=2;s=B", "ns=2;s=C"}, start); len(h["ns=2;s=A"]) == 300 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || len(h["ns=2;s=A"]) != 300 || len(h["ns=2;s=B"]) != 300 || len(h["ns=2;s=C"]) != 0 {
		t.Fatalf("history before writing: %d %d %v", len(h["ns=2;s=A"]), len(h["ns=2;s=B"]), err)
	}
	id, err := r.StartSession(device.ProtocolOPCUA, target, 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Line(id, time.Now(), "OPC UA 会话已连接")
	r.Line(0, time.Now(), "已断开连接")
	r.Connect(Connection{Time: time.Now(), Session: id, Target: target, Policy: "None", Mode: "None", User: "admin", OK: true, Server: "Prosys 5.4", Detail: strings.Repeat("{\"ns\":\"urn:x\"}", 50)})
	var targets []device.Address
	var results []device.EventResult
	for i := range 5000 {
		targets = append(targets, device.Address{NodeID: fmt.Sprintf("ns=3;s=Load.Station%03d.Tag%02d", i/100, i%100), Type: "Double", Count: 1})
		results = append(results, device.EventResult{Read: &device.ReadResult{Address: targets[i], Quality: "GOOD", Value: device.Value{Type: "Double", Real: 95.5}}})
	}
	r.Record(id, device.Event{Time: time.Now(), RequestID: 1, Dir: device.DirRX, Protocol: device.ProtocolOPCUA, Status: device.StatusSuccess, Operation: "READ", Targets: targets, Results: results})
	if err := r.EndSession(id); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	r, err = OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	h, err = r.History(target, []string{"ns=2;s=A"}, start.Add(10*time.Second))
	if a := h["ns=2;s=A"]; err != nil || len(a) != 200 || a[0].Value != 100 || a[199].Value != 299 {
		t.Fatalf("history after reopening: %d samples, %v", len(a), err)
	}
	lines, err := r.Lines(0, 10)
	if err != nil || len(lines) != 2 || lines[0].Text != "OPC UA 会话已连接" || lines[1].Session != 0 {
		t.Fatalf("lines %+v, %v", lines, err)
	}
	if mine, _ := r.Lines(id, 10); len(mine) != 1 {
		t.Fatalf("session lines %+v", mine)
	}
	cc, err := r.Connections(10)
	if err != nil || len(cc) != 1 || cc[0].User != "admin" || !cc[0].OK || cc[0].End.IsZero() || cc[0].Server != "Prosys 5.4" || cc[0].File == "" {
		t.Fatalf("connections %+v, %v", cc, err)
	}
	pp, err := r.SessionPackets(id, 10)
	if err != nil || len(pp) != 1 || len(pp[0].Targets) != 5000 || pp[0].Results[4999].Read.Value.Real != 95.5 {
		t.Fatalf("%d packets, %v", len(pp), err)
	}
	// The 5000 targets and results are stored compressed.
	db, _ := readDatabase(r.Path())
	defer db.Close()
	var size, codec int
	if err := db.QueryRow(`SELECT length(metadata), metadata_codec FROM packets`).Scan(&size, &codec); err != nil || codec != codecZlib || size > 60_000 {
		t.Fatalf("metadata %d bytes, codec %d, %v", size, codec, err)
	}
}

// Past the total limit the oldest files go; the current one stays.
func TestOldestFilesGoPastTheTotalLimit(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"packets-20261001-001.sqlite3", "packets-20261002-001.sqlite3", "packets-20261003-001.sqlite3"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 1000), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := MaxTotalBytes
	MaxTotalBytes = 2500
	t.Cleanup(func() { MaxTotalBytes = old })
	r := &Recorder{root: filepath.Join(dir, "packets.sqlite3"), daily: true}
	r.path = filepath.Join(dir, "packets-20261003-001.sqlite3")
	r.pruneFilesLocked()
	files, _ := r.Files()
	if len(files) != 2 || filepath.Base(files[0]) != "packets-20261002-001.sqlite3" {
		t.Fatalf("files left: %v", files)
	}
	MaxTotalBytes = 0
	r.pruneFilesLocked()
	if files, _ = r.Files(); len(files) != 1 || files[0] != r.path {
		t.Fatalf("the current file must stay: %v", files)
	}
}

// A block whose samples arrive out of time order is stored in order, so
// each keeps its own time.
func TestOutOfOrderSamplesKeepTheirTimes(t *testing.T) {
	r, err := OpenDaily(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().Truncate(time.Millisecond)
	r.Sample("opc.tcp://plc:4840", map[string]Sample{"n": {now, 1}})
	for i := range blockSamples - 1 {
		r.Sample("opc.tcp://plc:4840", map[string]Sample{"n": {now.Add(time.Duration(i-600) * time.Second), float64(i)}})
	}
	var h map[string][]Sample
	for range 20 {
		if h, err = r.History("opc.tcp://plc:4840", []string{"n"}, now.Add(-time.Hour)); len(h["n"]) == blockSamples {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	ss := h["n"]
	if err != nil || len(ss) != blockSamples || !ss[0].Time.Equal(now.Add(-600*time.Second)) || ss[0].Value != 0 || !ss[blockSamples-1].Time.Equal(now) {
		t.Fatalf("%d samples, first %+v, last %+v, %v", len(ss), ss[0], ss[len(ss)-1], err)
	}
}
