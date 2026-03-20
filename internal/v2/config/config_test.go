package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initGitRepo(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
	cmd := exec.Command("git", "init", path)
	require.NoError(t, cmd.Run())
}

func TestConfig_DefaultHasInitializedMaps(t *testing.T) {
	cfg := DefaultConfig()
	assert.NotNil(t, cfg.Repos)
	assert.NotNil(t, cfg.Crags)
	assert.Empty(t, cfg.Repos)
	assert.Empty(t, cfg.Crags)
}

func TestConfig_AddRepo_ValidPath(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "my-repo")
	initGitRepo(t, repoPath)

	cfg := DefaultConfig()
	require.NoError(t, cfg.AddRepo("my-repo", repoPath))
	assert.Contains(t, cfg.Repos, "my-repo")

	resolved, err := cfg.ResolveRepoPath("my-repo")
	require.NoError(t, err)
	assert.Equal(t, repoPath, resolved)
}

func TestConfig_AddRepo_NonExistentPath(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.AddRepo("bad-repo", "/nonexistent/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestConfig_AddRepo_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	nonGitDir := filepath.Join(dir, "not-a-repo")
	require.NoError(t, os.MkdirAll(nonGitDir, 0o755))

	cfg := DefaultConfig()
	err := cfg.AddRepo("not-git", nonGitDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestConfig_RemoveRepo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repos["test"] = RepoEntry{Path: "/tmp/test"}

	require.NoError(t, cfg.RemoveRepo("test"))
	assert.NotContains(t, cfg.Repos, "test")
}

func TestConfig_RemoveRepo_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.RemoveRepo("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestConfig_ResolveRepoPath_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.ResolveRepoPath("missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestConfig_ResolveRepoPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repos["a"] = RepoEntry{Path: "/tmp/a"}
	cfg.Repos["b"] = RepoEntry{Path: "/tmp/b"}

	paths, err := cfg.ResolveRepoPaths([]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a", paths["a"])
	assert.Equal(t, "/tmp/b", paths["b"])
}

func TestConfig_ResolveRepoPaths_Missing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repos["a"] = RepoEntry{Path: "/tmp/a"}

	_, err := cfg.ResolveRepoPaths([]string{"a", "missing"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestConfig_ValidateRepoPaths_AllValid(t *testing.T) {
	dir := t.TempDir()
	repoA := filepath.Join(dir, "repo-a")
	repoB := filepath.Join(dir, "repo-b")
	initGitRepo(t, repoA)
	initGitRepo(t, repoB)

	cfg := DefaultConfig()
	cfg.Repos["a"] = RepoEntry{Path: repoA}
	cfg.Repos["b"] = RepoEntry{Path: repoB}

	assert.NoError(t, cfg.ValidateRepoPaths([]string{"a", "b"}))
}

func TestConfig_ValidateRepoPaths_OneMissing(t *testing.T) {
	dir := t.TempDir()
	repoA := filepath.Join(dir, "repo-a")
	initGitRepo(t, repoA)

	cfg := DefaultConfig()
	cfg.Repos["a"] = RepoEntry{Path: repoA}
	cfg.Repos["b"] = RepoEntry{Path: "/nonexistent/path"}

	err := cfg.ValidateRepoPaths([]string{"a", "b"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestConfig_AddCrag(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	require.NoError(t, cfg.AddCrag("test", dir))
	assert.Contains(t, cfg.Crags, "test")

	resolved, err := cfg.ResolveCragPath("test")
	require.NoError(t, err)
	assert.Equal(t, dir, resolved)
}

func TestConfig_ResolveCragPath_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.ResolveCragPath("missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestDetectRepoPath_Sibling(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "my-repo")
	initGitRepo(t, repoDir)

	// Set CWD to a sibling directory.
	siblingDir := filepath.Join(dir, "other-project")
	require.NoError(t, os.MkdirAll(siblingDir, 0o755))
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(siblingDir)

	path, err := DetectRepoPath("my-repo")
	require.NoError(t, err)
	// Resolve symlinks for macOS /var → /private/var comparison.
	expectedReal, _ := filepath.EvalSymlinks(repoDir)
	actualReal, _ := filepath.EvalSymlinks(path)
	assert.Equal(t, expectedReal, actualReal)
}

func TestDetectRepoPath_NotFound(t *testing.T) {
	_, err := DetectRepoPath("definitely-not-a-real-repo-name-xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not auto-detect")
}
