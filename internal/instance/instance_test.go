package instance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// createTestRepo creates a local git repo with an initial commit, returns its path.
func createTestRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "config", "user.email", "test@test.com"},
		{"-C", dir, "config", "user.name", "Test"},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	return dir
}

// setupTestHome overrides HOME to a temp directory so tests don't touch real ~/.belayer.
func setupTestHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	return tmpHome
}

func TestCreateAndLoad(t *testing.T) {
	home := setupTestHome(t)
	repoPath := createTestRepo(t, "my-repo")

	instanceDir, err := Create("test-inst", []string{repoPath})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify crag directory exists
	expectedDir := filepath.Join(home, ".belayer", "crags", "test-inst")
	if instanceDir != expectedDir {
		t.Errorf("crag dir = %q, want %q", instanceDir, expectedDir)
	}
	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		t.Fatal("crag directory not created")
	}

	// Verify crag config loads correctly
	cfg, loadedDir, err := Load("test-inst")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadedDir != instanceDir {
		t.Errorf("loaded dir = %q, want %q", loadedDir, instanceDir)
	}
	if cfg.Name != "test-inst" {
		t.Errorf("config name = %q, want %q", cfg.Name, "test-inst")
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].Name != "my-repo" {
		t.Errorf("repo name = %q, want %q", cfg.Repos[0].Name, "my-repo")
	}

	// Verify bare repo exists
	bareRepoPath := filepath.Join(instanceDir, cfg.Repos[0].BarePath)
	if _, err := os.Stat(bareRepoPath); os.IsNotExist(err) {
		t.Fatal("bare repo not created")
	}

	// Verify SQLite database
	dbPath := filepath.Join(instanceDir, "belayer.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database not created")
	}
}

func TestCreateDuplicate(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "repo")

	if _, err := Create("dup-test", []string{repoPath}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	if _, err := Create("dup-test", []string{repoPath}); err == nil {
		t.Fatal("expected error for duplicate crag name")
	}
}

func TestList(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "repo")

	// Empty list
	crags, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(crags) != 0 {
		t.Errorf("expected 0 crags, got %d", len(crags))
	}

	// Create and list
	if _, err := Create("inst-a", []string{repoPath}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	crags, err = List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(crags) != 1 {
		t.Errorf("expected 1 crag, got %d", len(crags))
	}
	if _, ok := crags["inst-a"]; !ok {
		t.Error("crag inst-a not found in list")
	}
}

func TestDelete(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "repo")

	instanceDir, err := Create("to-delete", []string{repoPath})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Delete("to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify directory removed
	if _, err := os.Stat(instanceDir); !os.IsNotExist(err) {
		t.Fatal("crag directory not removed")
	}

	// Verify not in list
	crags, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := crags["to-delete"]; ok {
		t.Error("deleted crag still in list")
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "wt-repo")

	instanceDir, err := Create("wt-test", []string{repoPath})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create worktree
	worktreePath, err := CreateWorktree(instanceDir, "task-001", "wt-repo")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	expectedWT := filepath.Join(instanceDir, "tasks", "task-001", "wt-repo")
	if worktreePath != expectedWT {
		t.Errorf("worktree path = %q, want %q", worktreePath, expectedWT)
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatal("worktree directory not created")
	}

	// Remove worktree
	if err := RemoveWorktree(instanceDir, "task-001", "wt-repo"); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("worktree directory not removed")
	}
}

func TestCleanupProblemWorktrees(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "cleanup-repo")

	instanceDir, err := Create("cleanup-test", []string{repoPath})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create worktree
	if _, err := CreateWorktree(instanceDir, "task-cleanup", "cleanup-repo"); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Cleanup all worktrees for the problem
	if err := CleanupProblemWorktrees(instanceDir, "task-cleanup"); err != nil {
		t.Fatalf("CleanupProblemWorktrees: %v", err)
	}

	// Verify worktree gone
	worktreePath := filepath.Join(instanceDir, "tasks", "task-cleanup", "cleanup-repo")
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("worktree directory not cleaned up")
	}
}

func TestCragConfigPersistence(t *testing.T) {
	setupTestHome(t)
	repoPath := createTestRepo(t, "persist-repo")

	instanceDir, err := Create("persist-test", []string{repoPath})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read instance.json directly (file is still named instance.json on disk)
	data, err := os.ReadFile(filepath.Join(instanceDir, "instance.json"))
	if err != nil {
		t.Fatalf("reading crag config: %v", err)
	}

	var cfg CragConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing crag config: %v", err)
	}

	if cfg.Name != "persist-test" {
		t.Errorf("name = %q, want %q", cfg.Name, "persist-test")
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].URL != repoPath {
		t.Errorf("repo URL = %q, want %q", cfg.Repos[0].URL, repoPath)
	}
	if cfg.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}
}
