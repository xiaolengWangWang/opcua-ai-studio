package recorder

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"opcua-ai-studio/internal/device"
)

// fakeDay replaces the clock used for daily file names; the background writer reads it too.
func fakeDay(t *testing.T, start time.Time) func(time.Duration) {
	var clock atomic.Int64
	clock.Store(start.UnixNano())
	now = func() time.Time { return time.Unix(0, clock.Load()) }
	t.Cleanup(func() { now = time.Now })
	return func(d time.Duration) { clock.Add(int64(d)) }
}

func TestDailyFilesRollOverAtMidnight(t *testing.T) {
	advance := fakeDay(t, time.Date(2026, 10, 10, 23, 59, 0, 0, time.Local))
	dir := t.TempDir()
	// 不相关的文件不算在内
	for _, name := range []string{"packets.db", "packets-000001.db", "packets-2026101-001.sqlite3", "notes.sqlite3"} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o600)
	}
	r, err := OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(r.Path()); got != "packets-20261010-001.sqlite3" {
		t.Fatalf("当天第一个文件 = %s", got)
	}
	id, err := r.StartSession(device.ProtocolModbusTCP, "plc:502", 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Record(id, device.Event{Time: time.Now(), RequestID: 1, Dir: device.DirTX, Protocol: device.ProtocolModbusTCP, Status: device.StatusSent, Raw: []byte{1, 3}})

	// 过了零点：下一次写入换到新日期的文件，跨零点的会话照常记录和结束
	advance(2 * time.Minute)
	if err := r.Log(id, Event{Kind: EventDisconnect, Detail: "断开"}); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(r.Path()); got != "packets-20261011-001.sqlite3" {
		t.Fatalf("零点后的文件 = %s", got)
	}
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
	defer r.Close()
	if got := filepath.Base(r.Path()); got != "packets-20261011-001.sqlite3" {
		t.Fatalf("同一天重新打开应接着写 %s", got)
	}
	r.dbMu.Lock()
	err = r.rotateLocked() // 写满时换下一个编号
	r.dbMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Files()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	want := []string{"packets-20261010-001.sqlite3", "packets-20261011-001.sqlite3", "packets-20261011-002.sqlite3"}
	if !slices.Equal(names, want) {
		t.Fatalf("文件 = %v，应为 %v", names, want)
	}

	// 与桌面版同一种表结构：会话复制到零点后的文件里，报文和日志都查得到
	packets, events := 0, 0
	for _, f := range files[:2] {
		sessions, err := r.SessionsFile(f, 10)
		if err != nil || len(sessions) != 1 || sessions[0].ID != id {
			t.Fatalf("%s 的会话 %v %v", filepath.Base(f), sessions, err)
		}
		ps, err := r.PacketsFile(f, id, 10)
		if err != nil {
			t.Fatal(err)
		}
		es, err := r.EventsFile(f, id)
		if err != nil {
			t.Fatal(err)
		}
		packets, events = packets+len(ps), events+len(es)
	}
	if packets != 1 || events != 1 {
		t.Fatalf("报文 %d 条、日志 %d 条，应各 1 条", packets, events)
	}
}

func TestDailyOpenStartsNewDayFile(t *testing.T) {
	advance := fakeDay(t, time.Date(2026, 10, 10, 9, 0, 0, 0, time.Local))
	dir := t.TempDir()
	r, err := OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	advance(24 * time.Hour)
	r, err = OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := filepath.Base(r.Path()); got != "packets-20261011-001.sqlite3" {
		t.Fatalf("隔天打开应新建当天的文件，得到 %s", got)
	}
}

// Several program windows share the day's archive: each writes its own
// session while the others write, and nothing is dropped or lost.
func TestWindowsShareTheDailyArchive(t *testing.T) {
	dir := t.TempDir()
	const windows, packets = 2, 2000
	errs := make(chan error, windows)
	for w := range windows {
		go func() {
			r, err := OpenDaily(dir)
			if err != nil {
				errs <- err
				return
			}
			id, err := r.StartSession(device.ProtocolOPCUA, "opc.tcp://window"+string(rune('a'+w))+":4840", 1)
			if err != nil {
				r.Close()
				errs <- err
				return
			}
			for i := range packets {
				r.Record(id, device.Event{Time: time.Now(), RequestID: uint64(i), Protocol: device.ProtocolOPCUA, Dir: device.DirTX, Operation: "READ", Status: device.StatusSent})
				if i%200 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
			if err = r.EndSession(id); err == nil {
				if dropped, health := r.Health(); dropped != 0 || health != nil {
					err = fmt.Errorf("dropped %d, %v", dropped, health)
				}
			}
			if closeErr := r.Close(); err == nil {
				err = closeErr
			}
			errs <- err
		}()
	}
	for range windows {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	// Windows started at the same moment may each reserve a file; a later
	// one opens the day's latest. Every session is in one of them.
	r, err := OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	files, err := r.Files()
	if err != nil {
		t.Fatal(err)
	}
	var ss []Session
	for _, f := range files {
		s, err := r.SessionsFile(f, 10)
		if err != nil {
			t.Fatal(err)
		}
		ss = append(ss, s...)
	}
	if len(ss) != windows {
		t.Fatalf("sessions %+v", ss)
	}
	for _, s := range ss {
		if s.Packets != packets || s.End.IsZero() {
			t.Fatalf("session %+v", s)
		}
	}
	// A window opened later writes into the same file as the others.
	other, err := OpenDaily(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.Path() != r.Path() {
		t.Fatalf("later windows use %s and %s", r.Path(), other.Path())
	}
}
