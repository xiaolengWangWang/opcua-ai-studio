package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opcua-ai-studio/internal/device"
	"opcua-ai-studio/internal/opcua"
	"opcua-ai-studio/internal/service"
)

//go:embed static
var assets embed.FS

type state struct {
	Revision     string              `json:"revision"`
	Generation   string              `json:"generation"`
	State        string              `json:"state"`
	Target       string              `json:"target"`
	DemoTarget   string              `json:"demo_target"`
	LastError    string              `json:"last_error"`
	Results      []device.ReadResult `json:"results"`
	Nodes        []device.Node       `json:"nodes"`
	WriteResult  *device.WriteResult `json:"write_result"`
	Readback     string              `json:"readback"`
	Logs         []string            `json:"logs"`
	ScanStatus   string              `json:"scan_status"`
	BrowseStatus string              `json:"browse_status"`
	Polling      bool                `json:"polling"`
	RecordDir    string              `json:"record_dir"`
	RecordError  string              `json:"record_error"`
	Server       *opcua.ServerInfo   `json:"server"`
}

type response struct {
	State     state            `json:"state"`
	Error     string           `json:"error,omitempty"`
	Endpoints []opcua.Endpoint `json:"endpoints,omitempty"`
}

type server struct {
	c    *service.Controller
	host string
}

type requestError struct {
	status int
	err    error
}

func (e *requestError) Error() string { return e.err.Error() }

// New serves the local UI and its API. host is the exact expected HTTP Host,
// including port; callers should bind a loopback address.
func New(c *service.Controller, host string) http.Handler { return &server{c: c, host: host} }

func (s *server) snapshot() state {
	x := s.c.Snapshot()
	return state{
		Revision: strconv.FormatUint(x.Revision, 10), Generation: strconv.FormatUint(x.Generation, 10),
		State: x.State, Target: x.Target, DemoTarget: x.DemoTarget, LastError: x.LastError,
		Results: x.Results, Nodes: x.Nodes, WriteResult: x.WriteResult, Readback: x.Readback, Logs: x.Logs,
		ScanStatus: x.ScanStatus, BrowseStatus: x.BrowseStatus, Polling: x.Polling,
		RecordDir: x.RecordDir, RecordError: x.RecordError, Server: x.Server,
	}
}
func (s *server) reply(w http.ResponseWriter, status int, err error, endpoints []opcua.Endpoint) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	out := response{State: s.snapshot(), Endpoints: endpoints}
	if err != nil {
		out.Error = err.Error()
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Host != s.host {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"无效的 Host"}`)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.api(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := ""
	switch r.URL.Path {
	case "/":
		name = "index.html"
	case "/app.js":
		name = "app.js"
	case "/app.css":
		name = "app.css"
	case "/favicon.ico":
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("static/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch name {
	case "index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case "app.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case "app.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return &requestError{http.StatusUnsupportedMediaType, errors.New("请求须使用 application/json")}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &requestError{http.StatusRequestEntityTooLarge, errors.New("请求内容过大")}
		}
		return fmt.Errorf("无法读取请求: %w", err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		return errors.New("请求须为 JSON 对象")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("无效的 JSON: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("请求须只包含一个 JSON 对象")
	}
	return nil
}

type connectRequest struct {
	Target            string `json:"target"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	Policy            string `json:"policy"`
	Mode              string `json:"mode"`
	Certificate       string `json:"certificate"`
	PrivateKey        string `json:"private_key"`
	ServerCertificate string `json:"server_certificate"`
	AutoReconnect     bool   `json:"auto_reconnect"`
}
type nodeRequest struct {
	Node string `json:"node"`
}
type writeRequest struct {
	Node       string          `json:"node"`
	Type       device.DataType `json:"type"`
	Count      int             `json:"count"`
	Value      string          `json:"value"`
	Generation string          `json:"generation"`
	Approved   bool            `json:"approved"`
}

func (s *server) api(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/state" {
		if r.Method != http.MethodGet {
			s.reply(w, http.StatusMethodNotAllowed, errors.New("方法不允许"), nil)
			return
		}
		s.reply(w, http.StatusOK, nil, nil)
		return
	}
	if r.Method != http.MethodPost {
		s.reply(w, http.StatusMethodNotAllowed, errors.New("方法不允许"), nil)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
		s.reply(w, http.StatusForbidden, errors.New("跨来源请求被拒绝"), nil)
		return
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		s.reply(w, http.StatusForbidden, errors.New("跨站请求被拒绝"), nil)
		return
	}
	var err error
	var endpoints []opcua.Endpoint
	switch r.URL.Path {
	case "/api/endpoints":
		var q struct {
			Target string `json:"target"`
		}
		if err = decode(w, r, &q); err == nil {
			endpoints, err = opcua.Endpoints(r.Context(), q.Target, 5*time.Second)
		}
	case "/api/connect":
		var q connectRequest
		if err = decode(w, r, &q); err == nil {
			cfg := service.DefaultConfig()
			cfg.Target = q.Target
			cfg.Username = q.Username
			cfg.Password = q.Password
			cfg.CertificatePEM = q.Certificate
			cfg.PrivateKeyPEM = q.PrivateKey
			cfg.ServerCertificatePEM = q.ServerCertificate
			cfg.AutoReconnect = q.AutoReconnect
			if q.Policy != "" {
				cfg.Policy = q.Policy
			}
			if q.Mode != "" {
				cfg.Mode = q.Mode
			}
			// A session outlives the HTTP request and is cancelled by the controller.
			err = s.c.Connect(context.Background(), cfg)
		}
	case "/api/disconnect":
		var q struct{}
		if err = decode(w, r, &q); err == nil {
			err = s.c.Disconnect()
		}
	case "/api/demo/start":
		var q struct{}
		if err = decode(w, r, &q); err == nil {
			err = s.c.StartDemo(context.Background())
		}
	case "/api/demo/stop":
		var q struct{}
		if err = decode(w, r, &q); err == nil {
			err = s.c.StopDemo()
		}
	case "/api/browse":
		var q nodeRequest
		if err = decode(w, r, &q); err == nil {
			err = s.c.Browse(r.Context(), q.Node)
		}
	case "/api/read":
		var q struct {
			Nodes []string `json:"nodes"`
		}
		if err = decode(w, r, &q); err == nil {
			points := make([]service.Point, len(q.Nodes))
			for i, n := range q.Nodes {
				points[i] = service.Point{Address: n, Type: "Auto", Count: 1}
			}
			err = s.c.Read(r.Context(), points)
		}
	case "/api/read-all":
		var q nodeRequest
		if err = decode(w, r, &q); err == nil {
			err = s.c.ReadAll(r.Context(), q.Node)
		}
	case "/api/write":
		var q writeRequest
		if err = decode(w, r, &q); err == nil {
			var gen uint64
			gen, err = strconv.ParseUint(q.Generation, 10, 64)
			if err != nil || gen == 0 {
				err = errors.New("写入须提供有效的连接代次")
			} else {
				err = s.c.Write(r.Context(), service.WriteRequest{Point: service.Point{Address: q.Node, Type: q.Type, Count: q.Count}, Value: q.Value, Generation: gen}, q.Approved)
			}
		}
	case "/api/refresh/start":
		var q struct {
			IntervalMS int64 `json:"interval_ms"`
		}
		if err = decode(w, r, &q); err == nil {
			if q.IntervalMS < 100 || q.IntervalMS > int64((24*time.Hour)/time.Millisecond) {
				err = errors.New("刷新间隔须为 100 到 86400000 毫秒")
			} else {
				err = s.c.StartRefresh(context.Background(), time.Duration(q.IntervalMS)*time.Millisecond)
			}
		}
	case "/api/refresh/stop":
		var q struct{}
		if err = decode(w, r, &q); err == nil {
			s.c.StopPolling()
		}
	default:
		s.reply(w, http.StatusNotFound, errors.New("接口不存在"), nil)
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadRequest
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			status = requestErr.status
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
	}
	s.reply(w, status, err, endpoints)
}
