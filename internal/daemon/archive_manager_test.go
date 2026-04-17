package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// openTestStore creates a file-backed SQLite store in t.TempDir() and
// registers a cleanup that closes it.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "belayer.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// createStoreSession inserts a session directly into st and returns its ID.
func createStoreSession(t *testing.T, st *store.Store, name, status, workspaceDir string) string {
	t.Helper()
	id, err := st.CreateSession(store.Session{
		Name:         name,
		Status:       status,
		WorkspaceDir: workspaceDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return id
}

// logTestEvent logs a minimal event for the given session.
func logTestEvent(t *testing.T, st *store.Store, sessionID, typ string) {
	t.Helper()
	if err := st.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      typ,
		Data:      `{}`,
	}); err != nil {
		t.Fatalf("LogEvent: %v", err)
	}
}

// waitForManifest polls until <workspace>/.belayer/archive/<id>/manifest.json
// exists or deadline passes, then returns its parsed contents.
func waitForManifest(t *testing.T, workspaceDir, sessionID string, timeout time.Duration) map[string]any {
	t.Helper()
	path := filepath.Join(workspaceDir, ".belayer", "archive", sessionID, "manifest.json")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("manifest not produced within %v at %s", timeout, path)
	return nil
}

func TestIsTerminalSessionStatus_IncludesStalled(t *testing.T) {
	terminal := []string{"complete", "blocked", "failed", "cancelled", "needs_human_review", "stalled"}
	for _, s := range terminal {
		if !isTerminalSessionStatus(s) {
			t.Errorf("isTerminalSessionStatus(%q) = false; want true", s)
		}
	}
	nonTerminal := []string{"running", "pending"}
	for _, s := range nonTerminal {
		if isTerminalSessionStatus(s) {
			t.Errorf("isTerminalSessionStatus(%q) = true; want false", s)
		}
	}
}

func TestArchiveManager_TerminalTransitionWritesArchive(t *testing.T) {
	ws := t.TempDir()
	st := openTestStore(t)

	id := createStoreSession(t, st, "term-test", "running", ws)
	// Log a few events.
	for i := 0; i < 3; i++ {
		logTestEvent(t, st, id, "custom_event")
	}
	// Transition to terminal status directly in the store.
	if err := st.UpdateSessionStatus(id, "complete"); err != nil {
		t.Fatalf("UpdateSessionStatus: %v", err)
	}

	m := newArchiveManager(st)
	m.ArchiveTerminal(id)

	manifest := waitForManifest(t, ws, id, 2*time.Second)

	if manifest["partial"] != false {
		t.Errorf("expected partial=false, got %v", manifest["partial"])
	}
	// 3 custom events; no session_created because we inserted via store directly.
	if manifest["event_count"] != float64(3) {
		t.Errorf("expected event_count=3, got %v", manifest["event_count"])
	}
}

func TestArchiveManager_DedupesConcurrentCalls(t *testing.T) {
	ws := t.TempDir()
	st := openTestStore(t)

	id := createStoreSession(t, st, "dedupe-test", "complete", ws)
	logTestEvent(t, st, id, "session_created")

	m := newArchiveManager(st)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ArchiveTerminal(id)
		}()
	}
	wg.Wait()

	// Wait for all in-flight to finish.
	m.inflight.Wait()

	// Exactly one archive must exist; verify it parses cleanly.
	manifest := waitForManifest(t, ws, id, 2*time.Second)
	if manifest["session"] == nil {
		t.Error("manifest.session missing")
	}

	// The seen map must have exactly one entry.
	m.seenMu.Lock()
	seenCount := len(m.seen)
	m.seenMu.Unlock()
	if seenCount != 1 {
		t.Errorf("expected seen map to have 1 entry, got %d", seenCount)
	}
}

func TestArchiveManager_DrainAllPartialForNonTerminal(t *testing.T) {
	wsRunning := t.TempDir()
	wsComplete := t.TempDir()
	st := openTestStore(t)

	runningID := createStoreSession(t, st, "running-sess", "running", wsRunning)
	completeID := createStoreSession(t, st, "complete-sess", "complete", wsComplete)
	logTestEvent(t, st, runningID, "session_created")

	m := newArchiveManager(st)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok := m.DrainAll(ctx)
	if !ok {
		t.Error("DrainAll returned false (timeout); expected true")
	}

	// Running session should have a partial=true archive.
	runningManifest := waitForManifest(t, wsRunning, runningID, 2*time.Second)
	if runningManifest["partial"] != true {
		t.Errorf("running session archive: expected partial=true, got %v", runningManifest["partial"])
	}

	// Complete session should NOT have been archived by DrainAll (drain skips terminal).
	completePath := filepath.Join(wsComplete, ".belayer", "archive", completeID, "manifest.json")
	if _, err := os.Stat(completePath); err == nil {
		t.Error("complete session was unexpectedly archived by DrainAll (drain must skip terminal sessions)")
	}
}

func TestArchiveManager_DrainAllStopsAcceptingNew(t *testing.T) {
	st := openTestStore(t)
	ws := t.TempDir()
	newID := createStoreSession(t, st, "late-sess", "complete", ws)

	m := newArchiveManager(st)

	// Start DrainAll with a short timeout — it will set stopping=true immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var drainWg sync.WaitGroup
	drainWg.Add(1)
	go func() {
		defer drainWg.Done()
		m.DrainAll(ctx)
	}()

	// Yield briefly so DrainAll can set stopping=true.
	time.Sleep(5 * time.Millisecond)

	// This call must be dropped because stopping is set.
	m.ArchiveTerminal(newID)

	drainWg.Wait()

	// No archive should have been produced for the late session.
	latePath := filepath.Join(ws, ".belayer", "archive", newID, "manifest.json")
	if _, err := os.Stat(latePath); err == nil {
		t.Error("archive produced for session after DrainAll started; expected it to be dropped")
	}
}

func TestArchiveManager_NoWorkspaceSkipped(t *testing.T) {
	st := openTestStore(t)

	// Session without a workspace dir.
	id := createStoreSession(t, st, "no-ws", "complete", "")

	m := newArchiveManager(st)
	m.ArchiveTerminal(id)

	// Wait for the inflight goroutine to finish — it must not panic.
	m.inflight.Wait()
	// No way to assert a path since there's no workspace; just verify no crash.
}

func TestArchiveManager_DrainAllReturnsFalseOnTimeout(t *testing.T) {
	st := openTestStore(t)
	m := newArchiveManager(st)

	// Inject a goroutine that blocks indefinitely via the test hook.
	release := m.addForTest()
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ok := m.DrainAll(ctx)
	if ok {
		t.Error("expected DrainAll to return false on timeout, got true")
	}
}
