package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveGitCommonDir_WorktreeFile verifies that a worktree .git file is
// parsed and the gitdir path is returned.
func TestResolveGitCommonDir_WorktreeFile(t *testing.T) {
	tmp := t.TempDir()
	gitdir := "/repo/.git/worktrees/agent-1"
	gitFile := filepath.Join(tmp, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got := resolveGitCommonDir(tmp)
	if got != gitdir {
		t.Errorf("resolveGitCommonDir = %q, want %q", got, gitdir)
	}
}

// TestResolveGitCommonDir_RegularDir verifies that a .git directory (main
// repo) returns empty string.
func TestResolveGitCommonDir_RegularDir(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	got := resolveGitCommonDir(tmp)
	if got != "" {
		t.Errorf("resolveGitCommonDir = %q, want empty for regular .git dir", got)
	}
}

// TestResolveGitCommonDir_MissingFile verifies that a missing .git returns empty.
func TestResolveGitCommonDir_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	// No .git at all.
	got := resolveGitCommonDir(tmp)
	if got != "" {
		t.Errorf("resolveGitCommonDir = %q, want empty for missing .git", got)
	}
}

// TestResolveGitCommonDir_EmptyPath verifies that an empty worktree path
// returns empty.
func TestResolveGitCommonDir_EmptyPath(t *testing.T) {
	got := resolveGitCommonDir("")
	if got != "" {
		t.Errorf("resolveGitCommonDir(\"\") = %q, want empty", got)
	}
}

// TestResolveGitCommonDir_RelativeGitdir verifies that a relative gitdir path
// in the .git file is resolved relative to the worktree.
func TestResolveGitCommonDir_RelativeGitdir(t *testing.T) {
	tmp := t.TempDir()
	// Simulate a relative gitdir path as written by some git versions.
	relGitdir := "../.git/worktrees/agent-rel"
	gitFile := filepath.Join(tmp, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+relGitdir+"\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	want := filepath.Join(tmp, relGitdir)
	got := resolveGitCommonDir(tmp)
	if got != want {
		t.Errorf("resolveGitCommonDir = %q, want %q", got, want)
	}
}

// TestResolveGitCommonDir_UnexpectedFormat verifies that a .git file without
// the "gitdir: " prefix returns empty.
func TestResolveGitCommonDir_UnexpectedFormat(t *testing.T) {
	tmp := t.TempDir()
	gitFile := filepath.Join(tmp, ".git")
	if err := os.WriteFile(gitFile, []byte("not a gitdir line\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got := resolveGitCommonDir(tmp)
	if got != "" {
		t.Errorf("resolveGitCommonDir = %q, want empty for unexpected format", got)
	}
}
