package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStatusCmd_IncludesLogColumn verifies that `belayer status` prints a LOG
// column and surfaces the session's log_level value in the row.
//
// Test-plan item: T1.5 — `belayer status` adds LOG column.
func TestStatusCmd_IncludesLogColumn(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)

	// Create a session at log_level=trace so the column value is unambiguous.
	_, err := c.CreateSession("status-log-test", "nightshift", nil, "", "trace")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"status", "--socket", sock})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute status: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "LOG") {
		t.Fatalf("missing LOG column header in status output:\n%s", out)
	}
	if !strings.Contains(out, "trace") {
		t.Fatalf("missing 'trace' value in status row:\n%s", out)
	}
}

// TestLogsCmd_RawAgentFollowsBridgeStdout verifies that `belayer logs --raw --agent <name>`
// streams raw bytes from the bridge stdout HTTP endpoint.
//
// Test-plan item: T2.4 — belayer logs --raw --agent tails bridge stdout.
func TestLogsCmd_RawAgentFollowsBridgeStdout(t *testing.T) {
	// Start an httptest server that answers GET /sessions/{id}/bridges/{agent}/stdout?follow=1&after_byte=0
	// by writing "hello\n" then "world\n" with a small gap, then holding until client disconnects.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{id}/bridges/{agent}/stdout", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("follow") != "1" {
			t.Errorf("want follow=1, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		w.Write([]byte("hello\n"))
		if fl != nil {
			fl.Flush()
		}
		w.Write([]byte("world\n"))
		if fl != nil {
			fl.Flush()
		}
	})
	ts := httptest.NewUnstartedServer(mux)
	// Use os.MkdirTemp with a short prefix to stay within the 104-char
	// Unix socket path limit on macOS.
	tmp, err := os.MkdirTemp("", "bls")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	sock := filepath.Join(tmp, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ts.Listener = ln
	ts.Start()
	defer ts.Close()

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--raw", "--agent", "supervisor", "sess-123"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cmd.ExecuteContext(ctx)

	if !strings.Contains(out.String(), "hello") || !strings.Contains(out.String(), "world") {
		t.Fatalf("raw stream missing bytes, got %q", out.String())
	}
}
