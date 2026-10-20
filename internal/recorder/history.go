package recorder

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"opcua-ai-studio/internal/device"
)

// MaxTotalBytes bounds all database files together, 40 files of 50 MB. Past
// it the oldest files are deleted, as the records serve recent troubleshooting.
var MaxTotalBytes int64 = 2_000_000_000

// historyFiles bounds how many files one history query opens.
const historyFiles = 48

// Line is one line of the run log.
type Line struct {
	Time    time.Time
	Session int64 // 0 when no connection was open
	Text    string
}

// Connection is one connection made: where, with which security and account,
// whether it succeeded, and what the server reported.
type Connection struct {
	Time         time.Time
	End          time.Time // zero while connected, or when it ended abnormally
	Session      int64
	Target       string
	Policy, Mode string
	User         string // empty for anonymous
	OK           bool
	Error        string
	Server       string // product and version
	Detail       string // the server information as JSON
	Reconnect    bool   // an automatic reconnect
	File         string `json:"-"` // the database file holding it
}

// Sample is one value of a point at a time, for trends.
type Sample struct {
	Time  time.Time
	Value float64
}

// block holds the samples of one point not yet written.
type block struct {
	target, node string
	samples      []Sample
}

// Samples are written in blocks per point: at most blockSamples, or the
// samples of blockAge, whichever comes first.
const (
	blockSamples = 240
	blockAge     = 5 * time.Minute
)

// enqueue queues a write without blocking; a full buffer drops it.
func (r *Recorder) enqueue(it item) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		r.Dropped.Add(1)
		return
	}
	select {
	case r.ch <- it:
	default:
		r.Dropped.Add(1)
	}
}

// Line records a run log line.
func (r *Recorder) Line(session int64, at time.Time, text string) {
	r.enqueue(item{session: session, line: &Line{Time: at, Session: session, Text: text}})
}

// Connect records a connection attempt.
func (r *Recorder) Connect(c Connection) {
	r.enqueue(item{session: c.Session, conn: &c})
}

// Sample adds one sample per point of target; points are written in
// compressed blocks.
func (r *Recorder) Sample(target string, values map[string]Sample) {
	var full []*block
	r.sampleMu.Lock()
	if r.blocks == nil {
		r.blocks = map[[2]string]*block{}
	}
	for node, s := range values {
		k := [2]string{target, node}
		b := r.blocks[k]
		if b == nil {
			b = &block{target: target, node: node}
			r.blocks[k] = b
		}
		b.samples = append(b.samples, s)
		if len(b.samples) >= blockSamples {
			full = append(full, b)
			delete(r.blocks, k)
		}
	}
	r.sampleMu.Unlock()
	for _, b := range full {
		r.enqueue(item{block: b})
	}
}

// takeBlocks removes the blocks older than blockAge, or all of them.
func (r *Recorder) takeBlocks(all bool) []item {
	r.sampleMu.Lock()
	defer r.sampleMu.Unlock()
	var out []item
	for k, b := range r.blocks {
		if all || time.Since(b.samples[0].Time) >= blockAge {
			out = append(out, item{block: b})
			delete(r.blocks, k)
		}
	}
	return out
}

// encodeSamples packs the times as varint millisecond steps from the first
// and each value as big-endian float64 bits XOR the previous value's, which
// leaves leading zero bytes for steady values for deflate to remove.
func encodeSamples(ss []Sample) []byte {
	out := make([]byte, 0, len(ss)*10)
	t0, v0 := ss[0].Time.UnixMilli(), uint64(0)
	for _, s := range ss {
		t := s.Time.UnixMilli()
		out = binary.AppendUvarint(out, uint64(max(t-t0, 0)))
		t0 = max(t, t0)
		bits := math.Float64bits(s.Value)
		out = binary.BigEndian.AppendUint64(out, bits^v0)
		v0 = bits
	}
	return out
}

func decodeSamples(data []byte, start int64, count int) ([]Sample, error) {
	out := make([]Sample, 0, count)
	t, v := start, uint64(0)
	for len(data) > 0 && len(out) < count {
		step, n := binary.Uvarint(data)
		if n <= 0 || len(data) < n+8 {
			return nil, errors.New("趋势数据已损坏")
		}
		t += int64(step)
		v ^= binary.BigEndian.Uint64(data[n:])
		data = data[n+8:]
		out = append(out, Sample{time.UnixMilli(t), math.Float64frombits(v)})
	}
	return out, nil
}

// insertExtra writes a log line, a connection or a block of samples in the
// batch's transaction.
func insertExtra(tx *sql.Tx, it item, series map[[2]string]int64) error {
	switch {
	case it.line != nil:
		_, err := tx.Exec(`INSERT INTO logs (time, session_id, text) VALUES (?, ?, ?)`, it.line.Time.UnixMilli(), it.line.Session, it.line.Text)
		return err
	case it.conn != nil:
		c := *it.conn
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		info, codec, err := encodeData(b)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO connections (time, session_id, target, info, info_codec) VALUES (?, ?, ?, ?, ?)`, c.Time.UnixMilli(), c.Session, c.Target, info, codec)
		return err
	case it.block != nil:
		b := it.block
		// Samples are encoded as steps forward in time; a block can hold
		// older samples after newer ones, as when the clock was set back.
		slices.SortStableFunc(b.samples, func(x, y Sample) int { return x.Time.Compare(y.Time) })
		k := [2]string{b.target, b.node}
		id, ok := series[k]
		if !ok {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO series (target, node) VALUES (?, ?)`, b.target, b.node); err != nil {
				return err
			}
			if err := tx.QueryRow(`SELECT id FROM series WHERE target = ? AND node = ?`, b.target, b.node).Scan(&id); err != nil {
				return err
			}
			series[k] = id
		}
		data, codec, err := encodeData(encodeSamples(b.samples))
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO samples (series_id, start_ms, end_ms, count, codec, data) VALUES (?, ?, ?, ?, ?, ?)`,
			id, b.samples[0].Time.UnixMilli(), b.samples[len(b.samples)-1].Time.UnixMilli(), len(b.samples), codec, data)
		return err
	}
	return nil
}

func hasTable(db *sql.DB, name string) bool {
	var n int
	return db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n) == nil && n > 0
}

// newestFiles lists the database files newest first.
func (r *Recorder) newestFiles() ([]string, error) {
	files, err := r.Files()
	slices.Reverse(files)
	return files, err
}

// eachFile opens the newest files read-only, up to historyFiles, until visit
// returns false; files without the table are skipped.
func (r *Recorder) eachFile(table string, visit func(path string, db *sql.DB) (bool, error)) error {
	files, err := r.newestFiles()
	if err != nil {
		return err
	}
	for _, f := range files[:min(len(files), historyFiles)] {
		db, err := readDatabase(f)
		if err != nil {
			return err
		}
		more, err := true, error(nil)
		if hasTable(db, table) {
			more, err = visit(f, db)
		}
		db.Close()
		if err != nil {
			return fmt.Errorf("读取记录 %s：%w", f, err)
		}
		if !more {
			return nil
		}
	}
	return nil
}

// Lines returns up to limit run log lines, oldest first: one session's when
// session is not 0, else the latest of all.
func (r *Recorder) Lines(session int64, limit int) ([]Line, error) {
	var out []Line
	err := r.eachFile("logs", func(_ string, db *sql.DB) (bool, error) {
		query, args := `SELECT time, session_id, text FROM logs ORDER BY id DESC LIMIT ?`, []any{limit - len(out)}
		if session != 0 {
			query, args = `SELECT time, session_id, text FROM logs WHERE session_id = ? ORDER BY id DESC LIMIT ?`, []any{session, limit - len(out)}
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var l Line
			var t int64
			if err := rows.Scan(&t, &l.Session, &l.Text); err != nil {
				return false, err
			}
			l.Time = time.UnixMilli(t)
			out = append(out, l)
		}
		return len(out) < limit, rows.Err()
	})
	slices.Reverse(out)
	return out, err
}

// Connections returns up to limit connections, newest first.
func (r *Recorder) Connections(limit int) ([]Connection, error) {
	var out []Connection
	err := r.eachFile("connections", func(path string, db *sql.DB) (bool, error) {
		rows, err := db.Query(`SELECT c.time, c.session_id, c.target, c.info, c.info_codec, s.ended_at
			FROM connections c LEFT JOIN sessions s ON s.id = c.session_id ORDER BY c.time DESC, c.id DESC LIMIT ?`, limit-len(out))
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var c Connection
			var t int64
			var info []byte
			var codec int
			var end sql.NullInt64
			if err := rows.Scan(&t, &c.Session, &c.Target, &info, &codec, &end); err != nil {
				return false, err
			}
			if info, err = decodeData(info, codec); err != nil {
				return false, err
			}
			if err := json.Unmarshal(info, &c); err != nil {
				return false, err
			}
			c.Time, c.File = time.UnixMilli(t), path
			if end.Valid {
				c.End = time.UnixMilli(end.Int64)
			}
			out = append(out, c)
		}
		return len(out) < limit, rows.Err()
	})
	return out, err
}

// SessionPackets returns a session's last packets, up to limit, oldest
// first, across the files it was written to.
func (r *Recorder) SessionPackets(session int64, limit int) ([]device.Event, error) {
	var out []device.Event
	err := r.eachFile("packets", func(_ string, db *sql.DB) (bool, error) {
		pp, err := (&Recorder{db: db}).Packets(session, limit-len(out))
		out = append(pp, out...)
		return len(out) < limit, err
	})
	return out, err
}

// History returns the samples of nodes at target taken since the given time,
// oldest first: from the newest files back to that day, and from the blocks
// not written yet.
func (r *Recorder) History(target string, nodes []string, since time.Time) (map[string][]Sample, error) {
	out := map[string][]Sample{}
	if len(nodes) == 0 {
		return out, nil
	}
	day, from := since.Format("20060102"), since.UnixMilli()
	err := r.eachFile("samples", func(path string, db *sql.DB) (bool, error) {
		if d := r.fileDay(path); d != "" && d < day {
			return false, nil
		}
		for chunk := range slices.Chunk(nodes, 500) {
			args := []any{target, from}
			for _, n := range chunk {
				args = append(args, n)
			}
			rows, err := db.Query(`SELECT s.node, x.start_ms, x.count, x.codec, x.data FROM samples x JOIN series s ON s.id = x.series_id
				WHERE s.target = ? AND x.end_ms >= ? AND s.node IN (?`+strings.Repeat(", ?", len(chunk)-1)+`)`, args...)
			if err != nil {
				return false, err
			}
			for rows.Next() {
				var node string
				var start int64
				var count, codec int
				var data []byte
				if err = rows.Scan(&node, &start, &count, &codec, &data); err != nil {
					break
				}
				if data, err = decodeData(data, codec); err != nil {
					break
				}
				var ss []Sample
				if ss, err = decodeSamples(data, start, count); err != nil {
					break
				}
				out[node] = append(out[node], ss...)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return false, err
			}
		}
		return true, nil
	})
	want := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		want[n] = true
	}
	r.sampleMu.Lock()
	for k, b := range r.blocks {
		if k[0] == target && want[k[1]] {
			out[k[1]] = append(out[k[1]], b.samples...)
		}
	}
	r.sampleMu.Unlock()
	for node, ss := range out {
		slices.SortStableFunc(ss, func(a, b Sample) int { return a.Time.Compare(b.Time) })
		out[node] = slices.DeleteFunc(ss, func(s Sample) bool { return s.Time.Before(since) })
	}
	return out, err
}

// pruneFilesLocked deletes the oldest files while all of them exceed
// MaxTotalBytes; a file another program still has open is kept, and so are
// the files after it.
func (r *Recorder) pruneFilesLocked() {
	files, err := r.Files()
	if err != nil {
		return
	}
	sizes := make([]int64, len(files))
	var total int64
	for i, f := range files {
		for _, suffix := range []string{"", "-wal"} {
			if st, err := os.Stat(f + suffix); err == nil {
				sizes[i] += st.Size()
			}
		}
		total += sizes[i]
	}
	for i, f := range files {
		if total <= MaxTotalBytes || f == r.path || os.Remove(f) != nil {
			return
		}
		os.Remove(f + "-wal")
		os.Remove(f + "-shm")
		total -= sizes[i]
	}
}
