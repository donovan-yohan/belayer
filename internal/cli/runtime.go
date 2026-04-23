package cli

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	belayer "github.com/donovan-yohan/belayer"
)

// resolveRuntimeDir returns the directory where the daemon should extract the
// hermes_bridge package. Resolution order:
//
//  1. $BELAYER_RUNTIME_DIR env var (clamshell: preset to /opt/belayer/runtime)
//  2. Optional runtime_dir field in .belayer/config.yaml (workspaceDir may be "")
//  3. <workspaceDir>/.belayer/runtime when workspaceDir is non-empty
//  4. $XDG_STATE_HOME/belayer/runtime if $XDG_STATE_HOME is set
//  5. $HOME/.local/state/belayer/runtime (always available fallback)
func resolveRuntimeDir(workspaceDir string) (string, error) {
	// 1. Explicit env override.
	if v := strings.TrimSpace(os.Getenv("BELAYER_RUNTIME_DIR")); v != "" {
		return v, nil
	}

	// 2. Optional runtime_dir key in .belayer/config.yaml.
	// We do a simple line-scan so we don't need to import the YAML parser here.
	if workspaceDir != "" {
		cfgPath := filepath.Join(workspaceDir, ".belayer", "config.yaml")
		if data, err := os.ReadFile(cfgPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "runtime_dir:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "runtime_dir:"))
					val = strings.Trim(val, `"'`)
					if val != "" {
						return val, nil
					}
				}
			}
		}
	}

	// 3. Workspace-local default.
	if workspaceDir != "" {
		return filepath.Join(workspaceDir, ".belayer", "runtime"), nil
	}

	// 4. $XDG_STATE_HOME/belayer/runtime
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "belayer", "runtime"), nil
	}

	// 5. $HOME/.local/state/belayer/runtime
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve runtime dir: $HOME unavailable: %w", err)
	}
	return filepath.Join(home, ".local", "state", "belayer", "runtime"), nil
}

// legacyBridgeWarningOnce guards the one-shot deprecation log below.
var legacyBridgeWarningOnce sync.Once

// extractBridgeToRuntimeDir ensures the embedded hermes_bridge package is
// present and current in runtimeDir. It is idempotent: files whose size and
// SHA-256 already match the embedded version are left untouched. Files that
// have been deleted, modified, or never written are (re)extracted. Stale files
// (present on disk but absent from the embed) are removed.
//
// If workspaceDir is non-empty and runtimeDir is the canonical runtime path
// (i.e. BELAYER_RUNTIME_DIR was not set), the function also checks for a
// legacy hermes_bridge in <workspaceDir>/.belayer/hermes_bridge/ and logs a
// one-shot deprecation warning when found.
func extractBridgeToRuntimeDir(runtimeDir, workspaceDir string) error {
	// Legacy compat: warn once if the old workspace location still exists.
	if workspaceDir != "" {
		legacyPath := filepath.Join(workspaceDir, ".belayer", "hermes_bridge", "__main__.py")
		if _, err := os.Stat(legacyPath); err == nil {
			legacyBridgeWarningOnce.Do(func() {
				legacyBridgeDir := filepath.Join(workspaceDir, ".belayer", "hermes_bridge")
				fmt.Printf("warn: legacy hermes_bridge location in workspace detected; new installs use %s. Remove %s to silence.\n",
					runtimeDir, legacyBridgeDir)
			})
		}
	}

	bridgeDst := filepath.Join(runtimeDir, "hermes_bridge")
	if err := os.MkdirAll(bridgeDst, 0o755); err != nil {
		return fmt.Errorf("extract bridge: mkdir %s: %w", bridgeDst, err)
	}

	// Build a set of embedded paths so we can detect and remove stale on-disk files.
	embeddedPaths := make(map[string]struct{})
	err := fs.WalkDir(belayer.DefaultBridge, "hermes_bridge", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "hermes_bridge" {
			return nil
		}
		rel := strings.TrimPrefix(path, "hermes_bridge/")
		// Skip dev-only trees (same filter as copyDefaultBridge).
		base := filepath.Base(rel)
		if d.IsDir() && (base == "__pycache__" || base == "tests") {
			return fs.SkipDir
		}
		if !d.IsDir() && (strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".md")) {
			return nil
		}
		out := filepath.Join(bridgeDst, rel)
		if d.IsDir() {
			embeddedPaths[out] = struct{}{}
			return os.MkdirAll(out, 0o755)
		}
		embeddedPaths[out] = struct{}{}

		data, err := belayer.DefaultBridge.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		// Idempotency: skip write if on-disk file matches embedded content.
		if existing, err := os.ReadFile(out); err == nil {
			if len(existing) == len(data) && sha256.Sum256(existing) == sha256.Sum256(data) {
				return nil
			}
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("extract bridge: walk embedded: %w", err)
	}

	// Remove stale on-disk files/dirs not in the embedded set. Walk bottom-up
	// (post-order) so we can remove empty directories after their children.
	type stalePath struct {
		path  string
		isDir bool
	}
	var stale []stalePath
	_ = filepath.WalkDir(bridgeDst, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == bridgeDst {
			return nil
		}
		if _, ok := embeddedPaths[path]; !ok {
			stale = append(stale, stalePath{path, d.IsDir()})
		}
		return nil
	})
	// Remove in reverse order (deepest first) so parent dirs are empty by the
	// time we try to remove them.
	for i := len(stale) - 1; i >= 0; i-- {
		if stale[i].isDir {
			_ = os.Remove(stale[i].path) // only removes empty dirs
		} else {
			_ = os.Remove(stale[i].path)
		}
	}

	return nil
}
