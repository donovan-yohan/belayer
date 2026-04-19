package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithArtifactBytes returns a daemon with the artifact-bytes route
// registered.
func testDaemonWithArtifactBytes(t *testing.T) *Daemon {
	t.Helper()
	d := testDaemon(t)
	d.server.Handler.(*http.ServeMux).HandleFunc(
		"GET /sessions/{id}/artifacts/{artifact_id}",
		d.handleGetArtifactBytes,
	)
	return d
}

func TestGetArtifactBytes_OK(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	// Create a workspace with a real file.
	workspace := t.TempDir()
	const content = "hello artifact bytes\n"
	artPath := filepath.Join(workspace, "output.txt")
	if err := os.WriteFile(artPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create session with a workspace dir.
	sessID, err := d.store.CreateSession(store.Session{
		Name:         "artifact-bytes-test",
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Create artifact with relative path.
	artID, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sessID,
		Kind:      "output",
		Path:      "output.txt",
		Producer:  "test",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/artifacts/"+artID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != content {
		t.Fatalf("expected body %q, got %q", content, got)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/") {
		t.Fatalf("expected text/* content-type, got %q", ct)
	}
	disp := rr.Header().Get("Content-Disposition")
	if disp != "inline" {
		t.Fatalf("expected inline disposition for text file, got %q", disp)
	}
}

func TestGetArtifactBytes_AbsolutePath(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	workspace := t.TempDir()
	const content = "absolute path content\n"
	artPath := filepath.Join(workspace, "abs.txt")
	if err := os.WriteFile(artPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sessID, err := d.store.CreateSession(store.Session{
		Name:         "abs-path-test",
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Use the absolute path directly.
	artID, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sessID,
		Kind:      "output",
		Path:      artPath, // absolute
		Producer:  "test",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/artifacts/"+artID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != content {
		t.Fatalf("expected body %q, got %q", content, got)
	}
}

func TestGetArtifactBytes_UnknownArtifact(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	sessID, err := d.store.CreateSession(store.Session{Name: "no-artifact"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/artifacts/nonexistent-id", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetArtifactBytes_WrongSession(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	workspace := t.TempDir()
	artPath := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(artPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sess1ID, err := d.store.CreateSession(store.Session{Name: "sess1", WorkspaceDir: workspace})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess2ID, err := d.store.CreateSession(store.Session{Name: "sess2", WorkspaceDir: workspace})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Create artifact under sess1.
	artID, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sess1ID,
		Kind:      "output",
		Path:      "file.txt",
		Producer:  "test",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	// Request with sess2 — should 404.
	rr := doRequest(t, d, "GET", "/sessions/"+sess2ID+"/artifacts/"+artID, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong session, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetArtifactBytes_PathTraversal(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	sessID, err := d.store.CreateSession(store.Session{Name: "traversal-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Percent-encoded path traversal in artifact_id.
	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/artifacts/..%2Fetc", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetArtifactBytes_AttachmentDisposition(t *testing.T) {
	d := testDaemonWithArtifactBytes(t)

	workspace := t.TempDir()
	const content = "\x00binary data\x01"
	artPath := filepath.Join(workspace, "archive.bin")
	if err := os.WriteFile(artPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sessID, err := d.store.CreateSession(store.Session{Name: "bin-test", WorkspaceDir: workspace})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	artID, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sessID,
		Kind:      "binary",
		Path:      "archive.bin",
		Producer:  "test",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/artifacts/"+artID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	disp := rr.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disp, "attachment;") {
		t.Fatalf("expected attachment disposition for binary file, got %q", disp)
	}
}
