package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBridgesTailCmd_StreamsFollowMode verifies that `belayer bridges tail
// <session> <agent>` streams raw bytes from the bridge stdout endpoint with
// follow=1 by default.
//
// Test-plan item: T2.5 — belayer bridges tail shorthand.
func TestBridgesTailCmd_StreamsFollowMode(t *testing.T) {
	sock := startRawBridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("follow") != "1" {
			t.Errorf("want follow=1, got %q", r.URL.RawQuery)
		}
		if got := q.Get("after_byte"); got != "0" {
			t.Errorf("want after_byte=0, got %q", got)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		w.Write([]byte("tailed-bytes\n"))
		if fl != nil {
			fl.Flush()
		}
	})

	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bridges", "tail", "--socket", sock, "sess-123", "supervisor"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cmd.ExecuteContext(ctx)

	if !strings.Contains(out.String(), "tailed-bytes") {
		t.Fatalf("missing bytes in output, got %q", out.String())
	}
}

// TestBridgesTailCmd_OneShotWhenFollowFalse verifies --follow=false bypasses
// follow/after_byte query params (one-shot full-file fetch).
func TestBridgesTailCmd_OneShotWhenFollowFalse(t *testing.T) {
	sock := startRawBridgeServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("follow") != "" {
			t.Errorf("one-shot must not set follow, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("full-history\n"))
	})

	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"bridges", "tail", "--socket", sock, "--follow=false", "sess-123", "supervisor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "full-history") {
		t.Fatalf("missing bytes in output, got %q", out.String())
	}
}
