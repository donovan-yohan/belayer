package daemon

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithTranscripts returns a daemon with the transcript routes
// registered.
func testDaemonWithTranscripts(t *testing.T) *Daemon {
	t.Helper()
	d := testDaemon(t)
	mux := d.server.Handler.(*http.ServeMux)
	mux.HandleFunc("GET /sessions/{id}/transcripts", d.handleListTranscripts)
	// The Go mux does not allow literal text after a wildcard segment; the route
	// must capture the full basename (including .jsonl) and the handler strips
	// the suffix itself.
	mux.HandleFunc("GET /sessions/{id}/transcripts/{agent}", d.handleTranscriptContent)
	return d
}

// makeTranscriptSession creates a session with the given log level and a
// workspace dir under t.TempDir(). Returns the session ID and workspace dir.
func makeTranscriptSession(t *testing.T, d *Daemon, logLevel string) (sessID, workspace string) {
	t.Helper()
	workspace = t.TempDir()
	var err error
	sessID, err = d.store.CreateSession(store.Session{
		Name:         "transcript-test-" + logLevel,
		LogLevel:     logLevel,
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sessID, workspace
}

// writeTranscript writes content to <workspace>/.belayer/climbs/<sess>/transcripts/<agent>.jsonl.
func writeTranscript(t *testing.T, workspace, sessID, agent, content string) string {
	t.Helper()
	dir := filepath.Join(workspace, ".belayer", "climbs", sessID, "transcripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll transcripts dir: %v", err)
	}
	path := filepath.Join(dir, agent+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile transcript: %v", err)
	}
	return path
}

// TestListTranscripts_404IfStandardTier verifies that sessions at standard log
// level return 404 from GET /sessions/{id}/transcripts.
func TestListTranscripts_404IfStandardTier(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, _ := makeTranscriptSession(t, d, LogLevelStandard)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for standard tier, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestListTranscripts_OK verifies that a verbose-tier session with two
// transcript files returns 200 with both entries.
func TestListTranscripts_OK(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)

	writeTranscript(t, workspace, sessID, "backend-dev", `{"role":"user","content":"hello"}`+"\n")
	writeTranscript(t, workspace, sessID, "supervisor", `{"role":"assistant","content":"ok"}`+"\n")

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	entries := decodeJSON[[]transcriptEntry](t, rr)
	if len(entries) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d: %#v", len(entries), entries)
	}
	agents := map[string]bool{}
	for _, e := range entries {
		agents[e.Agent] = true
		if e.Size == 0 {
			t.Errorf("entry %q has zero size", e.Agent)
		}
	}
	if !agents["backend-dev"] || !agents["supervisor"] {
		t.Fatalf("unexpected agents in listing: %v", agents)
	}
}

// TestListTranscripts_TraceIncluded verifies that trace-tier sessions also
// return transcripts (trace >= verbose).
func TestListTranscripts_TraceIncluded(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelTrace)
	writeTranscript(t, workspace, sessID, "reviewer", `{"line":1}`+"\n")

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for trace tier, got %d: %s", rr.Code, rr.Body.String())
	}
	entries := decodeJSON[[]transcriptEntry](t, rr)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

// TestListTranscripts_EmptyDir verifies that a verbose session with no
// transcripts yet returns 200 + empty array.
func TestListTranscripts_EmptyDir(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, _ := makeTranscriptSession(t, d, LogLevelVerbose)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty transcripts, got %d: %s", rr.Code, rr.Body.String())
	}
	entries := decodeJSON[[]transcriptEntry](t, rr)
	if len(entries) != 0 {
		t.Fatalf("expected empty array, got %d entries", len(entries))
	}
}

// TestTranscriptContent_Full verifies that the full file is served when no
// query params are given.
func TestTranscriptContent_Full(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)

	lines := ""
	for i := 0; i < 10; i++ {
		lines += fmt.Sprintf(`{"n":%d}`+"\n", i)
	}
	writeTranscript(t, workspace, sessID, "backend-dev", lines)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts/backend-dev.jsonl", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != lines {
		t.Fatalf("expected full content, got %q", got)
	}
}

// TestTranscriptContent_Tail verifies that ?tail=<bytes> returns only the last
// N bytes.
func TestTranscriptContent_Tail(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)

	// 10 lines, each exactly 10 bytes: `{"n":NN}` + newline padded to 10.
	var allLines []string
	for i := 0; i < 10; i++ {
		allLines = append(allLines, fmt.Sprintf(`{"n":%d}`+"\n", i))
	}
	content := strings.Join(allLines, "")
	writeTranscript(t, workspace, sessID, "backend-dev", content)

	// Request only the last 3 lines.
	last3 := strings.Join(allLines[7:], "")
	tailBytes := int64(len(last3))

	url := fmt.Sprintf("/sessions/%s/transcripts/backend-dev.jsonl?tail=%d", sessID, tailBytes)
	rr := doRequest(t, d, "GET", url, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != last3 {
		t.Fatalf("tail: expected %q, got %q", last3, got)
	}
}

// TestTranscriptContent_PathTraversal verifies that a percent-encoded path
// traversal in the agent component is rejected with 400.
func TestTranscriptContent_PathTraversal(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, _ := makeTranscriptSession(t, d, LogLevelVerbose)

	// %2F is a percent-encoded '/'; after PathValue decoding this becomes "../escape"
	url := fmt.Sprintf("/sessions/%s/transcripts/..%%2Fescape.jsonl", sessID)
	rr := doRequest(t, d, "GET", url, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTranscriptContent_NotFound verifies that a non-existent agent transcript
// returns 404.
func TestTranscriptContent_NotFound(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, _ := makeTranscriptSession(t, d, LogLevelVerbose)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts/ghost.jsonl", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing transcript, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTranscriptContent_404IfStandardTier verifies that serving content also
// requires verbose or trace tier.
func TestTranscriptContent_404IfStandardTier(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelStandard)
	writeTranscript(t, workspace, sessID, "backend-dev", `{"n":1}`+"\n")

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/transcripts/backend-dev.jsonl", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for standard tier, got %d: %s", rr.Code, rr.Body.String())
	}
}

// readFollowLines consumes newline-delimited records from r into a channel
// until the caller cancels or the reader returns error. Helper for
// ?follow=1 tests — every appended record should appear as a scanner line.
func readFollowLines(t *testing.T, r interface {
	Read([]byte) (int, error)
}) <-chan string {
	t.Helper()
	ch := make(chan string, 128)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			ch <- scanner.Text()
		}
	}()
	return ch
}

// waitForLine blocks until ch yields want or timeout expires. Returns nothing
// — on failure it t.Fatals so call sites stay linear.
func waitForLine(t *testing.T, ch <-chan string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				t.Fatalf("follow stream closed before receiving %q", want)
			}
			if line == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for follow line %q", want)
		}
	}
}

// TestTranscriptFollow_AppendsStream verifies the baseline follow contract:
// bytes written to the transcript after the client connects are delivered.
func TestTranscriptFollow_AppendsStream(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)
	path := writeTranscript(t, workspace, sessID, "backend-dev", `{"seq":0}`+"\n")

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/transcripts/backend-dev.jsonl?follow=1", server.URL, sessID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET follow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	lines := readFollowLines(t, resp.Body)
	waitForLine(t, lines, `{"seq":0}`, 2*time.Second)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString(`{"seq":1}` + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = f.Close()

	waitForLine(t, lines, `{"seq":1}`, 2*time.Second)
}

// TestTranscriptFollow_RotatedFile verifies that when the transcript is
// replaced at its path (log rotation: rename-old + create-new), the tail
// picks up bytes written to the new inode. Prior behavior pinned the fd to
// the original inode and silently dropped post-rotation writes.
func TestTranscriptFollow_RotatedFile(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)
	path := writeTranscript(t, workspace, sessID, "backend-dev", `{"seq":"pre"}`+"\n")

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/transcripts/backend-dev.jsonl?follow=1", server.URL, sessID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET follow: %v", err)
	}
	defer resp.Body.Close()

	lines := readFollowLines(t, resp.Body)
	waitForLine(t, lines, `{"seq":"pre"}`, 2*time.Second)

	// Rotate: move the old file aside and create a new file at the same path.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"seq":"post"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write rotated file: %v", err)
	}

	waitForLine(t, lines, `{"seq":"post"}`, 3*time.Second)
}

// TestTranscriptFollow_TruncatedFile verifies in-place truncation recovery:
// if an external process rewrites the file via O_TRUNC the tail must reset
// its read offset so the fresh contents flush through. Otherwise the fd's
// offset (now past EOF) stays pinned and no further bytes are delivered.
func TestTranscriptFollow_TruncatedFile(t *testing.T) {
	d := testDaemonWithTranscripts(t)
	sessID, workspace := makeTranscriptSession(t, d, LogLevelVerbose)
	path := writeTranscript(t, workspace, sessID, "backend-dev", `{"seq":"pre-trunc"}`+"\n")

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/transcripts/backend-dev.jsonl?follow=1", server.URL, sessID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET follow: %v", err)
	}
	defer resp.Body.Close()

	lines := readFollowLines(t, resp.Body)
	waitForLine(t, lines, `{"seq":"pre-trunc"}`, 2*time.Second)

	// Truncate in place, then (after a gap > the tail's poll interval) write
	// a new record. The gap lets the tail observe size=0 and rewind — the
	// realistic pattern for in-place rotation. Back-to-back trunc+write in
	// one syscall burst is fundamentally racy without filesystem
	// notifications and is not what streamTranscript guarantees.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := os.WriteFile(path, []byte(`{"seq":"post-trunc"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write after truncate: %v", err)
	}

	waitForLine(t, lines, `{"seq":"post-trunc"}`, 3*time.Second)
}
