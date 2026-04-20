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

	wantAgents := []string{"backend-dev", "pm", "qa", "reviewer", "supervisor", "web-dev"}
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

func TestInitIdempotentReRun(t *testing.T) {
	dir := t.TempDir()

	first := newInitCmd()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Edit config.yaml so we can prove the second run does not clobber it.
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

	// Mutate a shipped agent file and a user config file.
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
	for _, want := range []string{"/.belayer/runs/", "/.belayer/worktrees/", "/.belayer/hermes_bridge/"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("expected %q in .gitignore, got: %s", want, string(got))
		}
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
	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "supervisor", "system-prompt.md")); err != nil {
		t.Fatalf("expected scaffolded supervisor prompt: %v", err)
	}

	// Second call must be silent — the bridge refresh is a hot-path no-op from
	// the user's perspective (files may be rewritten byte-for-byte but the
	// announce is suppressed).
	out2 := &bytes.Buffer{}
	if err := autoInitIfMissing(dir, out2); err != nil {
		t.Fatalf("autoInit second call: %v", err)
	}
	if out2.Len() != 0 {
		t.Fatalf("expected silent no-op, got: %s", out2.String())
	}
}

func TestInitExtractsHermesBridge(t *testing.T) {
	dir := t.TempDir()
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--target", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	bridgeDir := filepath.Join(dir, ".belayer", "hermes_bridge")
	// __main__.py is the package entry point; its absence is a hard failure.
	if _, err := os.Stat(filepath.Join(bridgeDir, "__main__.py")); err != nil {
		t.Fatalf("expected hermes_bridge/__main__.py to exist: %v", err)
	}
	// __init__.py makes it an importable package.
	if _, err := os.Stat(filepath.Join(bridgeDir, "__init__.py")); err != nil {
		t.Fatalf("expected hermes_bridge/__init__.py to exist: %v", err)
	}

	// __pycache__ from the host build must never leak into the extracted copy.
	if _, err := os.Stat(filepath.Join(bridgeDir, "__pycache__")); err == nil {
		t.Fatalf("__pycache__ was extracted; expected it to be filtered")
	}

	// No *.pyc files at any depth, and no dev-only trees (tests/, *.md).
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

func TestInitRefreshesBridgeOnReInit(t *testing.T) {
	dir := t.TempDir()

	first := newInitCmd()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	mainPath := filepath.Join(dir, ".belayer", "hermes_bridge", "__main__.py")
	originalBytes, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read extracted __main__.py: %v", err)
	}

	// Simulate drift: replace the extracted bridge file with garbage.
	if err := os.WriteFile(mainPath, []byte("# drifted\n"), 0o644); err != nil {
		t.Fatalf("seed drift: %v", err)
	}

	second := newInitCmd()
	out := &bytes.Buffer{}
	second.SetOut(out)
	second.SetErr(out)
	second.SetArgs([]string{"--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second init: %v", err)
	}

	// Output must announce the bridge refresh.
	if !strings.Contains(out.String(), "refreshed") {
		t.Fatalf("expected re-init output to mention refreshed bridge files, got: %s", out.String())
	}

	got, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read bridge after re-init: %v", err)
	}
	if !bytes.Equal(got, originalBytes) {
		t.Fatalf("re-init did not restore bridge __main__.py; drift persisted")
	}
}

func TestInitGitignoreUpgradeAppendsMissingEntry(t *testing.T) {
	dir := t.TempDir()
	// Seed a .gitignore that looks like a project initialized under an older
	// belayer version: it has the marker and the two historical entries but
	// not /.belayer/hermes_bridge/.
	legacy := gitignoreHeader + "\n/.belayer/runs/\n/.belayer/worktrees/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("seed legacy gitignore: %v", err)
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
	gotStr := string(got)
	if !strings.Contains(gotStr, "/.belayer/hermes_bridge/") {
		t.Fatalf("expected missing entry to be appended, got:\n%s", gotStr)
	}
	// Must not duplicate entries the user already had.
	if strings.Count(gotStr, "/.belayer/runs/") != 1 {
		t.Fatalf("existing /.belayer/runs/ entry should not be duplicated:\n%s", gotStr)
	}
	// Original marker should still appear exactly once — upgrade uses a
	// separate labelled block.
	if strings.Count(gotStr, gitignoreMarker) != 1 {
		t.Fatalf("marker should appear exactly once; got:\n%s", gotStr)
	}
	// Upgrade header visible so users see where the new entry came from.
	if !strings.Contains(gotStr, gitignoreUpgradeHeader) {
		t.Fatalf("expected upgrade header in gitignore, got:\n%s", gotStr)
	}

	// Re-running should be a no-op now — all entries are present.
	second := newInitCmd()
	second.SetOut(&bytes.Buffer{})
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second init: %v", err)
	}
	got2, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore #2: %v", err)
	}
	if !bytes.Equal(got, got2) {
		t.Fatalf("second init mutated gitignore; before=%q after=%q", got, got2)
	}
}

func TestInitPrunesStaleBridgeFiles(t *testing.T) {
	dir := t.TempDir()

	first := newInitCmd()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Simulate an upstream rename/deletion by planting a stale file in the
	// extracted tree that doesn't exist in the embedded bridge.
	bridgeDir := filepath.Join(dir, ".belayer", "hermes_bridge")
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

	second := newInitCmd()
	second.SetOut(&bytes.Buffer{})
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second init: %v", err)
	}

	if _, err := os.Stat(stalePath); err == nil {
		t.Fatalf("stale file survived re-init: %s", stalePath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale file: %v", err)
	}
	if _, err := os.Stat(staleSubdir); err == nil {
		t.Fatalf("stale subdir survived re-init: %s", staleSubdir)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale subdir: %v", err)
	}

	// The real bridge files must still be present.
	if _, err := os.Stat(filepath.Join(bridgeDir, "__main__.py")); err != nil {
		t.Fatalf("expected __main__.py after prune: %v", err)
	}
}

func TestAutoInitRefreshesBridgeOnExistingProject(t *testing.T) {
	dir := t.TempDir()

	// First auto-init creates everything.
	if err := autoInitIfMissing(dir, &bytes.Buffer{}); err != nil {
		t.Fatalf("first autoInit: %v", err)
	}

	mainPath := filepath.Join(dir, ".belayer", "hermes_bridge", "__main__.py")
	originalBytes, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read bridge: %v", err)
	}
	if err := os.WriteFile(mainPath, []byte("# drifted\n"), 0o644); err != nil {
		t.Fatalf("seed drift: %v", err)
	}

	// Second auto-init (simulating another `belayer run`) must refresh bridge
	// silently — the announce is suppressed on already-initialized projects.
	out := &bytes.Buffer{}
	if err := autoInitIfMissing(dir, out); err != nil {
		t.Fatalf("second autoInit: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected silent refresh on existing project, got: %s", out.String())
	}

	got, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read bridge after auto re-init: %v", err)
	}
	if !bytes.Equal(got, originalBytes) {
		t.Fatalf("auto-init did not refresh drifted bridge file")
	}
}
