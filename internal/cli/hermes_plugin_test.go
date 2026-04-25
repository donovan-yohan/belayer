package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractPluginsToHermesHome_WritesPluginFiles verifies that the
// embedded plugin tree lands at $HERMES_HOME/plugins/belayer/ on first run
// and that the manifest + __init__.py are both copied. This is the
// contract Hermes PluginManager relies on at discovery time.
func TestExtractPluginsToHermesHome_WritesPluginFiles(t *testing.T) {
	hermesHome := t.TempDir()

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("extractPluginsToHermesHome: %v", err)
	}

	manifest := filepath.Join(hermesHome, "plugins", "belayer", "plugin.yaml")
	init := filepath.Join(hermesHome, "plugins", "belayer", "__init__.py")

	for _, p := range []string{manifest, init} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist after extraction: %v", p, err)
		}
	}
}

// TestExtractPluginsToHermesHome_Idempotent verifies that running the
// extractor twice with no changes in between leaves file mtimes untouched
// (same SHA skips write). Cheap regression canary: a sloppy rewrite would
// churn the user's plugin directory on every daemon start.
func TestExtractPluginsToHermesHome_Idempotent(t *testing.T) {
	hermesHome := t.TempDir()

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	init := filepath.Join(hermesHome, "plugins", "belayer", "__init__.py")
	info1, err := os.Stat(init)
	if err != nil {
		t.Fatalf("stat after first extract: %v", err)
	}
	mtime1 := info1.ModTime()

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	info2, err := os.Stat(init)
	if err != nil {
		t.Fatalf("stat after second extract: %v", err)
	}
	if !info2.ModTime().Equal(mtime1) {
		t.Errorf("second extract rewrote file unnecessarily (mtime changed: %v → %v)", mtime1, info2.ModTime())
	}
}

// TestExtractPluginsToHermesHome_RewritesModifiedFile verifies the inverse:
// if a user (or an old extraction) left a stale/modified file, the next
// extraction overwrites it. Guards against half-upgraded plugin state.
func TestExtractPluginsToHermesHome_RewritesModifiedFile(t *testing.T) {
	hermesHome := t.TempDir()

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	init := filepath.Join(hermesHome, "plugins", "belayer", "__init__.py")
	if err := os.WriteFile(init, []byte("# stomped by user\n"), 0o644); err != nil {
		t.Fatalf("stomp file: %v", err)
	}

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	data, err := os.ReadFile(init)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if strings.Contains(string(data), "stomped by user") {
		t.Error("stale __init__.py was not overwritten on re-extract")
	}
	if !strings.Contains(string(data), "def register(ctx)") {
		t.Error("restored __init__.py does not contain register() — extractor broken")
	}
}

// TestExtractPluginsToHermesHome_PrunesStaleFile verifies that a file
// previously extracted but no longer in the embed is removed. Covers the
// case where a plugin file is renamed between belayer versions.
func TestExtractPluginsToHermesHome_PrunesStaleFile(t *testing.T) {
	hermesHome := t.TempDir()

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	stale := filepath.Join(hermesHome, "plugins", "belayer", "leftover_v1.py")
	if err := os.WriteFile(stale, []byte("# from an older belayer\n"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("expected leftover_v1.py to be pruned on re-extract, stat err = %v", err)
	}
}

// TestExtractPluginsToHermesHome_PreservesUnrelatedPlugins verifies that
// the extractor touches ONLY plugins we own. A user-installed plugin at
// $HERMES_HOME/plugins/other/ must survive belayer's extraction pass.
func TestExtractPluginsToHermesHome_PreservesUnrelatedPlugins(t *testing.T) {
	hermesHome := t.TempDir()
	otherPlugin := filepath.Join(hermesHome, "plugins", "other", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(otherPlugin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(otherPlugin, []byte("name: other\n"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}

	if err := extractPluginsToHermesHome(hermesHome); err != nil {
		t.Fatalf("extract: %v", err)
	}

	if _, err := os.Stat(otherPlugin); err != nil {
		t.Errorf("extraction clobbered unrelated plugin at %s: %v", otherPlugin, err)
	}
}

// --- injectEnabledPlugin (pure function) --------------------------------

func TestInjectEnabledPlugin_NoPluginsSection(t *testing.T) {
	in := []string{
		"model: claude-opus-4",
		"terminal:",
		"  cwd: /home/user",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if !changed {
		t.Fatal("expected change when plugins: section is missing")
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "plugins:") {
		t.Error("expected plugins: section to be appended")
	}
	if !strings.Contains(got, "- belayer") {
		t.Error("expected - belayer list item")
	}
}

func TestInjectEnabledPlugin_PluginsNoEnabled(t *testing.T) {
	in := []string{
		"plugins:",
		"  disabled:",
		"    - old",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if !changed {
		t.Fatal("expected change when enabled: subkey is missing")
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "  enabled:") {
		t.Errorf("expected enabled: subkey to be injected; got:\n%s", got)
	}
	if !strings.Contains(got, "    - belayer") {
		t.Errorf("expected list item for belayer; got:\n%s", got)
	}
}

func TestInjectEnabledPlugin_AlreadyEnabled_NoOp(t *testing.T) {
	in := []string{
		"plugins:",
		"  enabled:",
		"    - belayer",
		"    - foo",
	}
	_, changed := injectEnabledPlugin(in, "belayer")
	if changed {
		t.Error("expected no change when plugin is already in enabled list")
	}
}

func TestInjectEnabledPlugin_BlockListAppend(t *testing.T) {
	in := []string{
		"plugins:",
		"  enabled:",
		"    - foo",
		"    - bar",
		"other_key: baz",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if !changed {
		t.Fatal("expected change to append block-style list item")
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "    - belayer") {
		t.Errorf("expected - belayer appended; got:\n%s", got)
	}
	// other_key must remain after the list.
	belayerIdx := strings.Index(got, "- belayer")
	otherIdx := strings.Index(got, "other_key:")
	if belayerIdx == -1 || otherIdx == -1 || belayerIdx > otherIdx {
		t.Errorf("ordering wrong: belayer=%d other=%d\n%s", belayerIdx, otherIdx, got)
	}
}

func TestInjectEnabledPlugin_InlineListTransformed(t *testing.T) {
	in := []string{
		"plugins:",
		"  enabled: [foo, bar]",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if !changed {
		t.Fatal("expected change to transform inline list")
	}
	got := strings.Join(out, "\n")
	// Verify all items preserved + belayer added in block form.
	for _, want := range []string{"- foo", "- bar", "- belayer"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[foo, bar]") {
		t.Errorf("inline list should have been replaced; got:\n%s", got)
	}
}

func TestInjectEnabledPlugin_InlineList_AlreadyEnabled(t *testing.T) {
	in := []string{
		"plugins:",
		"  enabled: [belayer, foo]",
	}
	_, changed := injectEnabledPlugin(in, "belayer")
	if changed {
		t.Error("expected no change when inline list already includes plugin")
	}
}

// TestInjectEnabledPlugin_FourSpaceIndent_AlreadyEnabled guards against the
// regression where any indentation under plugins: that wasn't exactly two
// spaces was treated as "no enabled: present", causing a duplicate enabled:
// block to be injected and the resulting config.yaml to be invalid.
func TestInjectEnabledPlugin_FourSpaceIndent_AlreadyEnabled(t *testing.T) {
	in := []string{
		"plugins:",
		"    enabled:",
		"        - belayer",
		"        - foo",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if changed {
		t.Errorf("expected no change when 4-space-indented enabled: already includes plugin; got:\n%s", strings.Join(out, "\n"))
	}
}

// TestInjectEnabledPlugin_FourSpaceIndent_AppendItem guards the corresponding
// append path: a 4-space-indented enabled: list must have new items appended
// at the same indent level as existing items, not at a hard-coded 6 spaces.
func TestInjectEnabledPlugin_FourSpaceIndent_AppendItem(t *testing.T) {
	in := []string{
		"plugins:",
		"    enabled:",
		"        - foo",
	}
	out, changed := injectEnabledPlugin(in, "belayer")
	if !changed {
		t.Fatal("expected change to append item to 4-space-indented list")
	}
	got := strings.Join(out, "\n")
	if !strings.Contains(got, "        - belayer") {
		t.Errorf("expected belayer appended at 8-space indent (matching - foo); got:\n%s", got)
	}
	// Must not contain a duplicate enabled: block.
	if strings.Count(got, "enabled:") != 1 {
		t.Errorf("expected exactly one enabled: key; got:\n%s", got)
	}
}

// --- ensureHermesPluginEnabled -------------------------------------------

func TestEnsureHermesPluginEnabled_CreatesMissingConfig(t *testing.T) {
	hermesHome := t.TempDir()

	changed, err := ensureHermesPluginEnabled(hermesHome, "belayer")
	if err != nil {
		t.Fatalf("ensureHermesPluginEnabled: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for first install")
	}

	data, err := os.ReadFile(filepath.Join(hermesHome, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "plugins:") || !strings.Contains(got, "- belayer") {
		t.Errorf("generated config missing expected keys:\n%s", got)
	}
}

func TestEnsureHermesPluginEnabled_Idempotent(t *testing.T) {
	hermesHome := t.TempDir()

	if _, err := ensureHermesPluginEnabled(hermesHome, "belayer"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	changed, err := ensureHermesPluginEnabled(hermesHome, "belayer")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if changed {
		t.Error("expected changed=false on second call (plugin already enabled)")
	}
}

func TestEnsureHermesPluginEnabled_PreservesExistingKeys(t *testing.T) {
	hermesHome := t.TempDir()
	cfgPath := filepath.Join(hermesHome, "config.yaml")
	original := "# User config — do not touch comments\nmodel: claude-opus-4\nterminal:\n  cwd: /home/user\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if _, err := ensureHermesPluginEnabled(hermesHome, "belayer"); err != nil {
		t.Fatalf("ensureHermesPluginEnabled: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"# User config — do not touch comments",
		"model: claude-opus-4",
		"terminal:",
		"  cwd: /home/user",
		"- belayer",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q after mutation:\n%s", want, got)
		}
	}
}
