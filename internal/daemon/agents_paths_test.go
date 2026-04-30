package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentIdentityPathsWalksUpToProjectRoot(t *testing.T) {
	// Simulate the common case: workdir is nested under .belayer/climbs/<id>/workspace
	// (multi-repo provisioning) but the project-local agents/ override sits at the
	// project root, several levels up.
	projectRoot := t.TempDir()
	overrideDir := filepath.Join(projectRoot, ".belayer", "agents", "reviewer")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}
	overrideFile := filepath.Join(overrideDir, "system-prompt.md")
	if err := os.WriteFile(overrideFile, []byte("override"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	nestedWorkdir := filepath.Join(projectRoot, ".belayer", "climbs", "sess-1", "workspace")
	if err := os.MkdirAll(nestedWorkdir, 0o755); err != nil {
		t.Fatalf("mkdir nested workdir: %v", err)
	}

	belayerRoot := t.TempDir()
	shippedDir := filepath.Join(belayerRoot, "agents", "reviewer")
	if err := os.MkdirAll(shippedDir, 0o755); err != nil {
		t.Fatalf("mkdir shipped: %v", err)
	}
	shippedFile := filepath.Join(shippedDir, "system-prompt.md")
	if err := os.WriteFile(shippedFile, []byte("shipped"), 0o644); err != nil {
		t.Fatalf("write shipped: %v", err)
	}

	paths := agentIdentityPaths(nestedWorkdir, belayerRoot, "reviewer", "system-prompt.md")

	// The override at the project root must come before the shipped default,
	// even though the workdir is several directories below the project root.
	overrideIdx := -1
	shippedIdx := -1
	for i, p := range paths {
		if p == overrideFile {
			overrideIdx = i
		}
		if p == shippedFile {
			shippedIdx = i
		}
	}
	if overrideIdx == -1 {
		t.Fatalf("project-local override missing from candidates: %v", paths)
	}
	if shippedIdx == -1 {
		t.Fatalf("shipped default missing from candidates: %v", paths)
	}
	if overrideIdx >= shippedIdx {
		t.Fatalf("override (idx=%d) must come before shipped (idx=%d): %v", overrideIdx, shippedIdx, paths)
	}
}

func TestAgentIdentityPathsDeduplicates(t *testing.T) {
	// When workdir == belayerRoot the same path can appear from both the
	// walk-up branch and the shipped fallback. Verify no duplicates.
	root := t.TempDir()
	paths := agentIdentityPaths(root, root, "reviewer", "system-prompt.md")
	seen := make(map[string]int)
	for _, p := range paths {
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("path %q appears %d times in %v", p, n, paths)
		}
	}
}

func TestAgentIdentityKeyDefaultsToName(t *testing.T) {
	// Identity defaults to Name (preserves single-instance-per-identity convention).
	if got := (agentSpawnRequest{Name: "supervisor"}).identityKey(); got != "supervisor" {
		t.Fatalf("expected name fallback 'supervisor', got %q", got)
	}
	// Explicit Identity wins (multi-instance case: reviewer-1 / reviewer-2).
	if got := (agentSpawnRequest{Name: "reviewer-1", Identity: "reviewer"}).identityKey(); got != "reviewer" {
		t.Fatalf("expected explicit identity 'reviewer', got %q", got)
	}
}

func TestAgentIdentityPathsHandlesEmptyBases(t *testing.T) {
	if got := agentIdentityPaths("", "", "reviewer", "system-prompt.md"); len(got) != 0 {
		t.Fatalf("expected empty result for empty bases, got %v", got)
	}
	got := agentIdentityPaths("", "/opt/belayer", "reviewer", "system-prompt.md")
	if len(got) != 1 || !strings.HasSuffix(got[0], "/opt/belayer/agents/reviewer/system-prompt.md") {
		t.Fatalf("expected single shipped path, got %v", got)
	}
}
