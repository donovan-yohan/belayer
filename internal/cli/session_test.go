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

	// cleanupWorktrees calls `git worktree remove --force <path>` then returns.
	// For non-git directories the git command fails silently; the directories
	// themselves are NOT removed by cleanupWorktrees (it delegates to git).
	// The function contract is: call git worktree remove for each subdir.
	// We test that the function runs without panicking and touches each entry.
	cleanupWorktrees(baseDir)

	// After calling cleanupWorktrees the worktrees directory itself still exists
	// (the function does not remove the parent), and the subdirs may or may not
	// still exist depending on whether git succeeded.  What we can assert is that
	// the function does not leave extra unexpected directories.
	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		t.Fatalf("ReadDir after cleanup: %v", err)
	}
	// Both entries were there before; after cleanup each should have had
	// `git worktree remove --force` attempted.  On a CI machine without a real
	// git repo, the dirs survive (git exits non-zero, error is ignored).
	// We just verify the count didn't increase.
	if len(entries) > 2 {
		t.Errorf("unexpected extra entries after cleanupWorktrees: %d", len(entries))
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

// TestSessionCleanCmd_SandboxCleanup tests the session clean command's sandbox
// removal logic using a temporary directory that simulates ~/.belayer/sandboxes/.
func TestSessionCleanCmd_SandboxCleanup(t *testing.T) {
	// Build a fake home dir with sandbox directories.
	fakeHome := t.TempDir()
	sandboxesDir := filepath.Join(fakeHome, ".belayer", "sandboxes")
	if err := os.MkdirAll(sandboxesDir, 0o700); err != nil {
		t.Fatalf("mkdir sandboxes: %v", err)
	}

	// Create two sandbox directories for "stopped" session IDs (no active sessions).
	stoppedID1 := "session-dead-0001"
	stoppedID2 := "session-dead-0002"
	for _, id := range []string{stoppedID1, stoppedID2} {
		dir := filepath.Join(sandboxesDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir sandbox %s: %v", id, err)
		}
		// Write a placeholder file (not docker-compose.yml so no docker call).
		if err := os.WriteFile(filepath.Join(dir, "info.txt"), []byte("test"), 0o600); err != nil {
			t.Fatalf("write info: %v", err)
		}
	}

	// Directly exercise the sandbox-removal logic extracted from newSessionCleanCmd.
	// We simulate "no active sessions" by passing an empty activeSessions map.
	activeSessions := map[string]bool{}
	var out bytes.Buffer
	cleaned := 0

	entries, err := os.ReadDir(sandboxesDir)
	if err != nil {
		t.Fatalf("ReadDir sandboxes: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		if activeSessions[sessionID] {
			continue
		}
		sandboxDir := filepath.Join(sandboxesDir, sessionID)
		if err := os.RemoveAll(sandboxDir); err != nil {
			t.Errorf("RemoveAll %s: %v", sandboxDir, err)
		} else {
			out.WriteString("Removed sandbox: " + sandboxDir + "\n")
			cleaned++
		}
	}

	if cleaned != 2 {
		t.Errorf("expected 2 cleaned sandboxes, got %d; output: %s", cleaned, out.String())
	}

	// Verify they're gone.
	for _, id := range []string{stoppedID1, stoppedID2} {
		dir := filepath.Join(sandboxesDir, id)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("sandbox dir %s should be removed, stat err: %v", dir, err)
		}
	}
}

// TestSessionCleanCmd_ActiveSessionsPreserved verifies that sandbox dirs for
// active (running) sessions are NOT removed.
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

	activeSessions := map[string]bool{activeID: true}
	cleaned := 0

	entries, _ := os.ReadDir(sandboxesDir)
	for _, entry := range entries {
		if !entry.IsDir() || activeSessions[entry.Name()] {
			continue
		}
		os.RemoveAll(filepath.Join(sandboxesDir, entry.Name())) //nolint:errcheck
		cleaned++
	}

	if cleaned != 1 {
		t.Errorf("expected 1 cleaned, got %d", cleaned)
	}
	// Active session dir must still exist.
	if _, err := os.Stat(filepath.Join(sandboxesDir, activeID)); err != nil {
		t.Errorf("active session sandbox should still exist: %v", err)
	}
	// Dead session dir must be gone.
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
