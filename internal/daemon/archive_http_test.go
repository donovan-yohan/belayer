package daemon

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/archive"
)

// writeTestArchive uses archive.Write to produce the standard two-file layout
// (<ws>/.belayer/archive/<sessID>/) so HTTP handler tests have real files to serve.
func writeTestArchive(t *testing.T, ws, sessID string) {
	t.Helper()
	destDir := filepath.Join(ws, ".belayer", "archive", sessID)
	meta := archive.Meta{
		SchemaVersion:    "belayer-log/v1",
		DaemonInstanceID: "test-instance",
		Session:          archive.SessionMeta{ID: sessID, Name: "test-session", Workspace: ws},
		FinalStatus:      "complete",
		Partial:          false,
		ArchivedAt:       time.Now().UTC(),
	}
	events := []archive.Event{
		{ID: 1, SessionID: sessID, Timestamp: time.Now().UTC(), Type: "session_created", Data: []byte(`{"name":"test-session"}`)},
	}
	if _, err := archive.Write(destDir, meta, events); err != nil {
		t.Fatalf("writeTestArchive: %v", err)
	}
}

// TestArchiveHTTP_ServesNDJSON verifies that GET /sessions/{id}/archive.ndjson
// returns 200, the correct content-type, and body matching the on-disk file.
func TestArchiveHTTP_ServesNDJSON(t *testing.T) {
	d := testDaemon(t)
	ws := t.TempDir()

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "ndjson-test",
		WorkspaceDir: ws,
	}))

	writeTestArchive(t, ws, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/archive.ndjson", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/x-ndjson" {
		t.Errorf("Content-Type: got %q, want application/x-ndjson", ct)
	}

	// Verify body matches on-disk file.
	diskPath := filepath.Join(ws, ".belayer", "archive", sess.ID, "events.ndjson")
	diskBytes, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read on-disk events.ndjson: %v", err)
	}
	if rr.Body.String() != string(diskBytes) {
		t.Errorf("body mismatch:\ngot:  %s\nwant: %s", rr.Body.String(), string(diskBytes))
	}
}

// TestArchiveHTTP_ServesManifest verifies GET /sessions/{id}/archive/manifest.json.
func TestArchiveHTTP_ServesManifest(t *testing.T) {
	d := testDaemon(t)
	ws := t.TempDir()

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "manifest-test",
		WorkspaceDir: ws,
	}))

	writeTestArchive(t, ws, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/archive/manifest.json", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	// Parse manifest and verify key fields.
	var m map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m["schema_version"] != "belayer-log/v1" {
		t.Errorf("schema_version: got %v", m["schema_version"])
	}
	if m["final_status"] != "complete" {
		t.Errorf("final_status: got %v", m["final_status"])
	}
}

// TestArchiveHTTP_ServesTarGz verifies GET /sessions/{id}/archive.tar.gz contains
// both events.ndjson and manifest.json with correct content.
func TestArchiveHTTP_ServesTarGz(t *testing.T) {
	d := testDaemon(t)
	ws := t.TempDir()

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "tar-test",
		WorkspaceDir: ws,
	}))

	writeTestArchive(t, ws, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/archive.tar.gz", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/gzip" {
		t.Errorf("Content-Type: got %q, want application/gzip", ct)
	}

	// Decompress and verify tar entries.
	gr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	entries := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		entries[hdr.Name] = string(data)
	}

	if _, ok := entries["events.ndjson"]; !ok {
		t.Error("tar missing events.ndjson")
	}
	if _, ok := entries["manifest.json"]; !ok {
		t.Error("tar missing manifest.json")
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 tar entries, got %d: %v", len(entries), entries)
	}

	// Verify manifest.json parses correctly.
	if raw, ok := entries["manifest.json"]; ok {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("parse manifest.json from tar: %v", err)
		}
		if m["schema_version"] != "belayer-log/v1" {
			t.Errorf("manifest schema_version: %v", m["schema_version"])
		}
	}
}

// TestArchiveHTTP_404WhenNoArchive verifies that a 404 is returned when the
// session exists and has a workspace but no archive directory.
func TestArchiveHTTP_404WhenNoArchive(t *testing.T) {
	d := testDaemon(t)
	ws := t.TempDir()

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "no-archive",
		WorkspaceDir: ws,
	}))

	// Do NOT write an archive — archive dir does not exist.
	for _, path := range []string{
		"/sessions/" + sess.ID + "/archive.ndjson",
		"/sessions/" + sess.ID + "/archive/manifest.json",
		"/sessions/" + sess.ID + "/archive.tar.gz",
	} {
		rr := doRequest(t, d, "GET", path, nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rr.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Errorf("%s: decode error body: %v", path, err)
			continue
		}
		if body["error"] == "" {
			t.Errorf("%s: expected non-empty error field", path)
		}
	}
}

// TestArchiveHTTP_404WhenUnknownSession verifies that a random session ID yields 404.
func TestArchiveHTTP_404WhenUnknownSession(t *testing.T) {
	d := testDaemon(t)

	for _, path := range []string{
		"/sessions/nonexistent-id/archive.ndjson",
		"/sessions/nonexistent-id/archive/manifest.json",
		"/sessions/nonexistent-id/archive.tar.gz",
	} {
		rr := doRequest(t, d, "GET", path, nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rr.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Errorf("%s: decode error body: %v", path, err)
			continue
		}
		if !strings.Contains(body["error"], "not found") {
			t.Errorf("%s: error should mention 'not found', got %q", path, body["error"])
		}
	}
}

// TestArchiveHTTP_404WhenNoWorkspace verifies that a session with empty
// WorkspaceDir returns a clean 404 (no panic, no stack trace).
func TestArchiveHTTP_404WhenNoWorkspace(t *testing.T) {
	d := testDaemon(t)

	// Create session with no workspace.
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name: "no-workspace",
		// WorkspaceDir intentionally omitted.
	}))

	for _, path := range []string{
		"/sessions/" + sess.ID + "/archive.ndjson",
		"/sessions/" + sess.ID + "/archive/manifest.json",
		"/sessions/" + sess.ID + "/archive.tar.gz",
	} {
		rr := doRequest(t, d, "GET", path, nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rr.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
			t.Errorf("%s: decode error body: %v", path, err)
			continue
		}
		if !strings.Contains(body["error"], "workspace") {
			t.Errorf("%s: error should mention workspace, got %q", path, body["error"])
		}
	}
}

// TestArchiveHTTP_TarGzDispositionHeader verifies that the tar.gz response
// carries Content-Disposition: attachment so browsers download rather than render.
func TestArchiveHTTP_TarGzDispositionHeader(t *testing.T) {
	d := testDaemon(t)
	ws := t.TempDir()

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "disposition-test",
		WorkspaceDir: ws,
	}))

	writeTestArchive(t, ws, sess.ID)

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/sessions/" + sess.ID + "/archive.tar.gz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition should contain 'attachment', got %q", cd)
	}
	if !strings.Contains(cd, sess.ID+".tar.gz") {
		t.Errorf("Content-Disposition should contain session ID filename, got %q", cd)
	}
}
