package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	archivePkg "github.com/donovan-yohan/belayer/internal/archive"
	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/donovan-yohan/belayer/internal/store"
)

// startTestDaemon starts a real daemon on a temp socket and returns its socket
// path and a cleanup function. The daemon is shut down when t.Cleanup fires.
// Uses os.MkdirTemp with a short name to stay within the 104-char Unix socket
// path limit on macOS.
func startTestDaemon(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("", "bld")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	sockPath := filepath.Join(tmp, "d.sock")
	dbPath := filepath.Join(tmp, "d.db")

	d, err := daemon.New(daemon.Config{
		SocketPath: sockPath,
		DBPath:     dbPath,
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Start(ctx) }()

	// Wait until the socket is ready.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c := NewClient(sockPath)
		if _, err := c.Health(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		<-errc
	})
	return sockPath
}

// runArchiveCmd executes the archive command with the given args and returns
// stdout, stderr, and the error.
func runArchiveCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newArchiveCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestArchiveCmd_Success(t *testing.T) {
	sock := startTestDaemon(t)
	outDir := t.TempDir()
	c := NewClient(sock)

	// Create a session with a workspace dir.
	sess, err := c.CreateSession("archive-test", "implement", nil, outDir)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Log a few events.
	for i := 0; i < 3; i++ {
		data := fmt.Sprintf(`{"msg":"event %d"}`, i)
		if err := c.LogEvent(sess.ID, "custom_event", data); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}
	}

	destDir := filepath.Join(outDir, "arch-out")
	stdout, _, err := runArchiveCmd(t,
		sess.ID,
		"--socket", sock,
		"--output", destDir,
	)
	if err != nil {
		t.Fatalf("archive cmd: %v", err)
	}
	if !strings.Contains(stdout, sess.ID) {
		t.Errorf("expected session ID in output, got: %s", stdout)
	}
	// The daemon also logs a session_created event, so total is 4 (1 + 3 custom).
	if !strings.Contains(stdout, "event_count=4") {
		t.Errorf("expected event_count=4, got: %s", stdout)
	}

	// Verify both files exist.
	eventsPath := filepath.Join(destDir, "events.ndjson")
	manifestPath := filepath.Join(destDir, "manifest.json")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("events.ndjson missing: %v", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}

	// Verify manifest has expected fields.
	raw, _ := os.ReadFile(manifestPath)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m["schema_version"] != "belayer-log/v1" {
		t.Errorf("schema_version: got %v", m["schema_version"])
	}
	// 3 custom events + 1 session_created = 4 total.
	if m["event_count"] != float64(4) {
		t.Errorf("event_count: got %v", m["event_count"])
	}
}

// TestArchiveCmd_ManifestHasDaemonInstanceID verifies that the archive manifest
// carries a non-empty daemon_instance_id fetched from GET /health.
func TestArchiveCmd_ManifestHasDaemonInstanceID(t *testing.T) {
	sock := startTestDaemon(t)
	outDir := t.TempDir()
	c := NewClient(sock)

	sess, err := c.CreateSession("epoch-test", "implement", nil, outDir)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	destDir := outDir + "/arch"
	_, _, err = runArchiveCmd(t, sess.ID, "--socket", sock, "--output", destDir)
	if err != nil {
		t.Fatalf("archive cmd: %v", err)
	}

	raw, _ := os.ReadFile(destDir + "/manifest.json")
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	id, _ := m["daemon_instance_id"].(string)
	if id == "" {
		t.Error("manifest.daemon_instance_id must be non-empty (fetched from /health)")
	}
}

func TestArchiveCmd_MissingSession(t *testing.T) {
	sock := startTestDaemon(t)
	_, _, err := runArchiveCmd(t,
		"nonexistent-session-id",
		"--socket", sock,
		"--output", t.TempDir(),
	)
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no session") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestArchiveCmd_OutputOverride(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)

	sess, err := c.CreateSession("output-override-test", "implement", nil, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "custom-output")
	stdout, _, err := runArchiveCmd(t,
		sess.ID,
		"--socket", sock,
		"--output", destDir,
	)
	if err != nil {
		t.Fatalf("archive cmd: %v", err)
	}
	if !strings.Contains(stdout, destDir) {
		t.Errorf("expected custom output dir in stdout, got: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(destDir, "manifest.json")); err != nil {
		t.Fatalf("manifest.json not in custom dir: %v", err)
	}
}

func TestArchiveCmd_EmptyWorkspaceNoOutput(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)

	// Session with no workspace dir.
	sess, err := c.CreateSession("no-workspace", "implement", nil, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, _, err = runArchiveCmd(t,
		sess.ID,
		"--socket", sock,
		// No --output flag, no workspace dir set.
	)
	if err == nil {
		t.Fatal("expected error when workspace is empty and --output not set")
	}
	if !strings.Contains(err.Error(), "--output") {
		t.Errorf("expected '--output' mention in error, got: %v", err)
	}
}

// TestExtractArtifacts_SkipCounter validates that unparseable artifact_created
// events are surfaced via the skipped counter rather than silently dropped.
// The implementation lives in internal/archive; this test calls the shared function.
func TestExtractArtifacts_SkipCounter(t *testing.T) {
	events := []archivePkg.Event{
		{ID: 1, Type: "artifact_created", Data: json.RawMessage(`{"kind":"spec","path":"/tmp/spec.md"}`)},
		{ID: 2, Type: "artifact_created", Data: json.RawMessage(`not json at all`)},
		{ID: 3, Type: "bridge:heartbeat", Data: json.RawMessage(`{"agent":"sup"}`)},
		{ID: 4, Type: "artifact_created", Data: json.RawMessage(`{"path":"/tmp/x.md"}`)},
	}
	arts, skipped := archivePkg.ExtractArtifacts(events)
	if len(arts) != 1 {
		t.Errorf("expected 1 parseable artifact, got %d", len(arts))
	}
	if skipped != 2 {
		t.Errorf("expected skipped=2 (unparseable + missing kind), got %d", skipped)
	}
	if len(arts) > 0 && arts[0].Kind != "spec" {
		t.Errorf("expected kind=spec, got %s", arts[0].Kind)
	}
}

// TestArchiveCmd_RootCmdRegisters ensures the archive command is wired in.
func TestArchiveCmd_RootCmdRegisters(t *testing.T) {
	cmd := NewRootCmd()
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}
	if !seen["archive"] {
		t.Fatal("archive command not registered in root")
	}
}

// Compile-time check: ensure daemon and store packages are imported only for tests.
var _ = store.Session{}
var _ = daemon.Config{}
