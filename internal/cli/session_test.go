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

// startRawBridgeServer spins a stub daemon socket that serves the /bridges/{agent}/stdout
// endpoint with the handler h, and returns the socket path.
func startRawBridgeServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{id}/bridges/{agent}/stdout", h)
	ts := httptest.NewUnstartedServer(mux)
	// Short prefix keeps the unix socket path under macOS's 104-char limit.
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
	t.Cleanup(ts.Close)
	return sock
}

// TestLogsCmd_RawAgentFollowMode verifies that `belayer logs --raw --agent <name> -f`
// streams raw bytes from the bridge stdout HTTP endpoint with follow=1 and
// always sends after_byte (so existing history is not silently dropped).
//
// Test-plan item: T2.4 — belayer logs --raw --agent tails bridge stdout.
func TestLogsCmd_RawAgentFollowMode(t *testing.T) {
	sock := startRawBridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("follow") != "1" {
			t.Errorf("want follow=1, got %q", r.URL.RawQuery)
		}
		if got := q.Get("after_byte"); got != "0" {
			t.Errorf("want after_byte=0 (not omitted), got %q", got)
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

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--raw", "--agent", "supervisor", "-f", "sess-123"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cmd.ExecuteContext(ctx)

	if !strings.Contains(out.String(), "hello") || !strings.Contains(out.String(), "world") {
		t.Fatalf("raw stream missing bytes, got %q", out.String())
	}
}

// TestLogsCmd_RawAgentOneShot verifies that `belayer logs --raw --agent <name>`
// without --follow performs a one-shot full-file fetch (no follow=1, no after_byte).
func TestLogsCmd_RawAgentOneShot(t *testing.T) {
	sock := startRawBridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("follow") != "" {
			t.Errorf("one-shot must not set follow, got %q", r.URL.RawQuery)
		}
		if q.Get("after_byte") != "" {
			t.Errorf("one-shot must not set after_byte, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("historic-bytes\n"))
	})

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--raw", "--agent", "supervisor", "sess-123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "historic-bytes") {
		t.Fatalf("one-shot missing bytes, got %q", out.String())
	}
}

// TestLogsCmd_RawRejectsSince verifies that combining --raw with --since is
// rejected, since raw mode serves bytes, not timestamped events.
func TestLogsCmd_RawRejectsSince(t *testing.T) {
	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--raw", "--agent", "supervisor", "--since", "5", "sess-123"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error combining --raw with --since, got nil")
	}
	if !strings.Contains(err.Error(), "since") {
		t.Fatalf("error should mention --since, got %q", err.Error())
	}
}
