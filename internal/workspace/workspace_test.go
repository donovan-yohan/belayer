package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a WorkspaceConfig as repos.json into dir and returns the
// full path to the written file.
func writeConfig(t *testing.T, dir string, cfg WorkspaceConfig) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	p := filepath.Join(dir, "repos.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// TestLoad_ValidConfig verifies that Load parses a well-formed repos.json and
// resolves the workspace directory correctly.
func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git", Branch: "feature/v6", DefaultBranch: "master"},
		},
	}
	p := writeConfig(t, dir, cfg)

	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ws.Dir() != dir {
		t.Errorf("Dir: got %q, want %q", ws.Dir(), dir)
	}
	repos := ws.Repos()
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Name != "extend-api" {
		t.Errorf("repo name: got %q, want %q", repos[0].Name, "extend-api")
	}
}

// TestLoad_MissingFile verifies that Load returns an error when the config
// file does not exist.
func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/tmp/does-not-exist-belayer/repos.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestLoad_InvalidJSON verifies that Load returns an error for malformed JSON.
func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "repos.json")
	if err := os.WriteFile(p, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestRepoPath_KnownRepo verifies that RepoPath returns the correct path for a
// configured repo that uses the default (name-derived) path.
func TestRepoPath_KnownRepo(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := ws.RepoPath("extend-api")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	want := filepath.Join(dir, "extend-api")
	if got != want {
		t.Errorf("RepoPath: got %q, want %q", got, want)
	}
}

// TestRepoPath_UnknownRepo verifies that RepoPath returns an error for a repo
// that is not in the config.
func TestRepoPath_UnknownRepo(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos:   []RepoConfig{},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err = ws.RepoPath("nonexistent-repo")
	if err == nil {
		t.Fatal("expected error for unknown repo, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-repo") {
		t.Errorf("expected error to mention repo name, got: %v", err)
	}
}

// TestEnsureReady_MissingRepos verifies that EnsureReady returns a descriptive
// error listing repos whose local directories do not exist.
func TestEnsureReady_MissingRepos(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
			{Name: "extend-app", URL: "git@github.com:example/extend-app.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	err = ws.EnsureReady()
	if err == nil {
		t.Fatal("expected error for missing repos, got nil")
	}
	if !strings.Contains(err.Error(), "extend-api") {
		t.Errorf("expected error to mention 'extend-api', got: %v", err)
	}
	if !strings.Contains(err.Error(), "extend-app") {
		t.Errorf("expected error to mention 'extend-app', got: %v", err)
	}
}

// TestEnsureReady_AllPresent verifies that EnsureReady returns nil when all
// repo directories exist.
func TestEnsureReady_AllPresent(t *testing.T) {
	dir := t.TempDir()

	// Pre-create the repo directories to simulate already-cloned repos.
	for _, name := range []string{"extend-api", "extend-app"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
			{Name: "extend-app", URL: "git@github.com:example/extend-app.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := ws.EnsureReady(); err != nil {
		t.Errorf("EnsureReady: unexpected error: %v", err)
	}
}

// TestPathResolution_ExplicitPath verifies that RepoPath uses the explicit
// path from the config when set.
func TestPathResolution_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	customPath := "/custom/path/extend-app"
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-app", URL: "git@github.com:example/extend-app.git", Path: customPath},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := ws.RepoPath("extend-app")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	if got != customPath {
		t.Errorf("RepoPath with explicit path: got %q, want %q", got, customPath)
	}
}

// TestPathResolution_RelativeExplicitPath verifies that a relative explicit
// path is resolved relative to the workspace base directory.
func TestPathResolution_RelativeExplicitPath(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-app", URL: "git@github.com:example/extend-app.git", Path: "subdir/extend-app"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := ws.RepoPath("extend-app")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	want := filepath.Join(dir, "subdir", "extend-app")
	if got != want {
		t.Errorf("RepoPath with relative path: got %q, want %q", got, want)
	}
}

// TestPathResolution_DerivedFromName verifies that when no explicit path is
// set, the path is derived as <base_dir>/<name>.
func TestPathResolution_DerivedFromName(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := ws.RepoPath("extend-api")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	want := filepath.Join(dir, "extend-api")
	if got != want {
		t.Errorf("RepoPath derived from name: got %q, want %q", got, want)
	}
}

// TestRepos_ReturnsAll verifies that Repos returns all configured repos.
func TestRepos_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
			{Name: "extend-app", URL: "git@github.com:example/extend-app.git"},
			{Name: "extend-web", URL: "git@github.com:example/extend-web.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	repos := ws.Repos()
	if len(repos) != 3 {
		t.Fatalf("Repos: expected 3, got %d", len(repos))
	}
	names := map[string]bool{}
	for _, r := range repos {
		names[r.Name] = true
	}
	for _, want := range []string{"extend-api", "extend-app", "extend-web"} {
		if !names[want] {
			t.Errorf("Repos: missing %q", want)
		}
	}
}

// TestLoad_BaseDirRelative verifies that a relative base_dir in the config is
// resolved relative to the config file's directory.
func TestLoad_BaseDirRelative(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: "repos", // relative
		Repos:   []RepoConfig{{Name: "extend-api"}},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(dir, "repos")
	if ws.Dir() != want {
		t.Errorf("Dir with relative base_dir: got %q, want %q", ws.Dir(), want)
	}
}

// TestLoad_EmptyBaseDir verifies that when base_dir is omitted, the workspace
// dir defaults to the directory containing the config file.
func TestLoad_EmptyBaseDir(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		// BaseDir intentionally empty
		Repos: []RepoConfig{{Name: "extend-api"}},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ws.Dir() != dir {
		t.Errorf("Dir with empty base_dir: got %q, want %q", ws.Dir(), dir)
	}
}

// TestClone_SkipsExistingDir verifies that Clone does not attempt a git clone
// when the destination directory already exists (no network calls in tests).
func TestClone_SkipsExistingDir(t *testing.T) {
	dir := t.TempDir()

	// Pre-create the repo directory.
	repoDir := filepath.Join(dir, "extend-api")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos: []RepoConfig{
			{Name: "extend-api", URL: "git@github.com:example/extend-api.git"},
		},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should succeed without calling git (directory already exists).
	if err := ws.Clone("extend-api"); err != nil {
		t.Errorf("Clone with existing dir: unexpected error: %v", err)
	}
}

// TestClone_UnknownRepo verifies that Clone returns an error for a repo not in
// the config.
func TestClone_UnknownRepo(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		BaseDir: dir,
		Repos:   []RepoConfig{},
	}
	p := writeConfig(t, dir, cfg)
	ws, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := ws.Clone("nonexistent"); err == nil {
		t.Fatal("expected error for unknown repo, got nil")
	}
}
