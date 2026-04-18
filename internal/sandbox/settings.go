package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DefaultMode is the driver name resolved when .belayer/config.yaml has no
// sandbox section (or no sandbox.mode). The noop driver ships with belayer
// and is registered unconditionally.
const DefaultMode = "noop"

// ModeOverrideEnv, when set in the daemon process env, takes precedence over
// per-workspace config.yaml sandbox.mode. This is how an outer sandbox (e.g.
// clamshell running the daemon inside a one-container-per-run image) tells
// belayer "you're already sandboxed by me, use noop" without requiring every
// downstream repo's .belayer/config.yaml to opt out of mode: clamshell.
const ModeOverrideEnv = "BELAYER_SANDBOX_MODE"

// Settings holds the sandbox section of .belayer/config.yaml. A zero Settings
// signals default-noop behavior; see Settings.ModeOrDefault.
type Settings struct {
	Mode   string `yaml:"mode"`
	Policy string `yaml:"policy"`
}

// ModeOrDefault returns the effective sandbox mode, resolving in this order:
//  1. $BELAYER_SANDBOX_MODE, if set — an outer sandbox can override downstream
//     workspace config. Trimmed; empty string is treated as unset.
//  2. Settings.Mode, if non-empty — from the workspace's .belayer/config.yaml.
//  3. DefaultMode ("noop").
func (s Settings) ModeOrDefault() string {
	if override := strings.TrimSpace(os.Getenv(ModeOverrideEnv)); override != "" {
		return override
	}
	if s.Mode == "" {
		return DefaultMode
	}
	return s.Mode
}

type settingsFile struct {
	Sandbox Settings `yaml:"sandbox"`
}

// LoadSettings reads the sandbox section from <workdir>/.belayer/config.yaml.
// A missing file, empty workdir, or missing section returns a zero Settings
// and nil error; callers treat an empty Mode as DefaultMode via ModeOrDefault.
func LoadSettings(workdir string) (Settings, error) {
	if workdir == "" {
		return Settings{}, nil
	}
	path := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("sandbox: read config: %w", err)
	}
	var f settingsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Settings{}, fmt.Errorf("sandbox: parse config: %w", err)
	}
	// Policy paths in config.yaml are written relative to the workspace root
	// (e.g. ".belayer/policies/default.yaml"). The daemon's cwd is unrelated,
	// so anchor non-absolute paths to workdir before handing them to drivers.
	if f.Sandbox.Policy != "" && !filepath.IsAbs(f.Sandbox.Policy) {
		f.Sandbox.Policy = filepath.Join(workdir, f.Sandbox.Policy)
	}
	return f.Sandbox, nil
}
