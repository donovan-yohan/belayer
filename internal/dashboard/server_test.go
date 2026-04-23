package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServer_ServesWebUI(t *testing.T) {
	srv, err := NewServer([]DaemonConfig{{Name: "test", URL: "http://localhost:1", Token: "t"}})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// index.html
	res, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatalf("GET /ui/: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Belayer") {
		t.Fatalf("index.html should contain 'Belayer', got:\n%s", string(body))
	}

	// CSS
	res2, err := http.Get(ts.URL + "/ui/style.css")
	if err != nil {
		t.Fatalf("GET /ui/style.css: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for css, got %d", res2.StatusCode)
	}
	ct := res2.Header.Get("Content-Type")
	if ct != "text/css" {
		t.Fatalf("expected text/css, got %s", ct)
	}

	// JS
	res3, err := http.Get(ts.URL + "/ui/app.js")
	if err != nil {
		t.Fatalf("GET /ui/app.js: %v", err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for js, got %d", res3.StatusCode)
	}
	if res3.Header.Get("Content-Type") != "application/javascript" {
		t.Fatalf("expected application/javascript, got %s", res3.Header.Get("Content-Type"))
	}
}

func TestServer_ListDaemons(t *testing.T) {
	// Start a mock daemon
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer good-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.URL.Path == "/sessions" {
			w.Write([]byte(`[]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mock.Close()

	srv, err := NewServer([]DaemonConfig{
		{Name: "mock", URL: mock.URL, Token: "good-token"},
		{Name: "down", URL: "http://localhost:1", Token: "t"},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/daemons")
	if err != nil {
		t.Fatalf("GET /api/daemons: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var infos []daemonInfo
	if err := json.NewDecoder(res.Body).Decode(&infos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 daemons, got %d", len(infos))
	}

	m := make(map[string]bool)
	for _, info := range infos {
		m[info.Name] = info.Healthy
	}
	if !m["mock"] {
		t.Fatalf("mock daemon should be healthy")
	}
	if m["down"] {
		t.Fatalf("down daemon should be unhealthy")
	}
}

func TestServer_Proxy(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer tok" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/sessions" {
			w.Write([]byte(`{"proxied":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mock.Close()

	srv, _ := NewServer([]DaemonConfig{{Name: "a", URL: mock.URL, Token: "tok"}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/daemons/a/sessions")
	if err != nil {
		t.Fatalf("GET proxy: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `"proxied":true`) {
		t.Fatalf("expected proxied response, got: %s", string(body))
	}
}

func TestServer_ProxyUnknownDaemon(t *testing.T) {
	srv, _ := NewServer([]DaemonConfig{{Name: "a", URL: "http://localhost:1", Token: "t"}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/daemons/unknown/sessions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.yaml")
	content := `
daemons:
  - name: extend-api
    url: http://localhost:7523
    token: abc
  - name: relay-ide
    url: http://localhost:7524
    token: def
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgs, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 daemons, got %d", len(cfgs))
	}
	if cfgs[0].Name != "extend-api" || cfgs[0].Token != "abc" {
		t.Fatalf("unexpected first daemon: %+v", cfgs[0])
	}
	if cfgs[1].Name != "relay-ide" || cfgs[1].Token != "def" {
		t.Fatalf("unexpected second daemon: %+v", cfgs[1])
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/dashboard.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewServer_EmptyDaemons(t *testing.T) {
	_, err := NewServer([]DaemonConfig{})
	if err == nil {
		t.Fatal("expected error for empty daemon list")
	}
}

func TestNewServer_InvalidURL(t *testing.T) {
	_, err := NewServer([]DaemonConfig{{Name: "bad", URL: "://invalid", Token: "t"}})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestServer_ProxySSE(t *testing.T) {
	// SSE endpoint that sends two events then hangs until client disconnects
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		w.Write([]byte("data: hello\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Wait a bit so the client can read
		time.Sleep(50 * time.Millisecond)
	}))
	defer mock.Close()

	srv, _ := NewServer([]DaemonConfig{{Name: "sse", URL: mock.URL, Token: ""}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/daemons/sse/events/stream")
	if err != nil {
		t.Fatalf("GET SSE: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "data: hello") {
		t.Fatalf("expected SSE data, got: %s", string(body))
	}
}
