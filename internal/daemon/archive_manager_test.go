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

	m := newArchiveManager(st, "test-instance")
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

	m := newArchiveManager(st, "test-instance")

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

	// After completion, seen is cleared so the map stays bounded over long
	// daemon lifetimes. The dedupe proof is the single-archive outcome above;
	// if seen had leaked, the test would still pass the count check but we'd
	// have ambiguity about which mechanism deduped. Assert the post-completion
	// invariant: seen empty.
	m.seenMu.Lock()
	seenCount := len(m.seen)
	m.seenMu.Unlock()
	if seenCount != 0 {
		t.Errorf("expected seen map empty after archive completion, got %d entries", seenCount)
	}
}

func TestArchiveManager_DrainAllPartialForNonTerminal(t *testing.T) {
	wsRunning := t.TempDir()
	wsComplete := t.TempDir()
	st := openTestStore(t)

	runningID := createStoreSession(t, st, "running-sess", "running", wsRunning)
	completeID := createStoreSession(t, st, "complete-sess", "complete", wsComplete)
	logTestEvent(t, st, runningID, "session_created")

	m := newArchiveManager(st, "test-instance")

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

	m := newArchiveManager(st, "test-instance")

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

	m := newArchiveManager(st, "test-instance")
	m.ArchiveTerminal(id)

	// Wait for the inflight goroutine to finish — it must not panic.
	m.inflight.Wait()
	// No way to assert a path since there's no workspace; just verify no crash.
}

func TestArchiveManager_DrainAllReturnsFalseOnTimeout(t *testing.T) {
	st := openTestStore(t)
	m := newArchiveManager(st, "test-instance")

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

// TestArchiveManager_ConcurrentAddVsDrain stress-tests the WaitGroup
// Add/Wait invariant: ArchiveTerminal's stopping check + seen dedupe +
// inflight.Add must all live in one seenMu critical section so DrainAll
// (which flips stopping under the same mutex) can never race an Add after
// inflight.Wait has started. Run under `go test -race` to catch regressions.
func TestArchiveManager_ConcurrentAddVsDrain(t *testing.T) {
	for iter := 0; iter < 20; iter++ {
		ws := t.TempDir()
		st := openTestStore(t)
		m := newArchiveManager(st, "test-instance")

		ids := make([]string, 30)
		for i := range ids {
			ids[i] = createStoreSession(t, st,
				"race-"+string(rune('a'+(i%26))),
				"complete", ws)
		}

		var wg sync.WaitGroup
		wg.Add(len(ids) + 1)
		for _, id := range ids {
			go func(sessionID string) {
				defer wg.Done()
				m.ArchiveTerminal(sessionID)
			}(id)
		}
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			m.DrainAll(ctx)
		}()
		wg.Wait()
		// The assertion is absence-of-panic; `go test -race` catches the
		// Add/Wait race if the stopping-check + Add escape the mutex.
		st.Close()
	}
}

// TestArchiveTerminal_WorksAfterDedupeEviction verifies that a second
// terminal transition for the same session archives again after the first
// goroutine cleared the seen entry. This is the "status re-set" path cragd
// cares about when operators re-run a completed session's PATCH.
func TestArchiveTerminal_WorksAfterDedupeEviction(t *testing.T) {
	ws := t.TempDir()
	st := openTestStore(t)
	m := newArchiveManager(st, "test-instance")

	id := createStoreSession(t, st, "dedupe-evict", "complete", ws)
	logTestEvent(t, st, id, "session_created")

	m.ArchiveTerminal(id)
	m.inflight.Wait()
	_ = waitForManifest(t, ws, id, 2*time.Second)

	// seen is cleared — a second call must produce a new archive (overwriting
	// the first via atomic directory rename).
	logTestEvent(t, st, id, "session_re_completed")
	m.ArchiveTerminal(id)
	m.inflight.Wait()

	manifest := waitForManifest(t, ws, id, 2*time.Second)
	count, ok := manifest["event_count"].(float64)
	if !ok {
		t.Fatalf("event_count not numeric: %T", manifest["event_count"])
	}
	if count < 2 {
		t.Errorf("expected event_count >= 2 after re-archive, got %v", count)
	}
}

// TestArchiveManager_VerboseIncludesTranscripts verifies the end-to-end
// capture-at-level contract: a session created with LogLevel="verbose"
// whose transcript files live under <workspace>/.belayer/runs/<id>/transcripts/
// gets both manifest.session.log_level=="verbose" AND the transcript files
// copied into <archive>/transcripts/ with verbatim content.
func TestArchiveManager_VerboseIncludesTranscripts(t *testing.T) {
	ws := t.TempDir()
	st := openTestStore(t)

	id, err := st.CreateSession(store.Session{
		Name:         "verbose-sess",
		Status:       "running",
		WorkspaceDir: ws,
		LogLevel:     "verbose",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	logTestEvent(t, st, id, "session_created")

	// Stage a transcript file at the daemon-anchored path.
	transcriptsDir := filepath.Join(ws, ".belayer", "runs", id, "transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o700); err != nil {
		t.Fatalf("mkdir transcripts: %v", err)
	}
	transcriptContent := `{"ts":"2026-04-19T12:00:00Z","agent":"supervisor","kind":"reasoning","turn":1,"text":"thinking out loud"}` + "\n"
	transcriptPath := filepath.Join(transcriptsDir, "supervisor.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	if err := st.UpdateSessionStatus(id, "complete"); err != nil {
		t.Fatalf("UpdateSessionStatus: %v", err)
	}

	m := newArchiveManager(st, "test-instance")
	m.ArchiveTerminal(id)
	m.inflight.Wait()

	manifest := waitForManifest(t, ws, id, 2*time.Second)
	sess, ok := manifest["session"].(map[string]any)
	if !ok {
		t.Fatalf("manifest.session not an object")
	}
	if got := sess["log_level"]; got != "verbose" {
		t.Errorf("manifest.session.log_level: got %v, want %q", got, "verbose")
	}

	// Transcript must be copied verbatim into the archive.
	archivedTranscript := filepath.Join(ws, ".belayer", "archive", id, "transcripts", "supervisor.jsonl")
	got, err := os.ReadFile(archivedTranscript)
	if err != nil {
		t.Fatalf("archived transcript missing: %v", err)
	}
	if string(got) != transcriptContent {
		t.Errorf("archived transcript content mismatch:\ngot:  %q\nwant: %q", string(got), transcriptContent)
	}
}

// TestArchiveManager_StandardOmitsTranscripts verifies that a standard-level
// session produces an archive whose manifest omits session.log_level (the
// default) and does NOT materialize a transcripts/ directory.
func TestArchiveManager_StandardOmitsTranscripts(t *testing.T) {
	ws := t.TempDir()
	st := openTestStore(t)

	id, err := st.CreateSession(store.Session{
		Name:         "standard-sess",
		Status:       "complete",
		WorkspaceDir: ws,
		LogLevel:     "standard",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	logTestEvent(t, st, id, "session_created")

	m := newArchiveManager(st, "test-instance")
	m.ArchiveTerminal(id)
	m.inflight.Wait()

	manifest := waitForManifest(t, ws, id, 2*time.Second)
	sess, ok := manifest["session"].(map[string]any)
	if !ok {
		t.Fatalf("manifest.session not an object")
	}
	// "standard" must be serialized so readers can disambiguate from older
	// archives that predate the column. Only empty/unset log_level is omitted.
	if got := sess["log_level"]; got != "standard" {
		t.Errorf("manifest.session.log_level: got %v, want %q", got, "standard")
	}

	transcriptsDir := filepath.Join(ws, ".belayer", "archive", id, "transcripts")
	if _, err := os.Stat(transcriptsDir); err == nil {
		t.Errorf("transcripts/ should not exist for standard-level session, but does")
	}
}
