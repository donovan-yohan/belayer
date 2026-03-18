package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	marketplaceName  = "belayer"
	marketplacesFile = "known_marketplaces.json"
	installedFile    = "installed_plugins.json"

	// Plugin versions — keep in sync with plugins/*/.claude-plugin/plugin.json
	HarnessVersion = "2.1.0"
	PRVersion      = "1.2.0"
)

// Registry reads and writes Claude Code's plugin registry files.
// baseDir is the Claude Code config directory (typically ~/.claude).
// Not safe for concurrent use — callers must ensure only one process writes at a time.
type Registry struct {
	baseDir string
}

// NewRegistry creates a Registry that operates on the given Claude Code config directory.
func NewRegistry(baseDir string) *Registry {
	return &Registry{baseDir: baseDir}
}

// RegisterMarketplace adds the belayer GitHub marketplace to known_marketplaces.json.
// Idempotent: skips if already registered.
func (r *Registry) RegisterMarketplace(repo string) error {
	path := filepath.Join(r.baseDir, "plugins", marketplacesFile)
	existing, err := readJSONMap(path)
	if err != nil {
		return err
	}

	if _, ok := existing[marketplaceName]; ok {
		return nil // already registered
	}

	entry := map[string]any{
		"source": map[string]string{
			"source": "github",
			"repo":   repo,
		},
		"installLocation": filepath.Join(r.baseDir, "plugins", "marketplaces", marketplaceName),
		"lastUpdated":     time.Now().UTC().Format(time.RFC3339Nano),
		"autoUpdate":      true,
	}

	existing[marketplaceName] = entry
	return writeJSONMap(path, existing)
}

// InstallPlugin adds a plugin entry to installed_plugins.json.
// Idempotent: skips if already installed under the belayer marketplace.
func (r *Registry) InstallPlugin(name, version string) error {
	path := filepath.Join(r.baseDir, "plugins", installedFile)
	existing, err := readJSONMap(path)
	if err != nil {
		return err
	}

	key := name + "@" + marketplaceName
	if _, ok := existing[key]; ok {
		return nil // already installed
	}

	entry := []map[string]any{{
		"scope":       "user",
		"installPath": filepath.Join(r.baseDir, "plugins", "cache", marketplaceName, name, version),
		"version":     version,
		"installedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"lastUpdated": time.Now().UTC().Format(time.RFC3339Nano),
	}}

	existing[key] = entry
	return writeJSONMap(path, existing)
}

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return m, nil
}

func writeJSONMap(path string, m map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: write to temp file then rename to avoid corruption on interrupt.
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op if rename succeeded
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	return os.Rename(tmpName, path)
}
