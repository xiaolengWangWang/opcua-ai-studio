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

// Large lists are read in paced batches: one request in flight and a gap
// between batches so the server keeps serving other clients. A batch doubles
// while the server answers quickly, since per-request overhead dominates
// (50000 points take about 1.5 s in batches of 2000 and 8 s in batches of 100),
// and halves for good when the server rejects one or cannot answer in time.
// Batches also stay below messageBudget: the SDK closes the session on a
// message above the negotiated size, 2 MB on many servers.
const (
	firstBatchSize   = 100
	maxBatchSize     = 5000
	messageBudget    = 512 << 10
	growBelow        = 500 * time.Millisecond
	readBatchGap     = 20 * time.Millisecond
	readRetryDelay   = 500 * time.Millisecond
	progressInterval = 250 * time.Millisecond
	// A full read lists at most this many variables, leaving room above the
	// 50000 points it is sized for; folders count toward the node limit.
	maxPoints = 60000
	maxNodes  = 100000
)

// VerifyPoint updates one result without discarding the rest of the point list.
func (c *Controller) VerifyPoint(ctx context.Context, point Point) error {
	c.StopPolling()
	return c.readMode(ctx, []Point{point}, 0, true)
}

// scanTask publishes one read task's results while they arrive.
type scanTask struct {
	c        *Controller
	s        *session
	id       string
	scope    string
	name     func(int) string
	nodes    *[]device.Node // replaces the browse list when set
	complete bool
}

func (c *Controller) newScan(s *session, scope string, name func(int) string, nodes *[]device.Node) *scanTask {
	return &scanTask{c: c, s: s, id: time.Now().UTC().Format("20060102T150405.000000000Z"), scope: scope, name: name, nodes: nodes}
}

func (t *scanTask) update(results []device.ReadResult, status string) {
	for i := range results {
		results[i].DisplayName = t.name(i)
		results[i].TaskID = t.id
		results[i].Scope = t.scope
		results[i].Complete = t.complete
	}
	c := t.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.s == t.s && !c.closed {
		c.snap.Results = append([]device.ReadResult(nil), results...)
		if t.nodes != nil {
			c.snap.Nodes = append([]device.Node(nil), *t.nodes...)
		}
		c.snap.ScanStatus = status
		c.snap.LastError = ""
		c.snap.Revision++
	}
}

// read reads aa in paced batches with each variable's type and access, and
// publishes the final state under label.
func (t *scanTask) read(ctx context.Context, aa []device.Address, label string) error {
	rr, err := readBatches(ctx, t.s.d, aa, true, func(partial []device.ReadResult, batchSize int) {
		n := 0
		for _, r := range partial {
			if r.Execution != "NOT_READ" {
				n++
			}
		}
		t.update(partial, fmt.Sprintf("读取中 · %d / %d 个变量 · 每批 %d", n, len(aa), batchSize))
	})
	status := fmt.Sprintf("%s完成 · %d 个变量", label, len(aa))
	if err != nil {
		status = label + "未完成：" + err.Error()
	}
	t.complete = err == nil
	t.update(rr, status)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.c.failure(t.s, err)
	}
	return err
}

// ReadAll reads the variables reachable through hierarchical references below root.
func (c *Controller) ReadAll(ctx context.Context, root string) error {
	return c.ReadNodes(ctx, []device.Node{{NodeID: root}})
}

// ReadNodes reads selected browse nodes: each Variable itself and the variables
// reachable through hierarchical references below every node.
func (c *Controller) ReadNodes(ctx context.Context, roots []device.Node) error {
	if len(roots) == 0 {
		return errors.New("请选择要读取的节点")
	}
	var nodes []device.Node
	var points []Point
	var level []string
	seen := map[string]bool{}
	for _, n := range roots {
		a, err := device.ParseAddress(n.NodeID, "Auto", 1)
		if err != nil {
			return err
		}
		if n.NodeID = a.NodeID; seen[n.NodeID] {
			continue
		}
		seen[n.NodeID] = true
		level = append(level, n.NodeID)
		if strings.HasSuffix(n.Class, "Variable") {
			nodes = append(nodes, n)
			points = append(points, Point{n.NodeID, "Auto", 1})
		}
	}
	scope := level[0]
	if len(level) > 1 {
		scope = fmt.Sprintf("%s 等 %d 个节点", level[0], len(level))
	}
	c.StopPolling()
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	t := c.newScan(s, scope, func(i int) string {
		if i < len(nodes) {
			return nodes[i].DisplayName
		}
		return ""
	}, &nodes)
	t.update(nil, "浏览中 · 正在查找全部变量")
	// Browse level by level, many nodes per request, so 50000 variables take
	// hundreds of round trips rather than one per node.
	var reported time.Time
	for len(level) > 0 && err == nil {
		var next []string
		err = paced(ctx, s.d, len(level), func(start, end, _ int) (int, error) {
			lists, err := browseNodes(ctx, s.d, level[start:end])
			if err != nil {
				return 0, err
			}
			// Pages are bounded by the driver; the request grows with the batch.
			size := 0
			for _, n := range level[start:end] {
				size += len(n) + 32
			}
			for _, children := range lists {
				for _, n := range children {
					variable := strings.HasSuffix(n.Class, "Variable")
					if !variable && !strings.HasSuffix(n.Class, "Object") {
						continue
					}
					a, e := device.ParseAddress(n.NodeID, "Auto", 1)
					if e != nil {
						return 0, e
					}
					n.NodeID = a.NodeID
					if seen[n.NodeID] {
						continue
					}
					if len(seen) >= maxNodes || variable && len(points) >= maxPoints {
						return 0, fmt.Errorf("父节点下超过 %d 个变量或 %d 个节点，请选择较小的父节点", maxPoints, maxNodes)
					}
					seen[n.NodeID] = true
					next = append(next, n.NodeID)
					if variable {
						nodes = append(nodes, n)
						points = append(points, Point{n.NodeID, "Auto", 1})
					}
				}
			}
			if time.Since(reported) >= progressInterval {
				reported = time.Now()
				t.update(nil, fmt.Sprintf("浏览中 · 已发现 %d 个变量", len(points)))
			}
			return size, nil
		})
		level = next
	}
	if err != nil {
		rr := make([]device.ReadResult, len(points))
		for i, p := range points {
			rr[i] = device.ReadResult{Address: device.Address{NodeID: p.Address, Type: "Auto", Count: 1}, Quality: "UNKNOWN", Execution: "NOT_READ", Error: "浏览未完成，尚未读取"}
		}
		t.update(rr, "浏览未完成："+err.Error())
		if !errors.Is(err, context.Canceled) {
			c.failure(s, err)
		}
		return err
	}
	if len(points) == 0 {
		t.update(nil, "未发现可读取的变量")
		return errors.New("父节点下未发现变量，请浏览并选择设备或文件夹节点")
	}
	aa, err := resolve(points)
	if err != nil {
		return err
	}
	return t.read(ctx, aa, "全量读取")
}

// ValidatePoints reads an imported point list with the paced batches of a full
// read, labelling each result with its imported name.
func (c *Controller) ValidatePoints(ctx context.Context, source string, points []Point, names []string) error {
	aa, err := resolve(points)
	if err != nil {
		return err
	}
	c.StopPolling()
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	t := c.newScan(s, source, func(i int) string {
		if i < len(names) {
			return names[i]
		}
		return ""
	}, nil)
	return t.read(ctx, aa, "导入验证")
}

func browseNodes(ctx context.Context, d device.Driver, nodes []string) ([][]device.Node, error) {
	if b, ok := d.(interface {
		BrowseNodes(context.Context, []string) ([][]device.Node, error)
	}); ok {
		return b.BrowseNodes(ctx, nodes)
	}
	out := make([][]device.Node, len(nodes))
	for i, n := range nodes {
		children, err := d.Browse(ctx, n)
		if err != nil {
			return nil, err
		}
		out[i] = children
	}
	return out, nil
}

// retryAfter reports whether a smaller batch may succeed and how long to wait
// first: limit rejections retry at once, while timeouts and exhausted
// resources give a loaded server time to recover.
func retryAfter(ctx context.Context, d device.Driver, err error) (time.Duration, bool) {
	if err == nil || ctx.Err() != nil {
		return 0, false
	}
	if state, ok := d.(interface{ IsClosed() bool }); ok && state.IsClosed() {
		return 0, false
	}
	for _, code := range []ua.StatusCode{ua.StatusBadTooManyOperations, ua.StatusBadEncodingLimitsExceeded, ua.StatusBadRequestTooLarge, ua.StatusBadResponseTooLarge} {
		if errors.Is(err, code) {
			return 0, true
		}
	}
	for _, code := range []ua.StatusCode{ua.StatusBadTimeout, ua.StatusBadResourceUnavailable, ua.StatusBadOutOfMemory} {
		if errors.Is(err, code) {
			return readRetryDelay, true
		}
	}
	// The parent context is live, so the deadline is the per-request timeout.
	return readRetryDelay, errors.Is(err, context.DeadlineExceeded)
}

func pause(ctx context.Context, wait time.Duration) error {
	if err := ctx.Err(); err != nil || wait <= 0 {
		return err
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// paced calls do over [0, n) in batches, halving the batch when retryAfter
// allows; do reports the approximate bytes of its larger message and must
// leave no partial effect when it fails.
func paced(ctx context.Context, d device.Driver, n int, do func(start, end, batchSize int) (int, error)) error {
	batchSize, wait, grow := firstBatchSize, time.Duration(0), true
	for start := 0; start < n; {
		if err := pause(ctx, wait); err != nil {
			return err
		}
		wait = readBatchGap
		end := min(start+batchSize, n)
		began := time.Now()
		size, err := do(start, end, batchSize)
		if delay, ok := retryAfter(ctx, d, err); ok && batchSize > 1 {
			batchSize, wait, grow = max(1, batchSize/2), delay, false
			continue
		}
		if err != nil {
			return err
		}
		next := batchSize
		if grow && end-start == batchSize && time.Since(began) < growBelow {
			next = 2 * batchSize
		}
		batchSize = max(1, min(next, maxBatchSize, messageBudget*(end-start)/max(size, 1)))
		start = end
	}
	return nil
}

// messageBytes approximates the encoded size of a read: the request's node
// ids per attribute, and the response's values, strings and arrays.
func messageBytes(rr []device.ReadResult, ops int) int {
	request, response := 0, 0
	for i := range rr {
		request += ops * (len(rr[i].Address.NodeID) + 16)
		response += ops*24 + valueBytes(rr[i].Value)
	}
	return max(request, response)
}

func valueBytes(v device.Value) int {
	n := 8 + len(v.Text) + len(v.Raw)
	for _, e := range v.Elements {
		n += valueBytes(e)
	}
	return n
}

// readBatches reads aa in paced batches; describe also reads each variable's
// type and access in the same requests when the driver can.
func readBatches(ctx context.Context, d device.Driver, aa []device.Address, describe bool, progress func([]device.ReadResult, int)) ([]device.ReadResult, error) {
	read := d.Read
	if r, ok := d.(interface {
		ReadWithAttributes(context.Context, []device.Address) ([]device.ReadResult, error)
	}); ok && describe {
		read = r.ReadWithAttributes
	}
	rr := make([]device.ReadResult, len(aa))
	for i, a := range aa {
		rr[i] = device.ReadResult{Address: a, Quality: "UNKNOWN", Execution: "NOT_READ", Error: "尚未读取"}
	}
	var reported time.Time
	var failedStart, failedEnd int
	ops := 1
	if describe {
		ops = 3
	}
	err := paced(ctx, d, len(aa), func(start, end, batchSize int) (int, error) {
		failedStart, failedEnd = start, end
		batch, err := read(ctx, aa[start:end])
		if err == nil && len(batch) != end-start {
			err = errors.New("服务器读取结果数量不一致")
		}
		if err != nil {
			return 0, err
		}
		copy(rr[start:end], batch)
		for i := start; i < end; i++ {
			if rr[i].Execution == "" {
				rr[i].Execution = "READ"
			}
		}
		// Each report copies the whole list, so reports follow the UI refresh rate.
		if progress != nil && time.Since(reported) >= progressInterval {
			reported = time.Now()
			progress(rr, batchSize)
		}
		return messageBytes(batch, ops), nil
	})
	if err != nil && ctx.Err() == nil {
		for i := failedStart; i < failedEnd; i++ {
			rr[i].Execution = "REQUEST_FAILED"
			rr[i].Error = err.Error()
			rr[i].Time = time.Now()
		}
	}
	return rr, err
}
