package simulator

import (
	"context"
	"fmt"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/server"
	"github.com/gopcua/opcua/ua"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const CounterNode = "ns=2;s=Counter"

// ChineseNodes are demo points with Chinese string identifiers, as many
// Chinese servers name their tags; the pump switch is writable.
var ChineseNodes = []string{"ns=2;s=热力站.一次供水温度", "ns=2;s=热力站.循环泵启停", "ns=2;s=热力站.站点名称"}

type Server struct {
	Endpoint string
	srv      *server.Server
	cancel   context.CancelFunc
	once     sync.Once
	err      error
}

func Start(ctx context.Context, endpoint string) (*Server, error) { return StartLoad(ctx, endpoint, 0) }

// StartLoad also serves tags Double variables for load tests in namespace 3,
// 100 per folder below Objects/Load: every tenth is writable and every
// fiftieth reports BadNoCommunication like an offline device.
func StartLoad(ctx context.Context, endpoint string, tags int) (*Server, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "opc.tcp" || u.Hostname() == "" {
		return nil, fmt.Errorf("演示 Endpoint 须为 opc.tcp://127.0.0.1:端口")
	}
	ip := net.ParseIP(u.Hostname())
	if (ip == nil || !ip.IsLoopback()) && u.Hostname() != "localhost" {
		return nil, fmt.Errorf("演示服务器仅支持回环地址")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("无效演示端口")
	}
	if port == 0 {
		// The SDK cannot expose an OS-assigned listening port. Reserve a loopback port,
		// close it, then start; any intervening conflict is returned to the caller.
		l, e := net.Listen("tcp", net.JoinHostPort(u.Hostname(), "0"))
		if e != nil {
			return nil, e
		}
		port = l.Addr().(*net.TCPAddr).Port
		l.Close()
	}
	endpoint = "opc.tcp://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	srv := server.New(server.EndPoint(u.Hostname(), port), server.ServerName("OPC UA AI Studio Demo"), server.EnableSecurity("None", ua.MessageSecurityModeNone), server.EnableAuthMode(ua.UserTokenTypeAnonymous))
	server.NewNodeNameSpace(srv, "urn:opcua-ai-studio:application")
	ns := server.NewNodeNameSpace(srv, "urn:opcua-ai-studio:demo")
	root, _ := srv.Namespace(0)
	root.Objects().AddRef(ns.Objects(), id.Organizes, true)
	for _, p := range []struct {
		name  string
		value any
	}{{"Counter", int32(42)}, {"Temperature", float64(23.5)}, {"Enabled", true}, {"Message", "Hello OPC UA"}, {"Samples", []int32{1, 2, 3}}, {"ReadOnly", int32(7)}, {"热力站.一次供水温度", float64(95.5)}, {"热力站.循环泵启停", false}, {"热力站.站点名称", "一号换热站"}} {
		name := p.name[strings.LastIndex(p.name, ".")+1:]
		n := ns.AddNode(server.NewVariableNode(ua.NewStringNodeID(ns.ID(), p.name), name, p.value))
		// Report access explicitly so writable demo points show as control points.
		level := uint8(ua.AccessLevelTypeCurrentRead | ua.AccessLevelTypeCurrentWrite)
		if p.name == "ReadOnly" || p.name == "热力站.一次供水温度" || p.name == "热力站.站点名称" {
			level = uint8(ua.AccessLevelTypeCurrentRead)
		}
		n.SetAttribute(ua.AttributeIDAccessLevel, server.DataValueFromValue(level))
		n.SetAttribute(ua.AttributeIDUserAccessLevel, server.DataValueFromValue(level))
		ns.Objects().AddRef(n, id.HasComponent, true)
	}
	if tags > 0 {
		load := newLoadSpace(tags)
		srv.AddNamespace(load)
		root.Objects().AddRef(load.folderNode(), id.Organizes, true)
	}
	sctx, cancel := context.WithCancel(ctx)
	if err = srv.Start(sctx); err != nil {
		cancel()
		return nil, err
	}
	return &Server{Endpoint: endpoint, srv: srv, cancel: cancel}, nil
}
func (s *Server) Close() error { s.once.Do(func() { s.cancel(); s.err = s.srv.Close() }); return s.err }
