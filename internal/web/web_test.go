package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opcua-ai-studio/internal/service"
)

func TestDemoWorkflow(t *testing.T) {
	controller := service.New(service.Options{RecordDir: t.TempDir()})
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Error(err)
		}
	})
	handler := New(controller, "127.0.0.1:8787")
	for path, marker := range map[string]string{"/": "<!doctype html>", "/app.js": "/api/", "/app.css": ":root"} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !strings.Contains(strings.ToLower(w.Body.String()), marker) {
			t.Errorf("embedded asset %s is not readable (status %d)", path, w.Code)
		}
	}
	post := func(path, body string, status int) response {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:8787")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: status %d, want %d: %s", path, w.Code, status, w.Body)
		}
		var out response
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	demo := post("demo/start", `{}`, http.StatusOK).State
	if demo.State != "connected" || demo.DemoTarget == "" || len(demo.Results) != 1 || demo.Results[0].Value.Integer != "42" {
		t.Fatalf("incorrect demo state: %+v", demo)
	}
	browse := post("browse", `{"node":"ns=2;i=85"}`, http.StatusOK).State
	if len(browse.Nodes) == 0 {
		t.Fatal("demo browse returned no nodes")
	}
	read := post("read", `{"nodes":["ns=2;s=Counter", "ns=2;s=热力站.一次供水温度"]}`, http.StatusOK).State
	if len(read.Results) != 2 || read.Results[1].Quality != "GOOD" {
		t.Fatalf("batch read: %+v", read.Results)
	}
	if refresh := post("refresh/start", `{"interval_ms":100}`, http.StatusOK).State; !refresh.Polling {
		t.Fatal("refresh did not start")
	}
	if refresh := post("refresh/stop", `{}`, http.StatusOK).State; refresh.Polling {
		t.Fatal("refresh did not stop")
	}
	write := `{"node":"ns=2;s=Counter","type":"Int32","count":1,"value":"123","generation":"` + demo.Generation + `","approved":`
	post("write", write+`false}`, http.StatusBadRequest)
	written := post("write", write+`true}`, http.StatusOK).State
	if written.WriteResult == nil || written.WriteResult.State != "CONFIRMED" || written.Readback != "一致" {
		t.Fatalf("incorrect write and readback: %+v, %q", written.WriteResult, written.Readback)
	}
	if stopped := post("demo/stop", `{}`, http.StatusOK).State; stopped.DemoTarget != "" || stopped.State != "disconnected" {
		t.Fatalf("demo still running: %+v", stopped)
	}
	post("demo/start", `{}`, http.StatusOK)
	post("write", write+`true}`, http.StatusBadRequest)
}

func TestRejectsCrossOriginWrites(t *testing.T) {
	controller := service.New(service.Options{RecordDir: t.TempDir()})
	t.Cleanup(func() { _ = controller.Close() })
	handler := New(controller, "127.0.0.1:8787")
	for _, tt := range []struct {
		host, origin, contentType string
		want                      int
	}{
		{"other.example:8787", "", "application/json", http.StatusForbidden},
		{"127.0.0.1:8787", "https://other.example", "application/json", http.StatusForbidden},
		{"127.0.0.1:8787", "", "text/plain", http.StatusUnsupportedMediaType},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://"+tt.host+"/api/demo/start", strings.NewReader(`{}`))
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Content-Type", tt.contentType)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tt.want || controller.Snapshot().DemoTarget != "" {
			t.Errorf("%s / %s / %s: status %d, want %d and no demo", tt.host, tt.origin, tt.contentType, w.Code, tt.want)
		}
	}
}
