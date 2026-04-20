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

// testDaemonWithTraceSliceRoute returns a daemon with the trace-slice route
// registered, a trace-tier session ID, and a pre-populated fragment file under
// <traceBase>/<sessID>/myagent/0001.jsonl containing exactly 100 bytes.
func testDaemonWithTraceSliceRoute(t *testing.T) (d *Daemon, sessID string, fragContent []byte) {
	t.Helper()
	d, dir := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "slice-test",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Pre-populate a fragment file so the handler has something to read.
	agentDir := filepath.Join(dir, "traces", sessID, "myagent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fragContent = []byte(`{"hello":"world"}` + "\n")
	// Pad to 100 bytes so range tests have meaningful bounds.
	for len(fragContent) < 100 {
		fragContent = append(fragContent, '\n')
	}
	fragPath := filepath.Join(agentDir, "0001.jsonl")
	if err := os.WriteFile(fragPath, fragContent, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return d, sessID, fragContent
}

// TestTraceSlice_RejectsPathTraversalInAgent verifies that a percent-encoded
// slash in the agent path component (decoded by PathValue to "../etc") is
// rejected with 400.
func TestTraceSlice_RejectsPathTraversalInAgent(t *testing.T) {
	d, sessID, _ := testDaemonWithTraceSliceRoute(t)

	// %2F is a percent-encoded '/'; PathValue decodes it to "../etc"
	path := fmt.Sprintf("/sessions/%s/trace/..%%2Fetc/0001?offset=0&length=10", sessID)
	rr := doRequest(t, d, "GET", path, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal in agent, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTraceSlice_RejectsPathTraversalInFragment verifies that a percent-encoded
// traversal sequence in the fragment path value is rejected with 400.
func TestTraceSlice_RejectsPathTraversalInFragment(t *testing.T) {
	d, sessID, _ := testDaemonWithTraceSliceRoute(t)

	// PathValue decodes %2F, so "..%2F..%2Fetc" becomes "../../etc"
	path := fmt.Sprintf("/sessions/%s/trace/myagent/..%%2F..%%2Fetc?offset=0&length=10", sessID)
	rr := doRequest(t, d, "GET", path, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal in fragment, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTraceSlice_RejectsPathTraversalInSession verifies that a session ID
// containing ".." (percent-encoded) is rejected with 400.
func TestTraceSlice_RejectsPathTraversalInSession(t *testing.T) {
	d, _, _ := testDaemonWithTraceSliceRoute(t)

	// Use a session ID that contains an encoded path separator.
	path := "/sessions/..%2Fsomething/trace/myagent/0001?offset=0&length=10"
	rr := doRequest(t, d, "GET", path, nil)
	// The handler should reject this with 400 (bad path component).
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal in session, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTraceSlice_RejectsNegativeOffset verifies that offset=-1 yields 400.
func TestTraceSlice_RejectsNegativeOffset(t *testing.T) {
	d, sessID, _ := testDaemonWithTraceSliceRoute(t)

	path := fmt.Sprintf("/sessions/%s/trace/myagent/0001?offset=-1&length=10", sessID)
	rr := doRequest(t, d, "GET", path, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative offset, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTraceSlice_RejectsSymlinkEscape verifies that a symlink under traceBase
// pointing to a file outside traceBase is rejected with 400, not served.
// filepath.Clean does not resolve symlinks, so the byte-level prefix check
// above was insufficient — a malicious agent (or a writer bug) creating a
// symlink like traceBase/<sess>/<agent>/0001.jsonl -> /etc/hostname would
// otherwise expose arbitrary readable files.
//
// Addresses CodeRabbit critical: trace HTTP endpoint could be used to read any
// file the daemon process could open by planting a symlink inside traceBase.
func TestTraceSlice_RejectsSymlinkEscape(t *testing.T) {
	d, dir := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "symlink-escape-test",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Write the target file somewhere outside traceBase.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("top-secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a symlink inside traceBase that points at the outside file.
	agentDir := filepath.Join(dir, "traces", sessID, "myagent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	linkPath := filepath.Join(agentDir, "0001.jsonl")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	path := fmt.Sprintf("/sessions/%s/trace/myagent/0001?offset=0&length=10", sessID)
	rr := doRequest(t, d, "GET", path, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for symlink-escape, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "top-secret") {
		t.Fatalf("leaked target contents via symlink: %s", rr.Body.String())
	}
}

// TestTraceSlice_RejectsOverflowRange verifies that a large length that would
// overflow int64 when added to offset is rejected with 416, not a panic.
func TestTraceSlice_RejectsOverflowRange(t *testing.T) {
	d, sessID, _ := testDaemonWithTraceSliceRoute(t)

	// offset=1, length=max int64-ish — sum would overflow int64.
	path := fmt.Sprintf("/sessions/%s/trace/myagent/0001?offset=1&length=9223372036854775800", sessID)
	rr := doRequest(t, d, "GET", path, nil)
	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("expected 416 for overflow range, got %d: %s", rr.Code, rr.Body.String())
	}
}
