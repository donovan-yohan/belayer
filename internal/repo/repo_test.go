package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
		wantErr  bool
	}{
		{"https://github.com/org/my-repo.git", "my-repo", false},
		{"https://github.com/org/my-repo", "my-repo", false},
		{"git@github.com:org/my-repo.git", "my-repo", false},
		{"git@github.com:org/my-repo", "my-repo", false},
		{"https://github.com/org/repo.git/", "repo", false},
		{"https://gitlab.com/group/subgroup/project.git", "project", false},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			name, err := RepoNameFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for URL %q", tt.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for URL %q: %v", tt.url, err)
			}
			if name != tt.expected {
				t.Errorf("RepoNameFromURL(%q) = %q, want %q", tt.url, name, tt.expected)
			}
		})
	}
}

// initBareRepo creates a bare git repo for testing and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()

	// Create a regular repo first with an initial commit
	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init", srcDir},
		{"-C", srcDir, "config", "user.email", "test@test.com"},
		{"-C", srcDir, "config", "user.name", "Test"},
		{"-C", srcDir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	// Clone as bare
	bareDir := filepath.Join(t.TempDir(), "test-repo.git")
	cmd := exec.Command("git", "clone", "--bare", srcDir, bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %s: %v", out, err)
	}

	return bareDir
}

func TestWorktreeAddAndRemove(t *testing.T) {
	bareDir := initBareRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")

	// Add worktree
	if err := WorktreeAdd(bareDir, worktreePath, "test-branch"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatal("worktree directory not created")
	}

	// List worktrees
	paths, err := WorktreeList(bareDir)
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	resolvedWT, _ := filepath.EvalSymlinks(worktreePath)
	found := false
	for _, p := range paths {
		resolvedP, _ := filepath.EvalSymlinks(p)
		if resolvedP == resolvedWT {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("worktree %q not found in list: %v", worktreePath, paths)
	}

	// Remove worktree
	if err := WorktreeRemove(bareDir, worktreePath); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}

	// Verify worktree directory removed
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("worktree directory not removed")
	}
}

func TestCloneBare(t *testing.T) {
	// Create a source repo
	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init", srcDir},
		{"-C", srcDir, "config", "user.email", "test@test.com"},
		{"-C", srcDir, "config", "user.name", "Test"},
		{"-C", srcDir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	// Clone as bare
	bareDir := filepath.Join(t.TempDir(), "cloned.git")
	if err := CloneBare(srcDir, bareDir); err != nil {
		t.Fatalf("CloneBare: %v", err)
	}

	// Verify it's a bare repo
	cmd := exec.Command("git", "-C", bareDir, "rev-parse", "--is-bare-repository")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %s: %v", out, err)
	}
	if got := string(out[:4]); got != "true" {
		t.Errorf("expected bare repository, got %q", got)
	}
}
