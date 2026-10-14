package service

import (
	"context"
	"errors"
	"fmt"
	"opcua-ai-studio/internal/device"
	"time"
)

func (c *Controller) Read(ctx context.Context, points []Point) error {
	return c.read(ctx, points, 0)
}

func (c *Controller) read(ctx context.Context, points []Point, generation uint64) error {
	s, ctx, done, e := c.acquire(ctx, generation)
	if e != nil {
		return e
	}
	defer done()
	aa, e := resolve(points)
	if e != nil {
		return e
	}
	rr, e := s.d.Read(ctx, aa)
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.Results = rr
		c.snap.LastError = ""
		if e != nil {
			c.snap.LastError = e.Error()
		}
		c.snap.Revision++
	}
	c.mu.Unlock()
	if e != nil {
		c.failure(s, e)
	}
	return e
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
		if e == nil && len(rr) == 1 && rr[0].Quality == "GOOD" {
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
			c.snap.Results = rr
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
func (c *Controller) StartPolling(ctx context.Context, points []Point, interval time.Duration) error {
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
	seq, gen := c.pollSeq, c.snap.Generation
	c.snap.Polling = true
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
			if c.read(ctx, points, gen) != nil {
				return
			}
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
