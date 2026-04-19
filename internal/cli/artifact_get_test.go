package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// startArtifactGetServer spins a stub daemon socket that serves
// GET /sessions/{id}/artifacts/{artifact_id} with the provided handler,
// and returns the socket path.
func startArtifactGetServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions/{id}/artifacts/{artifact_id}", h)
	ts := httptest.NewUnstartedServer(mux)
	tmp, err := os.MkdirTemp("", "bla")
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

// TestArtifactGet_ToStdout verifies that artifact get writes bytes to stdout
// when no -o flag is given.
func TestArtifactGet_ToStdout(t *testing.T) {
	const wantContent = "artifact content bytes\n"

	sock := startArtifactGetServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(wantContent)) //nolint:errcheck
	})

	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"artifact", "get", "--socket", sock, "sess-123", "art-456"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := out.String(); got != wantContent {
		t.Fatalf("expected stdout %q, got %q", wantContent, got)
	}
}

// TestArtifactGet_ToFile verifies that -o writes bytes to the given file.
func TestArtifactGet_ToFile(t *testing.T) {
	const wantContent = "file artifact content\n"

	sock := startArtifactGetServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(wantContent)) //nolint:errcheck
	})

	outFile := filepath.Join(t.TempDir(), "artifact.txt")

	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"artifact", "get", "--socket", sock, "-o", outFile, "sess-123", "art-456"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(got) != wantContent {
		t.Fatalf("expected file content %q, got %q", wantContent, string(got))
	}
	// Nothing should be written to stdout when -o is set.
	if out.Len() != 0 {
		t.Fatalf("expected no stdout when -o is set, got %q", out.String())
	}
}

// TestArtifactGet_Non200Error verifies that a non-200 response results in an
// error.
func TestArtifactGet_Non200Error(t *testing.T) {
	sock := startArtifactGetServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`)) //nolint:errcheck
	})

	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"artifact", "get", "--socket", sock, "sess-123", "no-such-artifact"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}
