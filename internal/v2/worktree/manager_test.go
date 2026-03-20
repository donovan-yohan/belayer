package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initBareRepo(t *testing.T, dir string) string {
	t.Helper()
	repoPath := filepath.Join(dir, "test-repo")
	cmd := exec.Command("git", "init", "--bare", repoPath)
	require.NoError(t, cmd.Run())
	return repoPath
}

func initRepoWithCommit(t *testing.T, dir, name string) string {
	t.Helper()
	repoPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(repoPath, 0o755))

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Create an initial commit so worktree branching works.
	readmePath := filepath.Join(repoPath, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Test\n"), 0o644))
	run("add", ".")
	run("commit", "-m", "initial")

	return repoPath
}

func TestCreateWorktree(t *testing.T) {
	dir := t.TempDir()
	repoPath := initRepoWithCommit(t, dir, "my-repo")
	wtPath := filepath.Join(dir, "worktrees", "my-repo")

	err := CreateWorktree(repoPath, wtPath, "belayer/test-run")
	require.NoError(t, err)

	// Worktree should exist.
	_, err = os.Stat(wtPath)
	assert.NoError(t, err)

	// Should be on the right branch.
	cmd := exec.Command("git", "-C", wtPath, "branch", "--show-current")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "belayer/test-run", string(out[:len(out)-1]))
}

func TestSetupRunWorktrees(t *testing.T) {
	dir := t.TempDir()
	repoA := initRepoWithCommit(t, dir, "repo-a")
	repoB := initRepoWithCommit(t, dir, "repo-b")

	mgr := NewManager(filepath.Join(dir, "worktrees"))
	repos := map[string]string{"repo-a": repoA, "repo-b": repoB}

	worktrees, err := mgr.SetupRunWorktrees("run-123", repos)
	require.NoError(t, err)
	assert.Len(t, worktrees, 2)
	assert.Contains(t, worktrees, "repo-a")
	assert.Contains(t, worktrees, "repo-b")

	// Both worktrees should exist.
	for _, wtPath := range worktrees {
		_, err := os.Stat(wtPath)
		assert.NoError(t, err)
	}
}

func TestHasUnpushedCommits_NoCommits(t *testing.T) {
	dir := t.TempDir()
	repoPath := initRepoWithCommit(t, dir, "my-repo")
	wtPath := filepath.Join(dir, "worktrees", "my-repo")
	require.NoError(t, CreateWorktree(repoPath, wtPath, "belayer/test"))

	// Fresh worktree with no new commits — but no upstream either.
	// Should detect the initial commit as unpushed.
	unpushed, err := HasUnpushedCommits(wtPath)
	require.NoError(t, err)
	assert.True(t, unpushed) // No remote → all commits are "unpushed"
}

func TestSafeCleanup_NoUnpushed(t *testing.T) {
	dir := t.TempDir()
	repoPath := initRepoWithCommit(t, dir, "my-repo")

	// Create a "remote" so push check works.
	remoteDir := filepath.Join(dir, "remote.git")
	exec.Command("git", "clone", "--bare", repoPath, remoteDir).Run()
	exec.Command("git", "-C", repoPath, "remote", "add", "origin", remoteDir).Run()
	exec.Command("git", "-C", repoPath, "push", "-u", "origin", "main").Run()

	wtPath := filepath.Join(dir, "worktrees", "my-repo")
	require.NoError(t, CreateWorktree(repoPath, wtPath, "belayer/test"))

	// Push the branch so it's not "unpushed".
	exec.Command("git", "-C", wtPath, "push", "origin", "belayer/test").Run()

	warning := SafeCleanup(wtPath, "test")
	assert.Empty(t, warning)

	// Worktree should be removed.
	_, err := os.Stat(wtPath)
	assert.True(t, os.IsNotExist(err))
}

func TestRemoveWorktree(t *testing.T) {
	dir := t.TempDir()
	repoPath := initRepoWithCommit(t, dir, "my-repo")
	wtPath := filepath.Join(dir, "worktrees", "my-repo")
	require.NoError(t, CreateWorktree(repoPath, wtPath, "belayer/test"))

	err := RemoveWorktree(wtPath)
	assert.NoError(t, err)
}
