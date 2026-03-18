package repo

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepoNameFromURL(t *testing.T) {
	absoluteLocalPath := filepath.Join(t.TempDir(), "abs-repo")
	localBarePath := filepath.Join(t.TempDir(), "local-bare.git")
	relativeLocalPath := filepath.Join("repos", "relative-repo")

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
		{absoluteLocalPath, "abs-repo", false},
		{localBarePath, "local-bare", false},
		{relativeLocalPath, "relative-repo", false},
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

func TestValidateRepoSource(t *testing.T) {
	localRepo := filepath.Join(t.TempDir(), "local-repo")
	if err := os.MkdirAll(localRepo, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	tests := []struct {
		name            string
		repoSource      string
		allowLocalPaths bool
		wantErr         bool
	}{
		{name: "https url", repoSource: "https://github.com/org/my-repo.git"},
		{name: "ssh url", repoSource: "git@github.com:org/my-repo.git"},
		{name: "file url", repoSource: (&url.URL{Scheme: "file", Path: localRepo}).String()},
		{name: "local path with flag", repoSource: localRepo, allowLocalPaths: true},
		{name: "local path without flag", repoSource: localRepo, wantErr: true},
		{name: "missing local path", repoSource: filepath.Join(t.TempDir(), "missing-repo"), allowLocalPaths: true, wantErr: true},
		{name: "local file path", repoSource: createLocalFile(t), allowLocalPaths: true, wantErr: true},
		{name: "empty source", repoSource: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepoSource(tt.repoSource, tt.allowLocalPaths)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateRepoSource(%q, %t) succeeded, want error", tt.repoSource, tt.allowLocalPaths)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateRepoSource(%q, %t) error = %v", tt.repoSource, tt.allowLocalPaths, err)
			}
		})
	}
}

func TestValidateRepoSourceColonPath(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "local:repo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Skipf("filesystem does not allow colon in paths: %v", err)
	}

	if err := ValidateRepoSource(repoPath, true); err != nil {
		t.Fatalf("ValidateRepoSource(%q, true) error = %v", repoPath, err)
	}

	name, err := RepoNameFromURL(repoPath)
	if err != nil {
		t.Fatalf("RepoNameFromURL(%q) error = %v", repoPath, err)
	}
	if name != "local:repo" {
		t.Fatalf("RepoNameFromURL(%q) = %q, want %q", repoPath, name, "local:repo")
	}
}

func createLocalFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "not-a-repo.txt")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
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

func TestHasUnpushedCommits(t *testing.T) {
	bareDir := initBareRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	if err := WorktreeAdd(bareDir, worktreePath, "test-branch"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	// Fresh worktree — no new commits
	has, err := HasUnpushedCommits(worktreePath)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if has {
		t.Error("fresh worktree should have no unpushed commits")
	}
	// Make a commit
	newFile := filepath.Join(worktreePath, "new.txt")
	os.WriteFile(newFile, []byte("content"), 0644)
	for _, args := range [][]string{
		{"-C", worktreePath, "add", "new.txt"},
		{"-C", worktreePath, "commit", "-m", "test commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	// Now should have unpushed commits
	has, err = HasUnpushedCommits(worktreePath)
	if err != nil {
		t.Fatalf("HasUnpushedCommits after commit: %v", err)
	}
	if !has {
		t.Error("worktree with commits ahead should have unpushed commits")
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
