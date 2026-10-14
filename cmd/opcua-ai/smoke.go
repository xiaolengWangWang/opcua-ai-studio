package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/url"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/recorder"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/simulator"
	"os"
	"path/filepath"
	"time"
)

func runSmoke(ctx context.Context, c *service.Controller, capture func() image.Image, dir string) error {
	defer c.Close()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := c.StartDemo(ctx); err != nil {
		return err
	}
	s := c.Snapshot()
	if len(s.Results) != 1 || s.Results[0].Value.Integer != "42" {
		return fmt.Errorf("演示初值错误")
	}
	p := service.Point{Address: simulator.CounterNode, Type: "Int32", Count: 1}
	if err := c.Write(ctx, service.WriteRequest{Point: p, Value: "123", Generation: s.Generation}, true); err != nil {
		return err
	}
	s = c.Snapshot()
	if s.WriteResult == nil || s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" {
		return fmt.Errorf("写入/读回验证失败")
	}
	if err := c.Browse(ctx, "ns=2;i=85"); err != nil {
		return err
	}
	pp := []service.Point{p, {Address: "ns=2;s=Temperature", Type: "Auto", Count: 1}, {Address: "ns=2;s=Enabled", Type: "Auto", Count: 1}, {Address: "ns=2;s=Message", Type: "Auto", Count: 1}, {Address: "ns=2;s=Samples", Type: "Auto", Count: 1}}
	if err := c.Read(ctx, pp); err != nil {
		return err
	}
	s = c.Snapshot()
	for _, r := range s.Results {
		if r.Quality != "GOOD" {
			return fmt.Errorf("演示读取失败: %s", r.Error)
		}
	}
	im := capture()
	if im == nil {
		return fmt.Errorf("没有 GUI 截图")
	}
	f, err := os.Create(filepath.Join(dir, "native-gui.png"))
	if err != nil {
		return err
	}
	err = png.Encode(f, im)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = c.Close(); err != nil {
		return err
	}
	if state := c.Snapshot(); state.RecordError != "" || state.Dropped != 0 {
		return fmt.Errorf("记录未完整保存")
	}
	u, _ := url.Parse(s.DemoTarget)
	l, err := net.Listen("tcp", u.Host)
	if err != nil {
		return fmt.Errorf("演示端口未释放: %w", err)
	}
	l.Close()
	files, err := filepath.Glob(filepath.Join(s.RecordDir, "*.sqlite3"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("没有记录归档")
	}
	packets := 0
	for _, path := range files {
		r, e := recorder.Open(path)
		if e != nil {
			return e
		}
		ss, e := r.Sessions(10)
		if e != nil {
			r.Close()
			return e
		}
		for _, session := range ss {
			if session.End.IsZero() {
				r.Close()
				return fmt.Errorf("记录会话未结束")
			}
			packets += session.Packets
		}
		if e = r.Close(); e != nil {
			return e
		}
	}
	if packets < 10 {
		return fmt.Errorf("服务级记录不足: %d", packets)
	}
	report := struct {
		Version, Target, Initial, Readback, WriteState string
		Passed, PortReleased                           bool
		Packets, Nodes                                 int
		Archives                                       []string
		Time                                           time.Time
	}{version, s.Target, "42", device.FormatValue(s.Results[0].Value), s.WriteResult.State, true, true, packets, len(s.Nodes), files, time.Now().UTC()}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "smoke.json"), b, 0644)
}
