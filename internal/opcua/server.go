package opcua

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"
)

// ServerInfo describes the connected server from its standard Server object.
type ServerInfo struct {
	Endpoint, Policy, Mode                                       string
	Product, ProductURI, Manufacturer, Version, Build            string
	State                                                        string
	BuildDate, StartTime, CurrentTime                            time.Time
	Namespaces                                                   []string
	Sessions, CumulatedSessions, RejectedSessions, Subscriptions int // -1 when the server keeps no diagnostics
	Clients                                                      []SessionInfo
}

// SessionInfo is one client session the server reports in its diagnostics.
type SessionInfo struct {
	Name, Application, Endpoint string
	Connected                   time.Time
}

// Ping reads the server state as a heartbeat; it is not recorded as a packet.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	_, err := c.client.Read(ctx, &ua.ReadRequest{NodesToRead: []*ua.ReadValueID{{NodeID: ua.NewNumericNodeID(0, id.Server_ServerStatus_State), AttributeID: ua.AttributeIDValue}}})
	return err
}

// ServerInfo reads the server status, build information, namespaces and, when
// the server keeps them, session diagnostics with the connected clients.
func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	info := ServerInfo{Endpoint: c.target, Policy: c.options.Policy, Mode: c.options.Mode, Sessions: -1, CumulatedSessions: -1, RejectedSessions: -1, Subscriptions: -1}
	nodes := []uint32{id.Server_ServerStatus, id.Server_NamespaceArray, id.Server_ServerDiagnostics_ServerDiagnosticsSummary, id.Server_ServerDiagnostics_SessionsDiagnosticsSummary_SessionDiagnosticsArray}
	req := &ua.ReadRequest{}
	for _, n := range nodes {
		req.NodesToRead = append(req.NodesToRead, &ua.ReadValueID{NodeID: ua.NewNumericNodeID(0, n), AttributeID: ua.AttributeIDValue})
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	res, err := c.client.Read(ctx, req)
	if err != nil {
		return info, err
	}
	if len(res.Results) != len(nodes) {
		return info, fmt.Errorf("服务器读取结果数量不一致")
	}
	value := func(i int) any {
		r := res.Results[i]
		if r == nil || r.Value == nil || uint32(r.Status)&0x80000000 != 0 {
			return nil
		}
		v := r.Value.Value()
		if e, ok := v.(*ua.ExtensionObject); ok {
			return e.Value
		}
		return v
	}
	if s, ok := value(0).(*ua.ServerStatusDataType); ok {
		info.State, info.StartTime, info.CurrentTime = strings.TrimPrefix(s.State.String(), "ServerState"), s.StartTime, s.CurrentTime
		if b := s.BuildInfo; b != nil {
			info.Product, info.ProductURI, info.Manufacturer, info.Version, info.Build, info.BuildDate = b.ProductName, b.ProductURI, b.ManufacturerName, b.SoftwareVersion, b.BuildNumber, b.BuildDate
		}
	}
	info.Namespaces, _ = value(1).([]string)
	if s, ok := value(2).(*ua.ServerDiagnosticsSummaryDataType); ok {
		info.Sessions, info.CumulatedSessions, info.RejectedSessions, info.Subscriptions = int(s.CurrentSessionCount), int(s.CumulatedSessionCount), int(s.RejectedSessionCount), int(s.CurrentSubscriptionCount)
	}
	sessions, _ := value(3).([]*ua.ExtensionObject)
	for _, e := range sessions {
		s, ok := e.Value.(*ua.SessionDiagnosticsDataType)
		if !ok {
			continue
		}
		v := SessionInfo{Name: s.SessionName, Endpoint: s.EndpointURL, Connected: s.ClientConnectionTime}
		if d := s.ClientDescription; d != nil && d.ApplicationName != nil {
			v.Application = d.ApplicationName.Text
		}
		info.Clients = append(info.Clients, v)
	}
	return info, nil
}
