package service

import (
	"context"
	"errors"
	"fmt"
	"opcua-ai-studio/internal/device"
	"time"
)

func (c *Controller) Read(ctx context.Context, points []Point) error {
	c.StopPolling()
	return c.read(ctx, points, 0)
}

func (c *Controller) read(ctx context.Context, points []Point, generation uint64) error {
	return c.readMode(ctx, points, generation, readList)
}

// readKind says what a read does with the result list.
type readKind int

const (
	readList    readKind = iota // replaces it
	readSingle                  // merges one point, with its type and access
	readRefresh                 // merges new values into the shown points
)

func (c *Controller) readMode(ctx context.Context, points []Point, generation uint64, kind readKind) error {
	merge := kind != readList
	s, ctx, done, e := c.acquire(ctx, generation)
	if e != nil {
		return e
	}
	defer done()
	if generation != 0 {
		c.mu.Lock()
		polling := c.snap.Polling
		c.mu.Unlock()
		if !polling {
			return context.Canceled
		}
	}
	aa, e := resolve(points)
	if e != nil {
		return e
	}
	// A single point also reads its type and access, for the verification card.
	rr, e := readBatches(ctx, s.d, aa, kind == readSingle, nil)
	taskID := time.Now().UTC().Format("20060102T150405.000000000Z")
	scope := map[readKind]string{readList: "节点清单", readSingle: aa[0].NodeID, readRefresh: "自动刷新"}[kind]
	for i := range rr {
		rr[i].TaskID, rr[i].Scope, rr[i].Complete = taskID, scope, e == nil
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		names := make(map[string]string, len(c.snap.Nodes))
		for _, n := range c.snap.Nodes {
			if _, ok := names[n.NodeID]; !ok {
				names[n.NodeID] = n.DisplayName
			}
		}
		for i := range rr {
			if name, ok := names[rr[i].Address.NodeID]; ok {
				rr[i].DisplayName = name
			}
		}
		if merge {
			c.mergeResultsLocked(rr)
		} else {
			c.snap.Results = rr
		}
		switch kind {
		case readList:
			c.snap.ScanStatus = fmt.Sprintf("读取完成 · %d 点", len(aa))
		case readSingle:
			c.snap.ScanStatus = "单点验证完成 · " + aa[0].NodeID
		case readRefresh:
			c.snap.ScanStatus = fmt.Sprintf("自动刷新 · %d 点 · %s", len(aa), time.Now().Format("15:04:05"))
		}
		if e != nil {
			c.snap.ScanStatus = "读取未完成：" + e.Error()
		}
		c.snap.LastError = ""
		if e != nil {
			c.snap.LastError = e.Error()
		}
		c.snap.Revision++
	}
	c.mu.Unlock()
	if e == nil {
		s.misses.Store(0)
	} else if !errors.Is(e, context.Canceled) {
		c.failure(s, e)
	}
	return e
}

// mergeResultsLocked replaces the listed points' results in a copy, keeping
// the rest of the list, earlier names and attributes; results are copy-on-write.
func (c *Controller) mergeResultsLocked(rr []device.ReadResult) {
	results := append([]device.ReadResult(nil), c.snap.Results...)
	at := make(map[string]int, len(results))
	for i := len(results) - 1; i >= 0; i-- {
		at[results[i].Address.NodeID] = i
	}
	for _, r := range rr {
		i, ok := at[r.Address.NodeID]
		if !ok {
			at[r.Address.NodeID] = len(results)
			results = append(results, r)
			continue
		}
		if r.DisplayName == "" {
			r.DisplayName = results[i].DisplayName
		}
		if r.Path == nil {
			r.Path = results[i].Path
		}
		if r.NodeAttributes == (device.NodeAttributes{}) {
			r.NodeAttributes = results[i].NodeAttributes
		}
		results[i] = r
	}
	c.snap.Results = results
}

func (c *Controller) Write(ctx context.Context, req WriteRequest, approved bool) error {
	if !approved {
		return errors.New("写入默认禁用，须明确允许并确认")
	}
	if req.Generation == 0 {
		return errors.New("写入须绑定已确认的连接")
	}
	s, ctx, done, e := c.acquire(ctx, req.Generation)
	if e != nil {
		return e
	}
	defer done()
	aa, e := resolve([]Point{req.Point})
	if e != nil {
		return e
	}
	a := aa[0]
	v, e := device.ParseValue(a, req.Value)
	if e != nil {
		return e
	}
	result, e := s.d.Write(ctx, a, v)
	readback := "无法确认"
	var rr []device.ReadResult
	writeErr := e
	if result.State == "CONFIRMED" {
		rr, e = s.d.Read(ctx, []device.Address{a})
		if e == nil && len(rr) == 1 && rr[0].Quality == "GOOD" && rr[0].Error == "" {
			readback = "不一致"
			if device.ValueEqual(v, rr[0].Value) {
				readback = "一致"
			}
		}
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.WriteResult = &result
		c.snap.Readback = readback
		if len(rr) > 0 {
			c.mergeResultsLocked(rr)
		}
		c.snap.LastError = ""
		if e != nil {
			c.snap.LastError = e.Error()
		}
		c.logLocked(fmt.Sprintf("写入 %s: %s；读回%s", req.Point.Address, result.State, readback))
	}
	c.mu.Unlock()
	if e != nil {
		c.failure(s, e)
	}
	return errors.Join(writeErr, e)
}

// StartPolling reads the points every interval, replacing the result list.
func (c *Controller) StartPolling(ctx context.Context, points []Point, interval time.Duration) error {
	return c.startPoll(ctx, points, interval, readList)
}

// StartRefresh reads the points in the result list every interval and merges
// their new values, keeping each point's name, browse path, type and access.
func (c *Controller) StartRefresh(ctx context.Context, interval time.Duration) error {
	c.mu.Lock()
	rr := c.snap.Results
	c.mu.Unlock()
	if len(rr) == 0 {
		return errors.New("结果中没有可刷新的点位，请先读取")
	}
	points := make([]Point, len(rr))
	for i, r := range rr {
		points[i] = Point{r.Address.NodeID, r.Address.Type, r.Address.Count}
	}
	return c.startPoll(ctx, points, interval, readRefresh)
}

func (c *Controller) startPoll(ctx context.Context, points []Point, interval time.Duration, kind readKind) error {
	if interval < 100*time.Millisecond {
		return errors.New("轮询间隔不得小于 100 ms")
	}
	c.mu.Lock()
	if c.closed || c.s == nil || c.snap.State != "connected" {
		c.mu.Unlock()
		return errors.New("请先连接")
	}
	if _, e := resolve(points); e != nil {
		c.mu.Unlock()
		return e
	}
	c.stopPollLocked()
	pctx, cancel := context.WithCancel(ctx)
	c.pollCancel = cancel
	c.poll = pollSpec{append([]Point(nil), points...), interval, kind}
	seq, gen := c.pollSeq, c.snap.Generation
	c.snap.Polling = true
	c.snap.AutoRefresh = kind == readRefresh
	c.snap.Revision++
	c.ops.Add(1)
	c.mu.Unlock()
	points = append([]Point(nil), points...)
	go func() {
		defer c.ops.Done()
		defer func() {
			c.mu.Lock()
			if c.pollSeq == seq {
				c.snap.Polling = false
				c.snap.AutoRefresh = false
				c.pollCancel = nil
				c.snap.Revision++
			}
			c.mu.Unlock()
		}()
		for {
			c.mu.Lock()
			valid := c.snap.Generation == gen && !c.closed
			c.mu.Unlock()
			if !valid || pctx.Err() != nil {
				return
			}
			// Stopping polling cancels scheduling, while an in-flight read may finish.
			// Disconnect and shutdown still cancel the session and its active read.
			// A failed read shows in the results and polling goes on: a lost
			// connection stops it, and a reconnect resumes it.
			c.readMode(ctx, points, gen, kind)
			timer := time.NewTimer(interval)
			select {
			case <-pctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return nil
}
func (c *Controller) StopPolling() { c.mu.Lock(); c.stopPollLocked(); c.mu.Unlock() }
