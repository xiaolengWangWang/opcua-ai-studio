package service

import (
	"errors"
	"time"

	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/recorder"
)

// The record database's trend samples, connections and run log lines.
type (
	Sample     = recorder.Sample
	Connection = recorder.Connection
	LogLine    = recorder.Line
)

var errNoRecords = errors.New("记录数据库不可用")

// RecordSamples keeps one sample per numeric point for trends, under the
// connected Endpoint.
func (c *Controller) RecordSamples(values map[string]Sample) {
	c.mu.Lock()
	target, connected := c.snap.Target, c.snap.State == "connected"
	c.mu.Unlock()
	if c.rec != nil && connected && len(values) > 0 {
		c.rec.Sample(target, values)
	}
}

// History reads the trend samples of nodes of the current Endpoint since the
// given time, oldest first.
func (c *Controller) History(nodes []string, since time.Time) (map[string][]Sample, error) {
	if c.rec == nil {
		return nil, errNoRecords
	}
	c.mu.Lock()
	target := c.snap.Target
	c.mu.Unlock()
	return c.rec.History(target, nodes, since)
}

// Connections lists the latest connections, newest first.
func (c *Controller) Connections(limit int) ([]Connection, error) {
	if c.rec == nil {
		return nil, errNoRecords
	}
	return c.rec.Connections(limit)
}

// SessionRecords reads a connection's run log and its last requests.
func (c *Controller) SessionRecords(session int64, limit int) ([]LogLine, []device.Event, error) {
	if c.rec == nil {
		return nil, nil, errNoRecords
	}
	lines, err := c.rec.Lines(session, limit)
	if err != nil {
		return nil, nil, err
	}
	packets, err := c.rec.SessionPackets(session, limit)
	return lines, packets, err
}

// LogHistory reads the latest run log lines of every window, oldest first.
func (c *Controller) LogHistory(limit int) ([]LogLine, error) {
	if c.rec == nil {
		return nil, errNoRecords
	}
	return c.rec.Lines(0, limit)
}
