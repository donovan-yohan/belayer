package daemon

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// writeTranscript writes content to <workspace>/.belayer/runs/<sess>/transcripts/<agent>.jsonl.
func writeTranscript(t *testing.T, workspace, sessID, agent, content string) string {
	t.Helper()
	dir := filepath.Join(workspace, ".belayer", "runs", sessID, "transcripts")
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
