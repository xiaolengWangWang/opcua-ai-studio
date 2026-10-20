package service

import (
	"context"
	"fmt"
	"opcua-ai-studio/internal/device"
	"strings"
	"time"
)

func (c *Controller) Browse(ctx context.Context, node string) error {
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	nodes, err := s.d.Browse(ctx, node)
	c.mu.Lock()
	if c.s == s && !c.closed {
		c.snap.Nodes = nodes
		c.snap.BrowseStatus = fmt.Sprintf("%s 下 %d 个子节点", node, len(nodes))
		c.snap.LastError = ""
		if err != nil {
			c.snap.LastError = err.Error()
		}
		c.snap.Revision++
	}
	c.mu.Unlock()
	if err != nil {
		c.failure(s, err)
	}
	return err
}

// nodeName names a node in browse paths: Objects for i=85, else its display
// or browse name, else its NodeID.
func nodeName(n device.Node) string {
	switch {
	case n.DisplayName != "":
		return n.DisplayName
	case n.BrowseName != "":
		return n.BrowseName
	case n.NodeID == "i=85":
		return "Objects"
	}
	return n.NodeID
}

// walk browses level by level below level, many nodes per request, so 50000
// variables take hundreds of round trips rather than one per node. It calls
// visit once for each Object or Variable not yet seen, with its parent, and
// progress after each request.
func walk(ctx context.Context, d device.Driver, level []string, seen map[string]bool, visit func(parent string, n device.Node) error, progress func()) error {
	var err error
	for len(level) > 0 && err == nil {
		var next []string
		err = paced(ctx, d, len(level), func(start, end, _ int) (int, error) {
			lists, err := browseNodes(ctx, d, level[start:end])
			if err != nil {
				return 0, err
			}
			// Pages are bounded by the driver; the request grows with the batch.
			size := 0
			for _, n := range level[start:end] {
				size += len(n) + 32
			}
			for i, children := range lists {
				for _, n := range children {
					if !strings.HasSuffix(n.Class, "Variable") && !strings.HasSuffix(n.Class, "Object") {
						continue
					}
					a, e := device.ParseAddress(n.NodeID, "Auto", 1)
					if e != nil {
						return 0, e
					}
					if n.NodeID = a.NodeID; seen[n.NodeID] {
						continue
					}
					if len(seen) >= maxNodes {
						return 0, errTooLarge
					}
					seen[n.NodeID] = true
					if e = visit(level[start+i], n); e != nil {
						return 0, e
					}
					next = append(next, n.NodeID)
				}
			}
			progress()
			return size, nil
		})
		level = next
	}
	return err
}

// SearchNodes lists the objects and variables below root that match, in
// place of the browse list, so they can be selected and read.
func (c *Controller) SearchNodes(ctx context.Context, root string, match func(device.Node) bool) error {
	a, err := device.ParseAddress(root, "Auto", 1)
	if err != nil {
		return err
	}
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()
	var found []device.Node
	visited := 0
	publish := func(status string) {
		c.mu.Lock()
		if c.s == s && !c.closed {
			c.snap.Nodes = append([]device.Node(nil), found...)
			c.snap.BrowseStatus = status
			c.snap.Revision++
		}
		c.mu.Unlock()
	}
	var reported time.Time
	err = walk(ctx, s.d, []string{a.NodeID}, map[string]bool{a.NodeID: true}, func(_ string, n device.Node) error {
		visited++
		if match(n) {
			found = append(found, n)
		}
		return nil
	}, func() {
		if time.Since(reported) >= progressInterval {
			reported = time.Now()
			publish(fmt.Sprintf("搜索中 · 已浏览 %d 个节点，找到 %d 个", visited, len(found)))
		}
	})
	status := fmt.Sprintf("搜索完成 · 在 %s 下浏览 %d 个节点，找到 %d 个", a.NodeID, visited, len(found))
	if err != nil {
		status = fmt.Sprintf("搜索未完成（已浏览 %d 个节点，找到 %d 个）：%v", visited, len(found), err)
	}
	publish(status)
	if err != nil {
		c.failure(s, err)
	}
	return err
}
