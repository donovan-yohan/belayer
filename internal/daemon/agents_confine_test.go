package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// envContains returns true when want appears in roots.
func envContains(roots []string, want string) bool {
	for _, r := range roots {
		if r == want {
			return true
		}
	}
	return false
}

// setupWorkspace creates a temporary workspace directory with some top-level
// entries and a .belayer/ tree so computeWriteRoots can enumerate children.
func setupWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	// Create top-level dirs that supervisor should be able to write to.
	for _, d := range []string{"src", "docs", "tests"} {
		if err := os.Mkdir(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Create .belayer/ runtime structure.
	for _, sub := range []string{".belayer/climbs", ".belayer/artifacts", ".belayer/worktrees"} {
		if err := os.MkdirAll(filepath.Join(ws, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return ws
}

func TestComputeWriteRoots_Supervisor(t *testing.T) {
	ws := setupWorkspace(t)
	runDir := filepath.Join(ws, ".belayer", "climbs", "sess1", "supervisor")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	req := agentSpawnRequest{
		Name:     "supervisor",
		Identity: "supervisor",
		Role:     "supervisor",
	}

	roots := computeWriteRoots(req, ws, "", runDir, nil)

	// .belayer/ root must NOT appear.
	if envContains(roots, filepath.Join(ws, ".belayer")) {
		t.Error("supervisor roots must not include .belayer/ root")
	}

	// The three allowed .belayer subdirs must appear.
	for _, sub := range []string{"climbs", "artifacts", "worktrees"} {
		want := filepath.Join(ws, ".belayer", sub)
		if !envContains(roots, want) {
			t.Errorf("supervisor roots must include .belayer/%s; got %v", sub, roots)
		}
	}

	// Top-level workspace dirs (excluding .belayer/) must appear.
	for _, dir := range []string{"src", "docs", "tests"} {
		want := filepath.Join(ws, dir)
		if !envContains(roots, want) {
			t.Errorf("supervisor roots must include workspace child %q; got %v", dir, roots)
		}
	}

	// /tmp must be present.
	if !envContains(roots, "/tmp") {
		t.Error("roots must always include /tmp")
	}
}

func TestComputeWriteRoots_PM(t *testing.T) {
	ws := setupWorkspace(t)
	runDir := filepath.Join(ws, ".belayer", "climbs", "sess1", "pm")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	req := agentSpawnRequest{
		Name:     "pm",
		Identity: "pm",
		Role:     "pm",
	}

	roots := computeWriteRoots(req, ws, "", runDir, nil)

	// PM must only have runDir + /tmp.
	if !envContains(roots, runDir) {
		t.Errorf("pm roots must include runDir; got %v", roots)
	}
	if !envContains(roots, "/tmp") {
		t.Error("roots must always include /tmp")
	}
	// Must NOT include workspace or workspace sub-dirs.
	for _, r := range roots {
		if r == ws || (len(r) > len(ws) && r[:len(ws)] == ws && r[len(ws)] == '/' && r != runDir && r != "/tmp") {
			t.Errorf("pm roots must not include %q (not runDir or /tmp); got %v", r, roots)
		}
	}
}

func TestComputeWriteRoots_BranchedSpecialist(t *testing.T) {
	ws := setupWorkspace(t)
	// Simulate a linked worktree: create a worktree dir with a .git file.
	wt := t.TempDir()
	gitdir := "/repo/.git/worktrees/implementer-1"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	runDir := filepath.Join(ws, ".belayer", "climbs", "sess1", "implementer-1")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	req := agentSpawnRequest{
		Name:     "implementer-1",
		Identity: "implementer",
		Role:     "implementer",
		Branch:   "feat/thing",
	}
	sharedPaths := []string{"/workspace/.cache"}

	roots := computeWriteRoots(req, ws, wt, runDir, sharedPaths)

	// Must contain: worktree, runDir, gitcommondir, shared path, /tmp.
	for _, want := range []string{wt, runDir, gitdir, "/workspace/.cache", "/tmp"} {
		if !envContains(roots, want) {
			t.Errorf("branched specialist roots must include %q; got %v", want, roots)
		}
	}
}

func TestComputeWriteRoots_UnbranchedSpecialist(t *testing.T) {
	ws := setupWorkspace(t)
	runDir := filepath.Join(ws, ".belayer", "climbs", "sess1", "implementer")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	req := agentSpawnRequest{
		Name:     "implementer",
		Identity: "implementer",
		Role:     "implementer",
	}

	roots := computeWriteRoots(req, ws, "", runDir, nil)

	// Must contain: runDir + /tmp only.
	if !envContains(roots, runDir) {
		t.Errorf("unbranched specialist roots must include runDir; got %v", roots)
	}
	if !envContains(roots, "/tmp") {
		t.Error("roots must always include /tmp")
	}
	// Sorted for stable comparison: expect exactly [runDir, /tmp].
	sorted := make([]string, len(roots))
	copy(sorted, roots)
	sort.Strings(sorted)
	want := []string{"/tmp", runDir}
	sort.Strings(want)
	if len(sorted) != len(want) {
		t.Errorf("unbranched specialist roots = %v, want %v", sorted, want)
	}
}

func TestComputeWriteRoots_SharedPathsSlice(t *testing.T) {
	ws := setupWorkspace(t)
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /repo/.git/worktrees/a\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	runDir := filepath.Join(ws, ".belayer", "climbs", "s", "a")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	req := agentSpawnRequest{Name: "a", Identity: "implementer", Role: "implementer", Branch: "feat/a"}
	shared := []string{"/cache/npm", "/cache/pip"}

	roots := computeWriteRoots(req, ws, wt, runDir, shared)

	for _, sp := range shared {
		if !envContains(roots, sp) {
			t.Errorf("roots must include shared path %q; got %v", sp, roots)
		}
	}
}
