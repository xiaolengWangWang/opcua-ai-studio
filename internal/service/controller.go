package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"opcua-ai-studio/internal/device"
	"os"
	"path/filepath"

	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/recorder"
	"opcua-ai-studio/internal/simulator"
	"sync"
	"sync/atomic"
	"time"
)

// packetPoints bounds the targets and results a packet log keeps per request.
const packetPoints = 100

type Options struct {
	RecordDir string
	Dial      func(context.Context, string, opcua.Options) (device.Driver, error)
}
type Snapshot struct {
	ScanStatus, BrowseStatus                                                       string
	Revision, Generation                                                           uint64
	State, Target, Family, DemoTarget, LastError, RecordDir, RecordError, Readback string
	Polling, AutoRefresh                                                           bool // AutoRefresh: the poll refreshes the shown points
	Results                                                                        []device.ReadResult
	Nodes                                                                          []device.Node
	WriteResult                                                                    *device.WriteResult
	Packets                                                                        []device.Event
	Logs                                                                           []string
	Dropped                                                                        int64
	Reference                                                                      *device.ReadResult // the reference point's last read
	Server                                                                         *opcua.ServerInfo
}
type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	d      device.Driver
	gate   chan struct{}
	wg     sync.WaitGroup
	end    sync.Once
	id     int64
	cfg    Config
	misses atomic.Int32 // timeouts and failed heartbeats in a row
}
type Controller struct {
	mu         sync.Mutex
	ops        sync.WaitGroup
	closed     bool
	once       sync.Once
	closeErr   error
	snap       Snapshot
	s          *session
	dial       func(context.Context, string, opcua.Options) (device.Driver, error)
	rec        *recorder.Recorder
	pollCancel context.CancelFunc
	pollSeq    uint64
	poll       pollSpec // the running poll, resumed after a reconnect
	demo       *simulator.Server
	reference  *Point
	life       context.Context // ends when the controller closes
	stop       context.CancelFunc
}

func New(o Options) *Controller {
	if o.RecordDir == "" {
		dir, e := os.UserConfigDir()
		if e == nil {
			o.RecordDir = filepath.Join(dir, "OPCUAAIStudio", "records")
		}
	}
	c := &Controller{snap: Snapshot{Revision: 1, State: "disconnected", RecordDir: o.RecordDir}, dial: o.Dial}
	c.life, c.stop = context.WithCancel(context.Background())
	if c.dial == nil {
		c.dial = func(ctx context.Context, target string, o opcua.Options) (device.Driver, error) {
			return opcua.Dial(ctx, target, o)
		}
	}
	var e error
	if o.RecordDir == "" {
		e = fmt.Errorf("无法确定记录目录")
	} else {
		c.rec, e = recorder.OpenDaily(o.RecordDir)
	}
	if e != nil {
		c.snap.RecordError = e.Error()
	}
	return c
}
func (c *Controller) logLocked(text string) {
	c.snap.Logs = append(c.snap.Logs, time.Now().Format("15:04:05")+" "+text)
	if len(c.snap.Logs) > 100 {
		c.snap.Logs = c.snap.Logs[len(c.snap.Logs)-100:]
	}
	c.snap.Revision++
}
func (c *Controller) begin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("程序已关闭")
	}
	c.ops.Add(1)
	return nil
}
func (c *Controller) healthLocked() {
	if c.rec != nil {
		d, e := c.rec.Health()
		msg := c.snap.RecordError
		if e != nil {
			msg = e.Error()
		}
		if d > 0 && e == nil {
			msg = fmt.Sprintf("记录丢弃 %d 条报文", d)
		}
		if d != c.snap.Dropped || msg != c.snap.RecordError {
			c.snap.Dropped = d
			c.snap.RecordError = msg
			c.snap.Revision++
		}
	}
}
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthLocked()
	s := c.snap
	// Results and Nodes are copy-on-write: writers replace the slices, never
	// their elements, so snapshots share them. Packets are cloned on entry and
	// never mutated, so copying the slice suffices. Deep cloning 50000 points
	// per UI refresh held the lock long enough to stall reads.
	s.Logs = append([]string(nil), s.Logs...)
	s.Packets = append([]device.Event(nil), s.Packets...)
	if s.WriteResult != nil {
		r := *s.WriteResult
		r.Segments = append([]device.WriteSegment(nil), r.Segments...)
		s.WriteResult = &r
	}
	return s
}
func (c *Controller) recordError(e error) {
	if e == nil {
		return
	}
	c.mu.Lock()
	c.snap.RecordError = e.Error()
	c.snap.Revision++
	c.mu.Unlock()
}
func (c *Controller) end(s *session) {
	if s == nil {
		return
	}
	s.end.Do(func() {
		if c.rec != nil && s.id != 0 {
			c.recordError(c.rec.EndSession(s.id))
		}
	})
}
func (c *Controller) stopPollLocked() {
	if c.pollCancel != nil {
		c.pollCancel()
		c.pollCancel = nil
	}
	c.pollSeq++
	c.snap.Polling = false
	c.snap.AutoRefresh = false
	c.snap.Revision++
}
func (c *Controller) detachLocked(state string) *session {
	s := c.s
	c.s = nil
	c.stopPollLocked()
	c.snap.Generation++
	c.snap.State = state
	c.snap.Revision++
	if s != nil {
		s.cancel()
	}
	return s
}
func (c *Controller) finishSession(s *session) {
	if s == nil {
		return
	}
	if s.d != nil {
		s.d.Close()
	}
	s.wg.Wait()
	c.end(s)
}
func (c *Controller) Disconnect() error {
	if e := c.begin(); e != nil {
		return e
	}
	defer c.ops.Done()
	c.mu.Lock()
	s := c.detachLocked("disconnected")
	c.logLocked("已断开连接")
	c.mu.Unlock()
	c.finishSession(s)
	return nil
}
func (c *Controller) Connect(ctx context.Context, cfg Config) error {
	_, err := c.connect(ctx, cfg, 0)
	return err
}

// connect opens a session and returns its generation. A reconnect passes the
// generation of the lost session as resume: the attempt is dropped if the
// user connected or disconnected since, and the results are kept.
func (c *Controller) connect(ctx context.Context, cfg Config, resume uint64) (uint64, error) {
	o, e := cfg.validate()
	if e != nil {
		return 0, e
	}
	if e = c.begin(); e != nil {
		return 0, e
	}
	defer c.ops.Done()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, errors.New("程序已关闭")
	}
	if resume != 0 && c.snap.Generation != resume {
		c.mu.Unlock()
		return 0, context.Canceled
	}
	old := c.detachLocked("connecting")
	gen := c.snap.Generation
	sctx, cancel := context.WithCancel(ctx)
	s := &session{ctx: sctx, cancel: cancel, gate: make(chan struct{}, 1), cfg: cfg}
	s.gate <- struct{}{}
	s.wg.Add(1)
	defer s.wg.Done()
	if c.rec != nil {
		s.id, e = c.rec.StartSession(device.ProtocolOPCUA, cfg.Target, 1)
		if e != nil {
			c.snap.RecordError = e.Error()
		}
	}
	c.s = s
	c.snap.Target = cfg.Target
	c.snap.Family = "OPC UA"
	c.snap.Reference = nil
	c.snap.Server = nil
	if resume == 0 {
		c.snap.LastError = ""
		c.snap.Results = nil
		c.snap.ScanStatus = ""
		c.snap.BrowseStatus = ""
		c.snap.Nodes = nil
		c.snap.WriteResult = nil
		c.snap.Readback = ""
	}
	c.logLocked("正在连接 " + cfg.Target)
	c.mu.Unlock()
	c.finishSession(old)
	o.Observer = device.ObserverFunc(func(event device.Event) {
		event.Family = "OPC UA"
		// A bulk request of 5000 points would log megabytes and slow the read
		// it records; its results stay complete in the result table.
		if n := len(event.Targets); n > packetPoints {
			event.Total = n
			event.Targets = event.Targets[:packetPoints]
			event.Results = event.Results[:min(len(event.Results), packetPoints)]
		}
		if c.rec != nil && s.id != 0 {
			c.rec.Record(s.id, event)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.s != s {
			return
		}
		c.snap.Packets = append(c.snap.Packets, event.Clone())
		if len(c.snap.Packets) > 256 {
			c.snap.Packets = c.snap.Packets[len(c.snap.Packets)-256:]
		}
		c.snap.Revision++
	})
	d, e := c.dial(sctx, cfg.Target, o)
	c.mu.Lock()
	if c.closed || c.s != s {
		c.mu.Unlock()
		if d != nil {
			d.Close()
		}
		return gen, context.Canceled
	}
	if e != nil {
		c.snap.State = "error"
		c.snap.LastError = e.Error()
		c.logLocked("连接失败: " + e.Error())
		s.cancel()
		c.mu.Unlock()
		c.end(s)
		return gen, e
	}
	s.d = d
	c.snap.State = "connected"
	c.snap.LastError = ""
	c.logLocked("OPC UA 会话已连接")
	s.wg.Add(1)
	go c.watch(s)
	c.mu.Unlock()
	c.describe(ctx)
	return gen, nil
}
func (c *Controller) acquire(ctx context.Context, generation uint64) (*session, context.Context, func(), error) {
	c.mu.Lock()
	s := c.s
	if c.closed || s == nil || s.d == nil || c.snap.State != "connected" {
		c.mu.Unlock()
		return nil, nil, nil, errors.New("请先连接")
	}
	if generation != 0 && generation != c.snap.Generation {
		c.mu.Unlock()
		return nil, nil, nil, errors.New("连接已变化，请重新确认写入")
	}
	c.ops.Add(1)
	s.wg.Add(1)
	c.mu.Unlock()
	opctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	done := func() { stop(); cancel(); s.wg.Done(); c.ops.Done() }
	select {
	case <-opctx.Done():
		done()
		return nil, nil, nil, opctx.Err()
	case <-s.gate:
	}
	c.mu.Lock()
	valid := !c.closed && c.s == s && c.snap.State == "connected"
	c.mu.Unlock()
	if !valid || opctx.Err() != nil {
		s.gate <- struct{}{}
		done()
		return nil, nil, nil, context.Canceled
	}
	return s, opctx, func() { s.gate <- struct{}{}; done() }, nil
}
func (c *Controller) failure(s *session, e error) {
	if e == nil {
		return
	}
	if state, ok := s.d.(interface{ IsClosed() bool }); ok && !state.IsClosed() {
		// A silent network drop leaves the socket open: requests time out.
		if !timedOut(e) || s.misses.Add(1) < heartbeatMisses {
			return
		}
		e = fmt.Errorf("服务器连续 %d 次未响应: %w", heartbeatMisses, e)
	}
	c.lost(s, e)
}

func timedOut(e error) bool {
	return errors.Is(e, context.DeadlineExceeded) || errors.Is(e, ua.StatusBadTimeout) || errors.Is(e, ua.StatusBadRequestTimeout)
}
func (c *Controller) Close() error {
	c.once.Do(func() {
		c.stop()
		c.mu.Lock()
		c.closed = true
		s := c.detachLocked("closed")
		demo := c.demo
		c.demo = nil
		c.snap.DemoTarget = ""
		c.mu.Unlock()
		c.finishSession(s)
		if demo != nil {
			demo.Close()
		}
		c.ops.Wait()
		if c.rec != nil {
			c.closeErr = c.rec.Close()
		}
		c.mu.Lock()
		c.healthLocked()
		if c.snap.RecordError != "" {
			c.closeErr = errors.Join(c.closeErr, errors.New(c.snap.RecordError))
		}
		c.mu.Unlock()
	})
	return c.closeErr
}
