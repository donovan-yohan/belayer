package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestInitFirstRunScaffoldsDefaults(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--target", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	belayerDir := filepath.Join(dir, ".belayer")
	for _, rel := range []string{"config.yaml", "policies/standard.yaml"} {
		p := filepath.Join(belayerDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}

	wantAgents := []string{"backend-dev", "game-master", "pm", "qa", "reviewer", "supervisor", "web-dev"}
	got, err := os.ReadDir(filepath.Join(belayerDir, "agents"))
	if err != nil {
		t.Fatalf("read agents dir: %v", err)
	}
	var names []string
	for _, e := range got {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(wantAgents, ",") {
		t.Fatalf("agents: got %v, want %v", names, wantAgents)
	}

	// Each agent dir must have at least system-prompt.md to be useful.
	for _, name := range wantAgents {
		sp := filepath.Join(belayerDir, "agents", name, "system-prompt.md")
		if _, err := os.Stat(sp); err != nil {
			t.Fatalf("expected %s: %v", sp, err)
		}
	}

	kinds := map[string]string{
		"backend-dev": "main",
		"game-master": "main",
		"pm":          "side",
		"qa":          "side",
		"reviewer":    "side",
		"supervisor":  "main",
		"web-dev":     "main",
	}
	for name, kind := range kinds {
		cfg, err := os.ReadFile(filepath.Join(belayerDir, "agents", name, "agent.yaml"))
		if err != nil {
			t.Fatalf("read %s agent.yaml: %v", name, err)
		}
		if !strings.Contains(string(cfg), "\nkind: "+kind+"\n") {
			t.Fatalf("expected %s to declare kind %q, got:\n%s", name, kind, string(cfg))
		}
	}
}

func TestInitScaffoldsPersistenceStrategyBlock(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, ".belayer", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(cfg)
	if !strings.Contains(got, "\npersistence_strategy:\n") {
		t.Fatalf("expected 'persistence_strategy:' block in config.yaml, got:\n%s", got)
	}
	for _, want := range []string{
		"incomplete/<session-id>",
		"Push the branch to origin",
		"draft pull request",
		"persistence-notes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in persistence_strategy block, got:\n%s", want, got)
		}
	}
}

func TestInitWritesLogLevelStandardToConfig(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, ".belayer", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(cfg), "\nlog_level: standard\n") {
		t.Fatalf("expected 'log_level: standard' in config.yaml, got:\n%s", string(cfg))
	}
}

func TestInitScaffoldsSplitRuntimeCaps(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, ".belayer", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(cfg)
	if !strings.Contains(got, "\nruntime:\n") {
		t.Fatalf("expected runtime block in config.yaml, got:\n%s", got)
	}
	for _, want := range []string{
		"max_concurrent_mains: 8",
		"max_concurrent_sides: 15",
		"max_side_summons_per_session: 30",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q scaffold, got:\n%s", want, got)
		}
	}
}

func TestInitIdempotentReRun(t *testing.T) {
	dir := t.TempDir()

	first := newInitCmd()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	configPath := filepath.Join(dir, ".belayer", "config.yaml")
	const userEdit = "# user customization\n"
	if err := os.WriteFile(configPath, []byte(userEdit), 0o644); err != nil {
		t.Fatalf("seed user edit: %v", err)
	}

	second := newInitCmd()
	out := &bytes.Buffer{}
	second.SetOut(out)
	second.SetErr(out)
	second.SetArgs([]string{"--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if !strings.Contains(out.String(), "already initialized") {
		t.Fatalf("expected 'already initialized' message, got: %s", out.String())
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != userEdit {
		t.Fatalf("config.yaml was overwritten on re-run; got %q, want %q", string(got), userEdit)
	}
}

func TestInitForceRefreshesAgentsButPreservesConfig(t *testing.T) {
	dir := t.TempDir()

	first := newInitCmd()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	agentFile := filepath.Join(dir, ".belayer", "agents", "supervisor", "system-prompt.md")
	if err := os.WriteFile(agentFile, []byte("local hack"), 0o644); err != nil {
		t.Fatalf("mutate agent: %v", err)
	}
	configPath := filepath.Join(dir, ".belayer", "config.yaml")
	const userEdit = "# user customization\n"
	if err := os.WriteFile(configPath, []byte(userEdit), 0o644); err != nil {
		t.Fatalf("seed user edit: %v", err)
	}

	forced := newInitCmd()
	forced.SetOut(&bytes.Buffer{})
	forced.SetErr(&bytes.Buffer{})
	forced.SetArgs([]string{"--target", dir, "--force"})
	if err := forced.Execute(); err != nil {
		t.Fatalf("forced init: %v", err)
	}

	gotAgent, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}
	if string(gotAgent) == "local hack" {
		t.Fatalf("--force did not refresh shipped agent file")
	}

	gotCfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(gotCfg) != userEdit {
		t.Fatalf("--force overwrote user config; got %q, want %q", string(gotCfg), userEdit)
	}
}

func TestInitCreatesGitignoreWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	// hermes_bridge/ is no longer in the workspace; it lives in the runtime dir.
	for _, want := range []string{"/.belayer/runs/", "/.belayer/worktrees/"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("expected %q in .gitignore, got: %s", want, string(got))
		}
	}
	if strings.Contains(string(got), "/.belayer/hermes_bridge/") {
		t.Fatalf("/.belayer/hermes_bridge/ entry should not appear in workspace .gitignore; got: %s", string(got))
	}
}

func TestInitAppendsToExistingGitignoreOnce(t *testing.T) {
	dir := t.TempDir()
	const userIgnore = "node_modules/\n.env\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(userIgnore), 0o644); err != nil {
		t.Fatalf("seed gitignore: %v", err)
	}

	for i := 0; i < 2; i++ {
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--target", dir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("init #%d: %v", i, err)
		}
	}

	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.HasPrefix(string(got), userIgnore) {
		t.Fatalf("user gitignore content was not preserved at top: %q", string(got))
	}
	if strings.Count(string(got), gitignoreMarker) != 1 {
		t.Fatalf("expected belayer marker exactly once after 2 inits, got:\n%s", string(got))
	}
}

func TestInitRejectsBelayerWhenNotADirectory(t *testing.T) {
	dir := t.TempDir()
	belayerPath := filepath.Join(dir, ".belayer")
	if err := os.WriteFile(belayerPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	cmd := newInitCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--target", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected init to reject .belayer file, got success: %s", out.String())
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected 'not a directory' error, got: %v", err)
	}
}

func TestInitGitignoreCreatesWithoutLeadingBlankLine(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if len(got) == 0 || got[0] == '\n' {
		t.Fatalf("freshly-created .gitignore must not start with a blank line; got: %q", string(got))
	}
	if !strings.HasPrefix(string(got), gitignoreMarker) {
		t.Fatalf(".gitignore must start with the belayer marker; got: %q", string(got))
	}
}

func TestAutoInitIfMissingScaffoldsAndAnnounces(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	if err := autoInitIfMissing(dir, out); err != nil {
		t.Fatalf("autoInit: %v", err)
	}
	if !strings.Contains(out.String(), "Auto-initialized") {
		t.Fatalf("expected auto-init notice, got: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "game-master", "system-prompt.md")); err != nil {
		t.Fatalf("expected scaffolded game-master prompt: %v", err)
	}

	// Second call must be silent.
	out2 := &bytes.Buffer{}
	if err := autoInitIfMissing(dir, out2); err != nil {
		t.Fatalf("autoInit second call: %v", err)
	}
	if out2.Len() != 0 {
		t.Fatalf("expected silent no-op, got: %s", out2.String())
	}
}

// TestInitDoesNotExtractHermesBridgeToWorkspace verifies that 'belayer init'
// no longer places hermes_bridge/ inside the workspace .belayer/ directory.
// Bridge extraction now happens at daemon startup via extractBridgeToRuntimeDir.
func TestInitDoesNotExtractHermesBridgeToWorkspace(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	bridgeDir := filepath.Join(dir, ".belayer", "hermes_bridge")
	if _, err := os.Stat(bridgeDir); err == nil {
		t.Fatalf("hermes_bridge/ should not be in workspace .belayer/ after init; found at %s", bridgeDir)
	}
}

// TestExtractBridgeToRuntimeDirExtractsPackage verifies that the runtime
// extraction function places hermes_bridge/ at the correct location and
// filters dev-only files.
func TestExtractBridgeToRuntimeDirExtractsPackage(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := extractBridgeToRuntimeDir(runtimeDir, ""); err != nil {
		t.Fatalf("extractBridgeToRuntimeDir: %v", err)
	}

	bridgeDir := filepath.Join(runtimeDir, "hermes_bridge")
	if _, err := os.Stat(filepath.Join(bridgeDir, "__main__.py")); err != nil {
		t.Fatalf("expected hermes_bridge/__main__.py to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, "__init__.py")); err != nil {
		t.Fatalf("expected hermes_bridge/__init__.py to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, "__pycache__")); err == nil {
		t.Fatalf("__pycache__ was extracted; expected it to be filtered")
	}

	walkErr := filepath.Walk(bridgeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(bridgeDir, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() && filepath.Base(rel) == "tests" {
			t.Errorf("tests/ was extracted; expected it to be filtered: %s", path)
		}
		if !info.IsDir() {
			if strings.HasSuffix(path, ".pyc") {
				t.Errorf("unexpected .pyc file in extracted bridge: %s", path)
			}
			if strings.HasSuffix(path, ".md") {
				t.Errorf("unexpected .md file in extracted bridge: %s", path)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk bridge dir: %v", walkErr)
	}
}

// TestExtractBridgeToRuntimeDirIsIdempotent verifies that re-extracting the
// bridge restores drifted files without blowing up.
func TestExtractBridgeToRuntimeDirIsIdempotent(t *testing.T) {
	runtimeDir := t.TempDir()

	if err := extractBridgeToRuntimeDir(runtimeDir, ""); err != nil {
		t.Fatalf("first extract: %v", err)
	}

	mainPath := filepath.Join(runtimeDir, "hermes_bridge", "__main__.py")
	originalBytes, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read extracted __main__.py: %v", err)
	}

	if err := os.WriteFile(mainPath, []byte("# drifted\n"), 0o644); err != nil {
		t.Fatalf("seed drift: %v", err)
	}

	if err := extractBridgeToRuntimeDir(runtimeDir, ""); err != nil {
		t.Fatalf("second extract: %v", err)
	}

	got, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read bridge after re-extract: %v", err)
	}
	if !bytes.Equal(got, originalBytes) {
		t.Fatalf("re-extract did not restore bridge __main__.py; drift persisted")
	}
}

// TestExtractBridgeToRuntimeDirPrunesStaleFiles verifies that stale files
// (present on disk but absent from the embedded bridge) are removed.
func TestExtractBridgeToRuntimeDirPrunesStaleFiles(t *testing.T) {
	runtimeDir := t.TempDir()

	if err := extractBridgeToRuntimeDir(runtimeDir, ""); err != nil {
		t.Fatalf("first extract: %v", err)
	}

	bridgeDir := filepath.Join(runtimeDir, "hermes_bridge")
	stalePath := filepath.Join(bridgeDir, "stale_removed_module.py")
	if err := os.WriteFile(stalePath, []byte("# stale\n"), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}
	staleSubdir := filepath.Join(bridgeDir, "removed_subpackage")
	if err := os.MkdirAll(staleSubdir, 0o755); err != nil {
		t.Fatalf("seed stale subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleSubdir, "__init__.py"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed stale subdir file: %v", err)
	}

	if err := extractBridgeToRuntimeDir(runtimeDir, ""); err != nil {
		t.Fatalf("second extract: %v", err)
	}

	if _, err := os.Stat(stalePath); err == nil {
		t.Fatalf("stale file survived re-extract: %s", stalePath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale file: %v", err)
	}
	if _, err := os.Stat(staleSubdir); err == nil {
		t.Fatalf("stale subdir survived re-extract: %s", staleSubdir)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale subdir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bridgeDir, "__main__.py")); err != nil {
		t.Fatalf("expected __main__.py after prune: %v", err)
	}
}

// TestInitGitignoreUpgradeIsNoOpWhenAllEntriesPresent verifies that init
// does not append duplicate entries when all current entries are already
// present.
func TestInitGitignoreUpgradeIsNoOpWhenAllEntriesPresent(t *testing.T) {
	dir := t.TempDir()
	// Seed a .gitignore with the current header and both entries.
	existing := gitignoreHeader + "\n/.belayer/runs/\n/.belayer/worktrees/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644); err != nil {
		t.Fatalf("seed gitignore: %v", err)
	}

	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if strings.Contains(string(got), gitignoreUpgradeHeader) {
		t.Fatalf("upgrade header should not appear when all entries present:\n%s", string(got))
	}
	if strings.Count(string(got), "/.belayer/runs/") != 1 {
		t.Fatalf("existing /.belayer/runs/ entry should not be duplicated:\n%s", string(got))
	}
}

// TestResolveRuntimeDirPrecedence verifies the env var and XDG resolution order.
func TestResolveRuntimeDirPrecedence(t *testing.T) {
	t.Setenv("BELAYER_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")

	t.Run("env_var_wins", func(t *testing.T) {
		t.Setenv("BELAYER_RUNTIME_DIR", "/custom/runtime")
		got, err := resolveRuntimeDir("")
		if err != nil {
			t.Fatalf("resolveRuntimeDir: %v", err)
		}
		if got != "/custom/runtime" {
			t.Fatalf("expected /custom/runtime, got %q", got)
		}
	})

	t.Run("xdg_state_home", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/xdg/state")
		got, err := resolveRuntimeDir("")
		if err != nil {
			t.Fatalf("resolveRuntimeDir: %v", err)
		}
		if got != "/xdg/state/belayer/runtime" {
			t.Fatalf("expected /xdg/state/belayer/runtime, got %q", got)
		}
	})

	t.Run("config_yaml_runtime_dir", func(t *testing.T) {
		workspaceDir := t.TempDir()
		belayerDir := filepath.Join(workspaceDir, ".belayer")
		if err := os.MkdirAll(belayerDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		cfgContent := "log_level: standard\nruntime_dir: /from/config\n"
		if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(cfgContent), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		got, err := resolveRuntimeDir(workspaceDir)
		if err != nil {
			t.Fatalf("resolveRuntimeDir: %v", err)
		}
		if got != "/from/config" {
			t.Fatalf("expected /from/config, got %q", got)
		}
	})
}
