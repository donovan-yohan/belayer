package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	cmd.SetArgs([]string{"--raw", "--agent", "supervisor", "--since", "5m", "sess-123"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("want error combining --raw with --since, got nil")
	}
	if !strings.Contains(err.Error(), "since") {
		t.Fatalf("error should mention --since, got %q", err.Error())
	}
}

// TestLogsCmd_NDJSONFormat verifies that --format ndjson emits one JSON object
// per event per line, with the backfill filtered by --tail.
func TestLogsCmd_NDJSONFormat(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)
	sess, err := c.CreateSession("logs-ndjson", "nightshift", nil, "", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := c.LogEvent(sess.ID, "noop_test", mustJSON(map[string]int{"i": i})); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}
	}

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--format", "ndjson", "--type", "noop_test", sess.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 ndjson lines, got %d: %q", len(lines), out.String())
	}
	for _, line := range lines {
		var e eventResponse
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal ndjson line %q: %v", line, err)
		}
		if e.Type != "noop_test" {
			t.Fatalf("expected type=noop_test, got %q", e.Type)
		}
	}
}

// TestLogsCmd_FilterByAgent verifies that --agent narrows backfill to events
// whose data payload names that agent.
func TestLogsCmd_FilterByAgent(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)
	sess, err := c.CreateSession("logs-agent-filter", "nightshift", nil, "", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := c.LogEvent(sess.ID, "agent_note", mustJSON(map[string]string{"agent": "pm", "msg": "hi"})); err != nil {
		t.Fatalf("LogEvent pm: %v", err)
	}
	if err := c.LogEvent(sess.ID, "agent_note", mustJSON(map[string]string{"agent": "qa", "msg": "hi"})); err != nil {
		t.Fatalf("LogEvent qa: %v", err)
	}

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--agent", "pm", "--type", "agent_note", "--format", "ndjson", sess.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 event for agent pm, got %d: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `\"agent\":\"pm\"`) {
		t.Fatalf("expected pm event, got %q", lines[0])
	}
	if strings.Contains(out.String(), `\"agent\":\"qa\"`) {
		t.Fatalf("qa event leaked through filter: %q", out.String())
	}
}

// TestLogsCmd_TierFiltersBackfill verifies --tier caps backfill the same way
// SSE does. Seeds a mix of standard/verbose/trace-tier events and asserts
// --tier standard drops verbose and trace events on one-shot backfill.
func TestLogsCmd_TierFiltersBackfill(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)
	sess, err := c.CreateSession("logs-tier", "nightshift", nil, "", "trace")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Standard tier: plain type.
	if err := c.LogEvent(sess.ID, "plain_event", mustJSON(map[string]string{"x": "s"})); err != nil {
		t.Fatalf("LogEvent plain: %v", err)
	}
	// Verbose tier: type prefix bridge:.
	if err := c.LogEvent(sess.ID, "bridge:note", mustJSON(map[string]string{"x": "v"})); err != nil {
		t.Fatalf("LogEvent bridge: %v", err)
	}
	// Trace tier: type prefix trace:.
	if err := c.LogEvent(sess.ID, "trace:step", mustJSON(map[string]string{"x": "t"})); err != nil {
		t.Fatalf("LogEvent trace: %v", err)
	}

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--tier", "standard", "--format", "ndjson", sess.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs: %v", err)
	}

	body := out.String()
	if strings.Contains(body, "bridge:note") {
		t.Fatalf("tier=standard should drop bridge: events, got:\n%s", body)
	}
	if strings.Contains(body, "trace:step") {
		t.Fatalf("tier=standard should drop trace: events, got:\n%s", body)
	}
	if !strings.Contains(body, "plain_event") {
		t.Fatalf("tier=standard should include standard events, got:\n%s", body)
	}
}

// TestLogsCmd_TailLimits verifies --tail N returns only the last N matching events.
func TestLogsCmd_TailLimits(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)
	sess, err := c.CreateSession("logs-tail", "nightshift", nil, "", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := c.LogEvent(sess.ID, "tick", mustJSON(map[string]int{"n": i})); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}
	}

	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--socket", sock, "--type", "tick", "--tail", "2", "--format", "ndjson", sess.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines with --tail 2, got %d: %q", len(lines), out.String())
	}
	// Last two ticks are n=3 and n=4. The Data field on eventResponse is a
	// JSON-encoded string, so the rendered ndjson escapes its inner quotes.
	if !strings.Contains(lines[0], `\"n\":3`) || !strings.Contains(lines[1], `\"n\":4`) {
		t.Fatalf("unexpected tail events: %q", out.String())
	}
}

// TestResolveSessionArg covers the name/ID/prefix resolution rules including
// ambiguity rejection. Uses deterministic in-memory sessions rather than a
// live daemon so we can construct a guaranteed prefix collision.
func TestResolveSessionArg(t *testing.T) {
	sessions := []sessionResponse{
		{ID: "abc123-alpha", Name: "alpha"},
		{ID: "abc456-beta", Name: "beta"},
		{ID: "xyz789-gamma", Name: "gamma"},
	}

	tests := []struct {
		name    string
		arg     string
		wantID  string
		wantErr string
	}{
		{"exact full ID", "abc123-alpha", "abc123-alpha", ""},
		{"exact name", "alpha", "abc123-alpha", ""},
		{"unique prefix", "xyz", "xyz789-gamma", ""},
		{"ambiguous prefix", "abc", "", "ambiguous"},
		{"ambiguous prefix longer", "abc1", "abc123-alpha", ""},
		{"no match", "nope", "", "no session found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSessionArg(sessions, tc.arg)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error %q, got id=%q nil", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Fatalf("got %q, want %q", got, tc.wantID)
			}
		})
	}
}
