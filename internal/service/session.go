package service

import (
	"context"
	"errors"
	"fmt"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"time"
)

// A silent server is declared lost after heartbeatMisses heartbeats in a
// row fail; automatic reconnects wait a growing delay between attempts.
const (
	heartbeatEvery  = 5 * time.Second
	heartbeatMisses = 2
	reconnectStep   = 2 * time.Second
	reconnectMax    = 30 * time.Second
)

type pollSpec struct {
	points   []Point
	interval time.Duration
	kind     readKind
}

// watch notices a connection lost between operations: the driver closes when
// the server drops the socket, and a server that stops answering fails its
// heartbeats. A heartbeat is skipped while an operation holds the session;
// that operation's timeouts count as missed heartbeats.
func (c *Controller) watch(s *session) {
	defer s.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		if state, ok := s.d.(interface{ IsClosed() bool }); ok && state.IsClosed() {
			c.lost(s, errors.New("服务器已断开连接"))
			return
		}
		p, ok := s.d.(interface{ Ping(context.Context) error })
		if !ok || tick%int(heartbeatEvery/time.Second) != 0 {
			continue
		}
		select {
		case <-s.gate:
		default:
			continue
		}
		err := p.Ping(s.ctx)
		s.gate <- struct{}{}
		if err == nil || s.ctx.Err() != nil {
			s.misses.Store(0)
			continue
		}
		if s.misses.Add(1) >= heartbeatMisses {
			c.lost(s, fmt.Errorf("服务器连续 %d 次未响应: %w", heartbeatMisses, err))
			return
		}
	}
}

// lost marks the session's connection lost and, with AutoReconnect, starts
// reconnecting with the same settings, resuming any polling.
func (c *Controller) lost(s *session, e error) {
	c.mu.Lock()
	if c.s == s && !c.closed && c.snap.State == "connected" {
		var poll *pollSpec
		if c.snap.Polling {
			poll = &pollSpec{c.poll.points, c.poll.interval, c.poll.kind}
		}
		c.snap.State = "error"
		c.snap.LastError = e.Error()
		c.stopPollLocked()
		s.cancel()
		if s.cfg.AutoReconnect {
			c.logLocked("连接中断，将自动重连: " + e.Error())
			c.ops.Add(1)
			go c.reconnect(s.cfg, c.snap.Generation, poll)
		} else {
			c.logLocked("通讯失败，需重新连接: " + e.Error())
		}
	}
	c.mu.Unlock()
	s.d.Close()
}

// reconnect retries a lost connection with growing delays until it succeeds,
// the program closes, or the user connects or disconnects meanwhile.
func (c *Controller) reconnect(cfg Config, gen uint64, poll *pollSpec) {
	defer c.ops.Done()
	for attempt := 1; ; attempt++ {
		delay := min(time.Duration(attempt)*reconnectStep, reconnectMax)
		c.mu.Lock()
		current := !c.closed && c.snap.Generation == gen
		if current {
			c.snap.State = "reconnecting"
			c.logLocked(fmt.Sprintf("%v 后第 %d 次自动重连 %s", delay, attempt, cfg.Target))
		}
		c.mu.Unlock()
		if !current || pause(c.life, delay) != nil {
			return
		}
		next, err := c.connect(c.life, cfg, gen)
		switch {
		case err == nil:
			if poll != nil {
				c.startPoll(c.life, poll.points, poll.interval, poll.kind)
			}
			return
		case errors.Is(err, context.Canceled):
			return
		}
		gen = next
	}
}

// SetReference sets the point read after every connection to tell whether the
// server still serves data, as a known good point does; nil clears it.
func (c *Controller) SetReference(p *Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reference = nil
	if p != nil {
		v := *p
		c.reference = &v
	}
	c.snap.Reference = nil
	c.snap.Revision++
}

// CheckReference reads the reference point without changing the result list.
func (c *Controller) CheckReference(ctx context.Context) error {
	c.mu.Lock()
	p := c.reference
	c.mu.Unlock()
	if p == nil {
		return errors.New("尚未设置基准点位")
	}
	aa, err := resolve([]Point{*p})
	if err != nil {
		return err
	}
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	rr, err := s.d.Read(ctx, aa)
	r := device.ReadResult{Address: aa[0], Quality: "UNKNOWN", Execution: "REQUEST_FAILED", Time: time.Now()}
	if err == nil && len(rr) == 1 {
		r = rr[0]
	} else if err != nil {
		r.Error = err.Error()
	}
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.Reference = &r
		c.snap.Revision++
	}
	c.mu.Unlock()
	if err != nil {
		c.failure(s, err)
	}
	return err
}

// RefreshServerInfo reads the server's status, build, namespaces and session
// diagnostics into the snapshot.
func (c *Controller) RefreshServerInfo(ctx context.Context) error {
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	d, ok := s.d.(interface {
		ServerInfo(context.Context) (opcua.ServerInfo, error)
	})
	if !ok {
		return errors.New("当前连接不提供服务器信息")
	}
	info, err := d.ServerInfo(ctx)
	if err == nil {
		c.mu.Lock()
		if c.s == s && !c.closed {
			c.snap.Server = &info
			c.snap.Revision++
		}
		c.mu.Unlock()
	} else {
		c.failure(s, err)
	}
	return err
}

// describe reads the server information and the reference point after a
// connection; neither failing fails the connection.
func (c *Controller) describe(ctx context.Context) {
	c.RefreshServerInfo(ctx)
	c.mu.Lock()
	check := c.reference != nil
	c.mu.Unlock()
	if check {
		c.CheckReference(ctx)
	}
}
