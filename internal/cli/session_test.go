package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanupWorktrees_RemovesWorktreeDirectories verifies that cleanupWorktrees
// removes worktree subdirectories inside <baseDir>/worktrees/.
func TestCleanupWorktrees_RemovesWorktreeDirectories(t *testing.T) {
	// Create a temporary base directory simulating a session dir.
	baseDir := t.TempDir()
	worktreeDir := filepath.Join(baseDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0o700); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}

	// Create two fake worktree subdirectories (not real git worktrees, so the
	// `git worktree remove` call inside cleanupWorktrees will silently fail and
	// that is fine — the function does not require git to succeed).
	wt1 := filepath.Join(worktreeDir, "repo-a")
	wt2 := filepath.Join(worktreeDir, "repo-b")
	for _, d := range []string{wt1, wt2} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		// Add a file so the directory is non-empty.
		if err := os.WriteFile(filepath.Join(d, "dummy.txt"), []byte("test"), 0o600); err != nil {
			t.Fatalf("write dummy: %v", err)
		}
	}

	// cleanupWorktrees calls `git -C <path> worktree remove --force <path>` then
	// unconditionally calls os.RemoveAll on each subdir. For non-git directories the
	// git command fails silently, but the directory is removed from disk regardless.
	cleanupWorktrees(baseDir)

	// Both worktree subdirs should have been removed unconditionally by os.RemoveAll.
	for _, wt := range []string{wt1, wt2} {
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("expected worktree dir %s to be removed after cleanupWorktrees, stat err: %v", wt, err)
		}
	}
}

// TestCleanupWorktrees_MissingWorktreeDirIsNoop verifies that cleanupWorktrees
// is a no-op (does not panic or error) when the worktrees sub-directory does
// not exist.
func TestCleanupWorktrees_MissingWorktreeDirIsNoop(t *testing.T) {
	baseDir := t.TempDir()
	// Do NOT create the worktrees subdirectory.
	cleanupWorktrees(baseDir) // must not panic
}

// TestCleanupWorktrees_RealGitWorktree creates a real git repo and worktree,
// then verifies that cleanupWorktrees removes the worktree directory.
func TestCleanupWorktrees_RealGitWorktree(t *testing.T) {
	// Skip if git is not available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a bare-minimum git repo.
	repoDir := t.TempDir()
	mustGit(t, repoDir, "init")
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	// Need at least one commit for worktree add to work.
	readmeFile := filepath.Join(repoDir, "README")
	if err := os.WriteFile(readmeFile, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "init")

	// Create a session-like directory structure.
	baseDir := t.TempDir()
	worktreeDir := filepath.Join(baseDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0o700); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	wtPath := filepath.Join(worktreeDir, "repo-a")

	// Add a git worktree.
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath)

	// Verify the worktree exists before cleanup.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}

	// Run cleanup.
	cleanupWorktrees(baseDir)

	// The worktree directory should have been removed.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir to be removed, got err: %v", err)
	}
}

// TestSessionCleanCmd_SandboxCleanup tests cleanupSandboxDirs using a temporary
// directory that simulates ~/.belayer/sandboxes/.
func TestSessionCleanCmd_SandboxCleanup(t *testing.T) {
	fakeHome := t.TempDir()
	sandboxesDir := filepath.Join(fakeHome, ".belayer", "sandboxes")
	if err := os.MkdirAll(sandboxesDir, 0o700); err != nil {
		t.Fatalf("mkdir sandboxes: %v", err)
	}

	stoppedID1 := "session-dead-0001"
	stoppedID2 := "session-dead-0002"
	for _, id := range []string{stoppedID1, stoppedID2} {
		dir := filepath.Join(sandboxesDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir sandbox %s: %v", id, err)
		}
		// Use info.txt (not docker-compose.yml) so no docker call is attempted.
		if err := os.WriteFile(filepath.Join(dir, "info.txt"), []byte("test"), 0o600); err != nil {
			t.Fatalf("write info: %v", err)
		}
	}

	var out, errOut bytes.Buffer
	cleaned := cleanupSandboxDirs(sandboxesDir, map[string]bool{}, &out, &errOut)

	if cleaned != 2 {
		t.Errorf("expected 2 cleaned sandboxes, got %d; output: %s", cleaned, out.String())
	}
	for _, id := range []string{stoppedID1, stoppedID2} {
		dir := filepath.Join(sandboxesDir, id)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("sandbox dir %s should be removed, stat err: %v; output: %s", dir, err, out.String())
		}
		sandboxDir := filepath.Join(sandboxesDir, id)
		if !strings.Contains(out.String(), "Removed sandbox: "+sandboxDir) {
			t.Errorf("expected output to mention removed sandbox %s; output: %s", sandboxDir, out.String())
		}
	}
}

// TestSessionCleanCmd_ActiveSessionsPreserved verifies that sandbox dirs for
// active (running) sessions are NOT removed by cleanupSandboxDirs.
func TestSessionCleanCmd_ActiveSessionsPreserved(t *testing.T) {
	fakeHome := t.TempDir()
	sandboxesDir := filepath.Join(fakeHome, ".belayer", "sandboxes")
	if err := os.MkdirAll(sandboxesDir, 0o700); err != nil {
		t.Fatalf("mkdir sandboxes: %v", err)
	}

	activeID := "session-alive-0001"
	deadID := "session-dead-0001"
	for _, id := range []string{activeID, deadID} {
		dir := filepath.Join(sandboxesDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
	}

	var out, errOut bytes.Buffer
	cleaned := cleanupSandboxDirs(sandboxesDir, map[string]bool{activeID: true}, &out, &errOut)

	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}
	if _, err := os.Stat(filepath.Join(sandboxesDir, activeID)); err != nil {
		t.Errorf("active session sandbox should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandboxesDir, deadID)); !os.IsNotExist(err) {
		t.Errorf("dead session sandbox should be removed")
	}
}

// TestRepoForWorktree_NonGitDir returns empty string for a non-git directory.
func TestRepoForWorktree_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	result := repoForWorktree(dir)
	if result != "" {
		t.Errorf("expected empty string for non-git dir, got %q", result)
	}
}

// TestRepoForWorktree_WithRealWorktree verifies that repoForWorktree returns
// the main repo root for a real git worktree.
func TestRepoForWorktree_WithRealWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	mustGit(t, repoDir, "init")
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	readmeFile := filepath.Join(repoDir, "README")
	if err := os.WriteFile(readmeFile, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "init")

	wtPath := t.TempDir()
	// Remove the temp dir so git worktree add can create it.
	os.RemoveAll(wtPath)
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath)
	defer exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", wtPath).Run() //nolint:errcheck

	result := repoForWorktree(wtPath)
	if result == "" {
		t.Fatal("expected non-empty repo dir for real worktree")
	}
	// The returned path should match repoDir (resolve symlinks for macOS /var -> /private/var).
	wantAbs, _ := filepath.EvalSymlinks(repoDir)
	gotAbs, _ := filepath.EvalSymlinks(result)
	if !strings.EqualFold(wantAbs, gotAbs) {
		t.Errorf("repoForWorktree returned %q, want %q", gotAbs, wantAbs)
	}
}

// mustGit is a test helper that runs a git command in dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}
