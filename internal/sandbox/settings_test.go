package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/sandbox"
)

func TestLoadSettingsMissingFileReturnsZero(t *testing.T) {
	s, err := sandbox.LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSettings with no config.yaml returned error: %v", err)
	}
	if s.Mode != "" || s.Policy != "" {
		t.Errorf("expected zero Settings, got %+v", s)
	}
	if got := s.ModeOrDefault(); got != sandbox.DefaultMode {
		t.Errorf("ModeOrDefault on zero Settings = %q, want %q", got, sandbox.DefaultMode)
	}
}

func TestLoadSettingsEmptyWorkdirReturnsZero(t *testing.T) {
	s, err := sandbox.LoadSettings("")
	if err != nil {
		t.Fatalf("LoadSettings(\"\") returned error: %v", err)
	}
	if s.Mode != "" {
		t.Errorf("expected empty Mode, got %q", s.Mode)
	}
}

func TestLoadSettingsParsesSandboxSection(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
runtime:
  up: "pnpm dev"
sandbox:
  mode: clamshell
  policy: .belayer/policies/belayer-standard.yaml
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Mode != "clamshell" {
		t.Errorf("Mode = %q, want clamshell", s.Mode)
	}
	// Relative Policy paths in config.yaml must be anchored to workdir so
	// drivers can os.ReadFile them regardless of the daemon's cwd.
	wantPolicy := filepath.Join(dir, ".belayer/policies/belayer-standard.yaml")
	if s.Policy != wantPolicy {
		t.Errorf("Policy = %q, want %q", s.Policy, wantPolicy)
	}
	if got := s.ModeOrDefault(); got != "clamshell" {
		t.Errorf("ModeOrDefault = %q, want clamshell", got)
	}
}

func TestLoadSettingsMissingSectionYieldsDefault(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
runtime:
  up: "pnpm dev"
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Mode != "" {
		t.Errorf("Mode = %q, want empty", s.Mode)
	}
	if got := s.ModeOrDefault(); got != sandbox.DefaultMode {
		t.Errorf("ModeOrDefault = %q, want %q", got, sandbox.DefaultMode)
	}
}

func TestLoadSettingsAbsolutePolicyIsPreserved(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  mode: clamshell
  policy: /etc/belayer/strict.yaml
`)
	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Policy != "/etc/belayer/strict.yaml" {
		t.Errorf("absolute Policy = %q, want unchanged", s.Policy)
	}
}

func TestModeOrDefaultEnvOverrideTakesPrecedence(t *testing.T) {
	t.Setenv(sandbox.ModeOverrideEnv, "noop")
	s := sandbox.Settings{Mode: "clamshell"}
	if got := s.ModeOrDefault(); got != "noop" {
		t.Errorf("ModeOrDefault with env override = %q, want noop", got)
	}
}

func TestModeOrDefaultEnvOverrideTrimsWhitespace(t *testing.T) {
	t.Setenv(sandbox.ModeOverrideEnv, "  noop\n")
	s := sandbox.Settings{Mode: "clamshell"}
	if got := s.ModeOrDefault(); got != "noop" {
		t.Errorf("ModeOrDefault with whitespace env override = %q, want noop (trimmed)", got)
	}
}

func TestModeOrDefaultEnvOverrideWhitespaceOnlyTreatedAsUnset(t *testing.T) {
	t.Setenv(sandbox.ModeOverrideEnv, "   \t\n")
	s := sandbox.Settings{Mode: "clamshell"}
	if got := s.ModeOrDefault(); got != "clamshell" {
		t.Errorf("ModeOrDefault with whitespace-only env override = %q, want fallthrough to Mode (clamshell)", got)
	}
}

func TestLoadSettingsInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, "sandbox: [not-a-map\n")
	_, err := sandbox.LoadSettings(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func writeConfigYAML(t *testing.T, dir, contents string) {
	t.Helper()
	belayerDir := filepath.Join(dir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o700); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}
