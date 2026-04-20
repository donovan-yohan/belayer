package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// writeBridgeLog seeds the per-agent bridge-stdout.log under workdir so the
// handlers can serve it.
func writeBridgeLog(t *testing.T, workdir, sessionID, agentName, content string) string {
	t.Helper()
	dir := filepath.Join(workdir, ".belayer", "runs", sessionID, agentName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bridge-stdout.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedAgentRun(t *testing.T, d *Daemon, sessionID, agentName, workdir string) {
	t.Helper()
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      agentName,
		Role:      "mock",
		Profile:   "mock",
		Workdir:   workdir,
		Transport: "bridge",
		Status:    "running",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleListBridges_EmptyWhenNoLogs(t *testing.T) {
	d := testDaemon(t)
	// Create session + register agent without any log file.
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "nobridges"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	seedAgentRun(t, d, sess.ID, "sup", t.TempDir())

	rr = doRequest(t, d, "GET", "/sessions/"+sess.ID+"/bridges", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	descs := decodeJSON[[]bridgeLogDescriptor](t, rr)
	if len(descs) != 0 {
		t.Fatalf("want empty list, got %v", descs)
	}
}

func TestHandleListBridges_ReturnsEntriesWithRotationCount(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "withbridges"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	workdir := t.TempDir()
	seedAgentRun(t, d, sess.ID, "sup", workdir)

	logPath := writeBridgeLog(t, workdir, sess.ID, "sup", "line1\nline2\n")
	// Seed two rotations.
	for i := 1; i <= 2; i++ {
		if err := os.WriteFile(fmt.Sprintf("%s.%d", logPath, i), []byte("prior"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	rr = doRequest(t, d, "GET", "/sessions/"+sess.ID+"/bridges", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	descs := decodeJSON[[]bridgeLogDescriptor](t, rr)
	if len(descs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(descs))
	}
	if descs[0].Agent != "sup" {
		t.Fatalf("agent = %q", descs[0].Agent)
	}
	if descs[0].Size != int64(len("line1\nline2\n")) {
		t.Fatalf("size = %d", descs[0].Size)
	}
	if descs[0].RotatedFiles != 2 {
		t.Fatalf("rotated = %d (want 2)", descs[0].RotatedFiles)
	}
}

func TestHandleBridgeStdout_ServesFull(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-full"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	workdir := t.TempDir()
	seedAgentRun(t, d, sess.ID, "sup", workdir)
	writeBridgeLog(t, workdir, sess.ID, "sup", "hello world\n")

	rr = doRequest(t, d, "GET", "/sessions/"+sess.ID+"/bridges/sup/stdout", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("serve: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "hello world\n" {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestHandleBridgeStdout_TailReturnsLastNBytes(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-tail"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	workdir := t.TempDir()
	seedAgentRun(t, d, sess.ID, "sup", workdir)
	writeBridgeLog(t, workdir, sess.ID, "sup", "abcdefghij")

	rr = doRequest(t, d, "GET", "/sessions/"+sess.ID+"/bridges/sup/stdout?tail=4", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("serve: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "ghij" {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestHandleBridgeStdout_404WhenMissing(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-404"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	seedAgentRun(t, d, sess.ID, "ghost", t.TempDir())

	rr = doRequest(t, d, "GET", "/sessions/"+sess.ID+"/bridges/ghost/stdout", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleBridgeStdout_FollowStreamsAppendedBytes(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-follow"})
	sess := decodeJSON[sessionAPIResponse](t, rr)
	workdir := t.TempDir()
	seedAgentRun(t, d, sess.ID, "sup", workdir)
	path := writeBridgeLog(t, workdir, sess.ID, "sup", "first\n")

	// Use httptest.NewServer for real streaming over a socket.
	srv := httptest.NewServer(d.server.Handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// follow from byte 0 so we see the seed bytes too.
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sessions/"+sess.ID+"/bridges/sup/stdout?follow=1&after_byte=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read enough bytes for "first\n".
	buf := make([]byte, 6)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "first\n" {
		t.Fatalf("first read = %q", buf)
	}

	// Append and read again.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	buf2 := make([]byte, 7)
	if _, err := io.ReadFull(resp.Body, buf2); err != nil {
		t.Fatal(err)
	}
	if string(buf2) != "second\n" {
		t.Fatalf("second read = %q", buf2)
	}

	// Cancel the request to unblock the handler cleanly.
	cancel()
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = strings.ToLower // silence "imported and not used" if tests drop a future ref
}
