package service

import (
	"context"
	"errors"
	"opcua-ai-studio/internal/simulator"
)

func (c *Controller) StartDemo(ctx context.Context) error {
	if err := c.begin(); err != nil {
		return err
	}
	defer c.ops.Done()
	c.mu.Lock()
	running := c.demo != nil
	c.mu.Unlock()
	if running {
		return errors.New("本机演示已启动")
	}
	srv, err := simulator.Start(context.Background(), "opc.tcp://127.0.0.1:0")
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed || c.demo != nil {
		c.mu.Unlock()
		srv.Close()
		return context.Canceled
	}
	c.demo = srv
	c.snap.DemoTarget = srv.Endpoint
	c.logLocked("本机 OPC UA 演示: " + srv.Endpoint)
	c.mu.Unlock()
	cfg := DefaultConfig()
	cfg.Target = srv.Endpoint
	if err = c.Connect(ctx, cfg); err != nil {
		return err
	}
	return c.Read(ctx, []Point{{Address: simulator.CounterNode, Type: "Auto", Count: 1}})
}
func (c *Controller) StopDemo() error {
	if err := c.begin(); err != nil {
		return err
	}
	defer c.ops.Done()
	c.mu.Lock()
	srv := c.demo
	var s *session
	if srv != nil && c.snap.Target == srv.Endpoint {
		s = c.detachLocked("disconnected")
	}
	c.demo = nil
	c.snap.DemoTarget = ""
	c.logLocked("本机演示已停止")
	c.mu.Unlock()
	c.finishSession(s)
	if srv != nil {
		return srv.Close()
	}
	return nil
}
