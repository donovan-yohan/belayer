package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes content into <dir>/.belayer/config.yaml, creating
// the directory if needed.
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	belayerDir := filepath.Join(dir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o755); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// TestLoadProjectConfig_DefaultsWhenMissing verifies that a missing
// config.yaml returns SkipOpenRouterProbe=true.
func TestLoadProjectConfig_DefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=true when config.yaml is missing")
	}
}

// TestLoadProjectConfig_DefaultsWhenBridgeSectionAbsent verifies that a
// config.yaml without a bridge section returns SkipOpenRouterProbe=true.
func TestLoadProjectConfig_DefaultsWhenBridgeSectionAbsent(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "log_level: standard\n")

	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=true when bridge section is absent")
	}
}

// TestLoadProjectConfig_ExplicitTrue verifies that bridge.skip_openrouter_probe: true
// is parsed correctly.
func TestLoadProjectConfig_ExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "bridge:\n  skip_openrouter_probe: true\n")

	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=true")
	}
}

// TestLoadProjectConfig_ExplicitFalse verifies that bridge.skip_openrouter_probe: false
// overrides the default.
func TestLoadProjectConfig_ExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "bridge:\n  skip_openrouter_probe: false\n")

	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=false when explicitly set to false")
	}
}

// TestLoadProjectConfig_EmptyWorkdir verifies that an empty workdir returns defaults.
func TestLoadProjectConfig_EmptyWorkdir(t *testing.T) {
	cfg, err := LoadProjectConfig("")
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=true for empty workdir")
	}
}

// TestLoadProjectConfig_BridgeSectionPresentKeyMissing verifies that a bridge
// section present but with no skip_openrouter_probe key still defaults to true.
func TestLoadProjectConfig_BridgeSectionPresentKeyMissing(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "bridge:\n  # no skip_openrouter_probe key\n")

	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if !cfg.SkipOpenRouterProbe {
		t.Error("expected SkipOpenRouterProbe=true when key is absent from bridge section")
	}
}
