package service

import "context"

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
