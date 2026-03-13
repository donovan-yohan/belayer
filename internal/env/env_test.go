package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestGitRepo initialises a local git repo with an initial commit and returns its path.
func createTestGitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(dir, 0755))
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "config", "user.email", "test@test.com"},
		{"-C", dir, "config", "user.name", "Test"},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return dir
}

// setupTestCrag creates an isolated crag with one repo and returns (store, cragDir).
// It inserts a problem with id=envID so the environments FK is satisfied.
func setupTestCrag(t *testing.T, envID string) (*store.Store, string) {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	repoDir := createTestGitRepo(t, "test-repo")

	cragDir, err := crag.Create("test-crag", []string{repoDir})
	require.NoError(t, err)

	database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	s := store.New(database.Conn())

	// Insert a problem so the environments FK (problem_id → problems.id) is satisfied.
	err = s.InsertProblem(&model.Problem{
		ID:         envID,
		CragID:     "test-crag",
		Spec:       "test spec",
		ClimbsJSON: "{}",
		Status:     model.ProblemStatusPending,
	}, nil)
	require.NoError(t, err)

	return s, cragDir
}

func TestCreateAndStatus(t *testing.T) {
	envID := "env-test-1"
	s, cragDir := setupTestCrag(t, envID)

	resp, err := Create(s, cragDir, envID, "")
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, envID, resp.Name)

	status, err := Status(s, cragDir, envID)
	require.NoError(t, err)
	assert.Equal(t, "ok", status.Status)
	assert.Equal(t, envID, status.Name)
	assert.Empty(t, status.Worktrees)
}

func TestAddWorktreeAndStatus(t *testing.T) {
	envID := "env-test-2"
	s, cragDir := setupTestCrag(t, envID)

	_, err := Create(s, cragDir, envID, "")
	require.NoError(t, err)

	wtResp, err := AddWorktree(s, cragDir, envID, "test-repo", "my-branch", "")
	require.NoError(t, err)
	assert.Equal(t, "ok", wtResp.Status)
	assert.Equal(t, "test-repo", wtResp.Repo)
	assert.Equal(t, filepath.Join(cragDir, "tasks", envID, "test-repo"), wtResp.Path)
	assert.DirExists(t, wtResp.Path)

	status, err := Status(s, cragDir, envID)
	require.NoError(t, err)
	require.Len(t, status.Worktrees, 1)
	assert.Equal(t, "test-repo", status.Worktrees[0].Repo)
	assert.Equal(t, wtResp.Path, status.Worktrees[0].Path)
}

func TestRemoveWorktree(t *testing.T) {
	envID := "env-test-3"
	s, cragDir := setupTestCrag(t, envID)

	_, err := Create(s, cragDir, envID, "")
	require.NoError(t, err)

	wtResp, err := AddWorktree(s, cragDir, envID, "test-repo", "my-branch", "")
	require.NoError(t, err)

	err = RemoveWorktree(cragDir, envID, "test-repo", "my-branch")
	require.NoError(t, err)
	assert.NoDirExists(t, wtResp.Path)

	status, err := Status(s, cragDir, envID)
	require.NoError(t, err)
	assert.Empty(t, status.Worktrees)
}

func TestDestroyLifecycle(t *testing.T) {
	envID := "env-test-4"
	s, cragDir := setupTestCrag(t, envID)

	_, err := Create(s, cragDir, envID, "")
	require.NoError(t, err)

	wtResp, err := AddWorktree(s, cragDir, envID, "test-repo", "my-branch", "")
	require.NoError(t, err)
	assert.DirExists(t, wtResp.Path)

	err = Destroy(s, cragDir, envID)
	require.NoError(t, err)
	assert.NoDirExists(t, wtResp.Path)

	// Status should fail after destroy since the record is gone.
	_, err = Status(s, cragDir, envID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestList(t *testing.T) {
	envID := "env-test-5"
	s, cragDir := setupTestCrag(t, envID)

	resp, err := List(s)
	require.NoError(t, err)
	assert.Empty(t, resp.Environments)

	_, err = Create(s, cragDir, envID, "")
	require.NoError(t, err)

	resp, err = List(s)
	require.NoError(t, err)
	require.Len(t, resp.Environments, 1)
	assert.Equal(t, envID, resp.Environments[0].Name)
}

func TestReset(t *testing.T) {
	resp, err := Reset("env-test-6", "snap-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "snap-1", resp.Snapshot)
	assert.Equal(t, int64(0), resp.DurationMs)
}

func TestLogs(t *testing.T) {
	resp, err := Logs("env-test-7", "web")
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, "web", resp.Service)
	assert.Empty(t, resp.Lines)
}
