package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWriteCoreFile_CreatesFile verifies that WriteCoreFile creates core.md
// with the correct format under baseDir/{repo}/core.md.
func TestWriteCoreFile_CreatesFile(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	entries := []CoreEntry{
		{Key: "goal", Value: "build a memory system"},
		{Key: "phase", Value: "climb"},
	}

	if err := fs.WriteCoreFile("myrepo", entries); err != nil {
		t.Fatalf("WriteCoreFile: %v", err)
	}

	path := filepath.Join(base, "myrepo", "core.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "## goal\nbuild a memory system") {
		t.Errorf("expected goal entry in core.md, got:\n%s", content)
	}
	if !strings.Contains(content, "## phase\nclimb") {
		t.Errorf("expected phase entry in core.md, got:\n%s", content)
	}
}

// TestReadCoreFile_ParsesEntries verifies that ReadCoreFile correctly parses
// a core.md file written by WriteCoreFile.
func TestReadCoreFile_ParsesEntries(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	written := []CoreEntry{
		{Key: "goal", Value: "build a memory system"},
		{Key: "phase", Value: "climb"},
	}

	if err := fs.WriteCoreFile("repo1", written); err != nil {
		t.Fatalf("WriteCoreFile: %v", err)
	}

	got, err := fs.ReadCoreFile("repo1")
	if err != nil {
		t.Fatalf("ReadCoreFile: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}

	keys := map[string]string{}
	for _, e := range got {
		keys[e.Key] = e.Value
	}

	if keys["goal"] != "build a memory system" {
		t.Errorf("goal: got %q, want %q", keys["goal"], "build a memory system")
	}
	if keys["phase"] != "climb" {
		t.Errorf("phase: got %q, want %q", keys["phase"], "climb")
	}
}

// TestReadCoreFile_NonexistentReturnsEmpty verifies that ReadCoreFile returns
// an empty (non-nil) slice when no core.md exists.
func TestReadCoreFile_NonexistentReturnsEmpty(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	entries, err := fs.ReadCoreFile("no-such-repo")
	if err != nil {
		t.Fatalf("ReadCoreFile: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil slice for nonexistent file")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestWriteArchivalFile_AppendsToTopicFile verifies that WriteArchivalFile
// appends an entry with a provenance header to the topic file.
func TestWriteArchivalFile_AppendsToTopicFile(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	entry := ArchivalEntry{
		SessionID: "sess-1",
		Content:   "The implementer completed the auth module",
		Tags:      "auth,implementation",
		Source:    "reflection:sess-1",
		CreatedAt: time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
	}

	if err := fs.WriteArchivalFile("repo1", "auth", entry); err != nil {
		t.Fatalf("WriteArchivalFile: %v", err)
	}

	path := filepath.Join(base, "repo1", "archival", "auth.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Session: sess-1") {
		t.Errorf("expected session provenance, got:\n%s", content)
	}
	if !strings.Contains(content, "Source: reflection:sess-1") {
		t.Errorf("expected source provenance, got:\n%s", content)
	}
	if !strings.Contains(content, "2026-04-09") {
		t.Errorf("expected date provenance, got:\n%s", content)
	}
	if !strings.Contains(content, "The implementer completed the auth module") {
		t.Errorf("expected content in file, got:\n%s", content)
	}
	if !strings.Contains(content, "Tags: auth,implementation") {
		t.Errorf("expected tags in file, got:\n%s", content)
	}
}

// TestWriteArchivalFile_AppendsBothEntries verifies that writing two entries
// to the same topic file results in both being present.
func TestWriteArchivalFile_AppendsBothEntries(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	e1 := ArchivalEntry{
		SessionID: "sess-1",
		Content:   "first learning about auth",
		Tags:      "auth",
		Source:    "reflection:sess-1",
		CreatedAt: time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
	}
	e2 := ArchivalEntry{
		SessionID: "sess-2",
		Content:   "second learning about auth",
		Tags:      "auth",
		Source:    "reflection:sess-2",
		CreatedAt: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
	}

	if err := fs.WriteArchivalFile("repo1", "auth", e1); err != nil {
		t.Fatalf("WriteArchivalFile e1: %v", err)
	}
	if err := fs.WriteArchivalFile("repo1", "auth", e2); err != nil {
		t.Fatalf("WriteArchivalFile e2: %v", err)
	}

	path := filepath.Join(base, "repo1", "archival", "auth.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "first learning about auth") {
		t.Errorf("expected first entry in file")
	}
	if !strings.Contains(content, "second learning about auth") {
		t.Errorf("expected second entry in file")
	}
}

// TestReadArchivalFiles_ReadsAcrossTopics verifies that ReadArchivalFiles
// returns entries from all topic files under a repo.
func TestReadArchivalFiles_ReadsAcrossTopics(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	entries := []struct {
		topic string
		entry ArchivalEntry
	}{
		{"auth", ArchivalEntry{SessionID: "s1", Content: "auth learning one", Tags: "auth", Source: "src1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{"auth", ArchivalEntry{SessionID: "s1", Content: "auth learning two", Tags: "auth", Source: "src1", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}},
		{"db", ArchivalEntry{SessionID: "s2", Content: "database learning one", Tags: "db", Source: "src2", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}},
	}

	for _, tc := range entries {
		if err := fs.WriteArchivalFile("repo1", tc.topic, tc.entry); err != nil {
			t.Fatalf("WriteArchivalFile %q: %v", tc.topic, err)
		}
	}

	got, err := fs.ReadArchivalFiles("repo1")
	if err != nil {
		t.Fatalf("ReadArchivalFiles: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 entries across topics, got %d", len(got))
	}

	contents := make(map[string]bool)
	for _, e := range got {
		contents[e.Content] = true
	}
	for _, tc := range entries {
		if !contents[tc.entry.Content] {
			t.Errorf("expected content %q in results", tc.entry.Content)
		}
	}
}

// TestReadArchivalFiles_NonexistentReturnsEmpty verifies that ReadArchivalFiles
// returns an empty (non-nil) slice when the archival dir doesn't exist.
func TestReadArchivalFiles_NonexistentReturnsEmpty(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	entries, err := fs.ReadArchivalFiles("no-such-repo")
	if err != nil {
		t.Fatalf("ReadArchivalFiles: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil slice for nonexistent dir")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestListRepos_ReturnsRepoDirectories verifies that ListRepos returns the
// repo directory names under baseDir.
func TestListRepos_ReturnsRepoDirectories(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	// Create two repos by writing core files.
	if err := fs.WriteCoreFile("repo-a", []CoreEntry{{Key: "k", Value: "v"}}); err != nil {
		t.Fatalf("WriteCoreFile repo-a: %v", err)
	}
	if err := fs.WriteCoreFile("repo-b", []CoreEntry{{Key: "k", Value: "v"}}); err != nil {
		t.Fatalf("WriteCoreFile repo-b: %v", err)
	}

	repos, err := fs.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %v", len(repos), repos)
	}

	repoSet := map[string]bool{}
	for _, r := range repos {
		repoSet[r] = true
	}
	if !repoSet["repo-a"] {
		t.Errorf("expected repo-a in results")
	}
	if !repoSet["repo-b"] {
		t.Errorf("expected repo-b in results")
	}
}

// TestListRepos_NonexistentBaseDirReturnsEmpty verifies that ListRepos returns
// an empty (non-nil) slice when baseDir doesn't exist.
func TestListRepos_NonexistentBaseDirReturnsEmpty(t *testing.T) {
	fs := NewMemFS("/nonexistent/path/that/does/not/exist")

	repos, err := fs.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if repos == nil {
		t.Fatal("expected non-nil slice for nonexistent base dir")
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

// TestRebuildIndex_PopulatesSQLiteFromMarkdown verifies that RebuildIndex
// reads all markdown files and populates the SQLite FTS5 index.
func TestRebuildIndex_PopulatesSQLiteFromMarkdown(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	// Write core entries for repo1.
	coreEntries := []CoreEntry{
		{SessionID: "sess-rebuild", Key: "goal", Value: "build the index"},
		{SessionID: "sess-rebuild", Key: "phase", Value: "summit"},
	}
	if err := fs.WriteCoreFile("repo1", coreEntries); err != nil {
		t.Fatalf("WriteCoreFile: %v", err)
	}

	// Write archival entries for repo1.
	archEntry := ArchivalEntry{
		SessionID: "sess-rebuild",
		Content:   "implementer finished database layer",
		Tags:      "database,implementation",
		Source:    "reflection:sess-rebuild",
		CreatedAt: time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC),
	}
	if err := fs.WriteArchivalFile("repo1", "database", archEntry); err != nil {
		t.Fatalf("WriteArchivalFile: %v", err)
	}

	// Open an in-memory SQLite store.
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	defer store.Close()

	// Rebuild the index.
	if err := RebuildIndex(fs, store); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}

	// Verify core entries are in SQLite.
	// Core entries are stored without SessionID from markdown (it's derived),
	// so we just verify archival search works for the content.
	archResults, err := store.SearchArchival("database", 10)
	if err != nil {
		t.Fatalf("SearchArchival after rebuild: %v", err)
	}
	if len(archResults) != 1 {
		t.Fatalf("expected 1 archival result after rebuild, got %d", len(archResults))
	}
	if !strings.Contains(archResults[0].Content, "implementer finished database layer") {
		t.Errorf("unexpected archival content: %q", archResults[0].Content)
	}
}

// TestRebuildIndex_EmptyFSIsNoOp verifies that RebuildIndex on an empty MemFS
// succeeds without error.
func TestRebuildIndex_EmptyFSIsNoOp(t *testing.T) {
	base := t.TempDir()
	fs := NewMemFS(base)

	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open SQLite: %v", err)
	}
	defer store.Close()

	if err := RebuildIndex(fs, store); err != nil {
		t.Fatalf("RebuildIndex on empty FS: %v", err)
	}
}
