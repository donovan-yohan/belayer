package memory

import (
	"testing"
)

func openMemory(t *testing.T) *SQLiteMemory {
	t.Helper()
	m, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

// TestWriteCore_ReadCore verifies a round-trip write and read of a core entry.
func TestWriteCore_ReadCore(t *testing.T) {
	m := openMemory(t)

	if err := m.WriteCore("session-1", "goal", "build a memory system"); err != nil {
		t.Fatalf("WriteCore: %v", err)
	}

	entries, err := m.ReadCore("session-1")
	if err != nil {
		t.Fatalf("ReadCore: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SessionID != "session-1" {
		t.Errorf("SessionID: got %q, want %q", entries[0].SessionID, "session-1")
	}
	if entries[0].Key != "goal" {
		t.Errorf("Key: got %q, want %q", entries[0].Key, "goal")
	}
	if entries[0].Value != "build a memory system" {
		t.Errorf("Value: got %q, want %q", entries[0].Value, "build a memory system")
	}
	if entries[0].UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

// TestWriteCore_Upsert verifies that writing the same key twice updates the value.
func TestWriteCore_Upsert(t *testing.T) {
	m := openMemory(t)

	if err := m.WriteCore("session-1", "status", "initial"); err != nil {
		t.Fatalf("WriteCore first: %v", err)
	}
	if err := m.WriteCore("session-1", "status", "updated"); err != nil {
		t.Fatalf("WriteCore second: %v", err)
	}

	entries, err := m.ReadCore("session-1")
	if err != nil {
		t.Fatalf("ReadCore: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", len(entries))
	}
	if entries[0].Value != "updated" {
		t.Errorf("Value after upsert: got %q, want %q", entries[0].Value, "updated")
	}
}

// TestReadCore_NonexistentSession verifies that ReadCore returns an empty slice
// for a session with no entries.
func TestReadCore_NonexistentSession(t *testing.T) {
	m := openMemory(t)

	entries, err := m.ReadCore("no-such-session")
	if err != nil {
		t.Fatalf("ReadCore: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil slice for nonexistent session")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestWriteArchival_SearchArchival verifies a round-trip write and search of
// an archival entry.
func TestWriteArchival_SearchArchival(t *testing.T) {
	m := openMemory(t)

	if err := m.WriteArchival("session-1", "The implementer completed the auth module", "auth,implementation", "docs/LEARNINGS.md"); err != nil {
		t.Fatalf("WriteArchival: %v", err)
	}

	results, err := m.SearchArchival("implementer", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "session-1" {
		t.Errorf("SessionID: got %q, want %q", results[0].SessionID, "session-1")
	}
	if results[0].Content != "The implementer completed the auth module" {
		t.Errorf("Content: got %q", results[0].Content)
	}
	if results[0].CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// TestSearchArchival_MatchesContent verifies FTS5 matches on the content field.
func TestSearchArchival_MatchesContent(t *testing.T) {
	m := openMemory(t)

	m.WriteArchival("s1", "reviewer found a critical security flaw", "review", "")
	m.WriteArchival("s1", "pilot planned the next iteration", "planning", "")
	m.WriteArchival("s1", "implementer wrote the database layer", "implementation", "")

	results, err := m.SearchArchival("security", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "reviewer found a critical security flaw" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
}

// TestSearchArchival_MatchesTags verifies FTS5 matches on the tags field.
func TestSearchArchival_MatchesTags(t *testing.T) {
	m := openMemory(t)

	m.WriteArchival("s1", "work item alpha", "planning,epic", "")
	m.WriteArchival("s1", "work item beta", "implementation,sprint", "")
	m.WriteArchival("s1", "work item gamma", "review,epic", "")

	results, err := m.SearchArchival("epic", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (both with 'epic' tag), got %d", len(results))
	}
}

// TestSearchArchival_LimitRespected verifies that SearchArchival returns at
// most limit results.
func TestSearchArchival_LimitRespected(t *testing.T) {
	m := openMemory(t)

	for i := 0; i < 5; i++ {
		m.WriteArchival("s1", "common keyword entry", "tag", "")
	}

	results, err := m.SearchArchival("common", 3)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (limit), got %d", len(results))
	}
}

// TestSearchArchival_NoMatches verifies that SearchArchival returns an empty
// (non-nil) slice when nothing matches.
func TestSearchArchival_NoMatches(t *testing.T) {
	m := openMemory(t)

	m.WriteArchival("s1", "some content here", "tag", "")

	results, err := m.SearchArchival("xyzzy_nonexistent_term", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice for no matches")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestRecall_CombinesCoreAndArchival verifies that Recall returns both core
// entries for the session and archival search results.
func TestRecall_CombinesCoreAndArchival(t *testing.T) {
	m := openMemory(t)

	m.WriteCore("session-recall", "phase", "climb")
	m.WriteCore("session-recall", "agent", "implementer")
	m.WriteArchival("session-recall", "the implementer wrote excellent tests", "quality", "docs/LEARNINGS.md")
	m.WriteArchival("session-recall", "the pilot planned the sprint carefully", "planning", "docs/PLANS.md")

	result, err := m.Recall("session-recall", "implementer")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}

	if len(result.Core) != 2 {
		t.Errorf("expected 2 core entries, got %d", len(result.Core))
	}
	if len(result.Archival) != 1 {
		t.Errorf("expected 1 archival result for 'implementer', got %d", len(result.Archival))
	}
	if result.Archival[0].Content != "the implementer wrote excellent tests" {
		t.Errorf("unexpected archival content: %q", result.Archival[0].Content)
	}
}

// TestArchivalEntry_SourcePreserved verifies that the source file path is
// stored and returned correctly — the "markdown is authoritative" contract.
func TestArchivalEntry_SourcePreserved(t *testing.T) {
	m := openMemory(t)

	sourcePath := "docs/LEARNINGS.md"
	if err := m.WriteArchival("s1", "learned something important", "learning", sourcePath); err != nil {
		t.Fatalf("WriteArchival: %v", err)
	}

	results, err := m.SearchArchival("learned", 10)
	if err != nil {
		t.Fatalf("SearchArchival: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Source != sourcePath {
		t.Errorf("Source: got %q, want %q", results[0].Source, sourcePath)
	}
}
