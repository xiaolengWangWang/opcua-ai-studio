package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"opcua-ai-studio/internal/device"
	"strings"
	"time"
)

// VerifyPoint updates one result without discarding the rest of the point list.
func (c *Controller) VerifyPoint(ctx context.Context, point Point) error {
	c.StopPolling()
	return c.readMode(ctx, []Point{point}, 0, true)
}

// ReadAll reads the variables reachable through hierarchical references below root.
func (c *Controller) ReadAll(ctx context.Context, root string) error {
	rootAddress, err := device.ParseAddress(root, "Auto", 1)
	if err != nil {
		return err
	}
	c.StopPolling()
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	var nodes []device.Node
	var points []Point
	taskID := time.Now().UTC().Format("20060102T150405.000000000Z")
	complete := false
	update := func(results []device.ReadResult, status string) {
		for i := range results {
			if i < len(nodes) {
				results[i].DisplayName = nodes[i].DisplayName
			}
			results[i].TaskID = taskID
			results[i].Scope = rootAddress.NodeID
			results[i].Complete = complete
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.s == s && !c.closed {
			c.snap.Results = append([]device.ReadResult(nil), results...)
			c.snap.Nodes = append([]device.Node(nil), nodes...)
			c.snap.ScanStatus = status
			c.snap.LastError = ""
			c.snap.Revision++
		}
	}
	update(nil, "浏览中 · 正在查找全部变量")
	queue := []string{rootAddress.NodeID}
	seen := map[string]bool{rootAddress.NodeID: true}
	for at := 0; at < len(queue); at++ {
		previousPoints := len(points)
		if err = ctx.Err(); err != nil {
			break
		}
		var children []device.Node
		children, err = s.d.Browse(ctx, queue[at])
		if err != nil {
			break
		}
		for _, n := range children {
			if !strings.HasSuffix(n.Class, "Object") && !strings.HasSuffix(n.Class, "Variable") {
				continue
			}
			a, e := device.ParseAddress(n.NodeID, "Auto", 1)
			if e != nil {
				err = e
				break
			}
			n.NodeID = a.NodeID
			if seen[n.NodeID] {
				continue
			}
			if len(seen) >= 10000 {
				err = fmt.Errorf("浏览超过 10000 个节点，请选择较小的父节点")
				break
			}
			seen[n.NodeID] = true
			queue = append(queue, n.NodeID)
			if strings.HasSuffix(n.Class, "Variable") {
				nodes = append(nodes, n)
				points = append(points, Point{n.NodeID, "Auto", 1})
			}
		}
		if previousPoints != len(points) || err != nil {
			update(nil, fmt.Sprintf("浏览中 · 已发现 %d 个变量", len(points)))
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		rr := make([]device.ReadResult, len(points))
		for i, p := range points {
			rr[i] = device.ReadResult{Address: device.Address{NodeID: p.Address, Type: "Auto", Count: 1}, Quality: "UNKNOWN", Execution: "NOT_READ", Error: "浏览未完成，尚未读取"}
		}
		update(rr, "浏览未完成："+err.Error())
		if !errors.Is(err, context.Canceled) {
			c.failure(s, err)
		}
		return err
	}
	if len(points) == 0 {
		update(nil, "未发现可读取的变量")
		return errors.New("父节点下未发现变量，请浏览并选择设备或文件夹节点")
	}
	aa, err := resolve(points)
	if err != nil {
		return err
	}
	rr, err := readBatches(ctx, s.d, aa, func(partial []device.ReadResult) {
		n := 0
		for _, r := range partial {
			if r.Execution != "NOT_READ" {
				n++
			}
		}
		update(partial, fmt.Sprintf("读取中 · %d / %d 个变量", n, len(aa)))
	})
	status := fmt.Sprintf("全量读取完成 · %d 个变量", len(aa))
	if err != nil {
		status = "全量读取未完成：" + err.Error()
	}
	complete = err == nil
	update(rr, status)
	if err != nil && !errors.Is(err, context.Canceled) {
		c.failure(s, err)
	}
	return err
}

func readBatches(ctx context.Context, d device.Driver, aa []device.Address, progress func([]device.ReadResult)) ([]device.ReadResult, error) {
	rr := make([]device.ReadResult, len(aa))
	for i, a := range aa {
		rr[i] = device.ReadResult{Address: a, Quality: "UNKNOWN", Execution: "NOT_READ", Error: "尚未读取"}
	}
	batchSize := 100
	for start := 0; start < len(aa); {
		if err := ctx.Err(); err != nil {
			return rr, err
		}
		end := min(start+batchSize, len(aa))
		batch, err := d.Read(ctx, aa[start:end])
		if (errors.Is(err, ua.StatusBadTooManyOperations) || errors.Is(err, ua.StatusBadEncodingLimitsExceeded)) && batchSize > 1 {
			batchSize = max(1, batchSize/2)
			continue
		}
		if err == nil && len(batch) != end-start {
			err = errors.New("服务器读取结果数量不一致")
		}
		if err != nil {
			if ctx.Err() == nil {
				for i := start; i < end; i++ {
					rr[i].Execution = "REQUEST_FAILED"
					rr[i].Error = err.Error()
					rr[i].Time = time.Now()
				}
			}
			if progress != nil {
				progress(rr)
			}
			return rr, err
		}
		copy(rr[start:end], batch)
		for i := start; i < end; i++ {
			if rr[i].Execution == "" {
				rr[i].Execution = "READ"
			}
		}
		if progress != nil {
			progress(rr)
		}
		start = end
	}
	return rr, nil
}
