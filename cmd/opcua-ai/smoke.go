package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/url"
	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/recorder"
	"opcua-ai-studio/internal/service"
	"opcua-ai-studio/internal/simulator"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// chineseText is written to a String point and must read back and export
// unchanged, full-width punctuation and symbols included.
const chineseText = "一号换热站 正常运行，供水温度95℃"

// view sets the result search, switches between the table and the tree and
// selects a tab.
func runSmoke(ctx context.Context, c *service.Controller, capture func() image.Image, export func(io.Writer, string) error, view func(search string, tree bool, tab int), dir string) error {
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
	if s.Server == nil || s.Server.Product == "" || !slices.Contains(s.Server.Namespaces, "urn:opcua-ai-studio:demo") {
		return fmt.Errorf("服务器信息读取失败")
	}
	c.SetReference(&service.Point{Address: simulator.CounterNode, Type: "Auto", Count: 1})
	if err := c.CheckReference(ctx); err != nil {
		return err
	}
	if r := c.Snapshot().Reference; r == nil || r.Quality != "GOOD" {
		return fmt.Errorf("基准点位检测失败")
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
	// Chinese string identifiers read, write and read back like any other.
	var chinese []service.Point
	for _, n := range simulator.ChineseNodes {
		chinese = append(chinese, service.Point{Address: n, Type: "Auto", Count: 1})
	}
	if err := c.Read(ctx, chinese); err != nil {
		return err
	}
	s = c.Snapshot()
	if len(s.Results) != 3 || s.Results[0].Value.Real != 95.5 || s.Results[2].Value.Text != "一号换热站" {
		return fmt.Errorf("中文点位读取失败")
	}
	pump := service.Point{Address: simulator.ChineseNodes[1], Type: "Boolean", Count: 1}
	if err := c.Write(ctx, service.WriteRequest{Point: pump, Value: "true", Generation: s.Generation}, true); err != nil {
		return err
	}
	if s = c.Snapshot(); s.WriteResult == nil || s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" {
		return fmt.Errorf("中文点位写入/读回验证失败")
	}
	message := service.Point{Address: "ns=2;s=Message", Type: "String", Count: 1}
	if err := c.Write(ctx, service.WriteRequest{Point: message, Value: chineseText, Generation: s.Generation}, true); err != nil {
		return err
	}
	if s = c.Snapshot(); s.WriteResult == nil || s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" {
		return fmt.Errorf("中文文字写入/读回验证失败")
	}
	if err := c.ReadAll(ctx, "ns=2;i=85"); err != nil {
		return err
	}
	if len(c.Snapshot().Results) != 9 {
		return fmt.Errorf("全量读取没有取得九个演示变量")
	}
	for _, r := range c.Snapshot().Results {
		if len(r.Path) == 0 {
			return fmt.Errorf("全量读取没有记录浏览路径")
		}
	}
	if err := c.SearchNodes(ctx, "ns=2;i=85", func(n device.Node) bool { return strings.Contains(n.NodeID, "热力站") }); err != nil {
		return err
	}
	if n := len(c.Snapshot().Nodes); n != 3 {
		return fmt.Errorf("节点搜索找到 %d 个中文点位", n)
	}
	if err := c.VerifyPoint(ctx, service.Point{Address: "ns=2;s=Missing", Type: "Auto", Count: 1}); err != nil {
		return err
	}
	s = c.Snapshot()
	good, bad := 0, 0
	counter := ""
	for _, r := range s.Results {
		if r.Quality == "GOOD" {
			good++
		}
		if r.Quality == "BAD" {
			bad++
		}
		if !r.Complete || r.TaskID == "" {
			return fmt.Errorf("任务范围信息缺失")
		}
		if r.Address.NodeID == simulator.CounterNode {
			counter = device.FormatValue(r.Value)
		}
	}
	if good != 9 || bad != 1 || counter != "123" {
		return fmt.Errorf("质量区分或单点结果保留失败")
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
	for _, filter := range []string{"全部", "BAD", "含中文"} {
		var buf bytes.Buffer
		if err = export(&buf, filter); err != nil {
			return err
		}
		rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(buf.String(), "\ufeff"))).ReadAll()
		if e != nil {
			return e
		}
		name := "points-all.csv"
		if filter == "含中文" {
			name = "points-chinese.csv"
			if len(rows) != 4 || rows[1][1] != simulator.ChineseNodes[0] || rows[1][16] != "只读" || rows[2][3] != "true" || rows[2][16] != "读写" {
				return fmt.Errorf("中文点位导出错误")
			}
		} else if filter == "BAD" {
			name = "points-bad.csv"
			if len(rows) != 2 || rows[1][1] != "ns=2;s=Missing" || rows[1][4] != "BAD" {
				return fmt.Errorf("BAD 导出错误")
			}
		} else if len(rows) != 11 {
			return fmt.Errorf("全量结果导出行数错误")
		} else if !slices.ContainsFunc(rows, func(r []string) bool { return r[1] == message.Address && r[3] == chineseText }) {
			return fmt.Errorf("中文文字值导出错误")
		}
		if err = os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0644); err != nil {
			return err
		}
	}
	// The search narrows the table, the tree and the export alike.
	for _, shot := range []struct {
		name string
		tab  int
	}{{"native-gui-tree.png", 0}, {"native-gui-packets.png", 2}} {
		view("热力站", true, shot.tab)
		if im = capture(); im == nil {
			return fmt.Errorf("没有截图 %s", shot.name)
		}
		if f, err = os.Create(filepath.Join(dir, shot.name)); err != nil {
			return err
		}
		err = png.Encode(f, im)
		if closeErr = f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	var searched bytes.Buffer
	if err = export(&searched, "全部"); err != nil {
		return err
	}
	if rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(searched.String(), string(rune(0xfeff))))).ReadAll(); e != nil || len(rows) != 4 {
		return fmt.Errorf("搜索结果导出错误: %d 行 %v", len(rows), e)
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
		GoodPoints, BadPoints                          int
		CSVVerified                                    bool
		Archives                                       []string
		Time                                           time.Time
	}{version, s.Target, "42", counter, s.WriteResult.State, true, true, packets, len(s.Nodes), good, bad, true, files, time.Now().UTC()}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "smoke.json"), b, 0644)
}
