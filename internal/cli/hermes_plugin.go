package cli

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	belayer "github.com/donovan-yohan/belayer"
)

// resolveHermesHome returns the Hermes home directory where plugins and
// config.yaml live. Matches get_hermes_home() in hermes_constants.py:
// reads $HERMES_HOME if set, otherwise falls back to ~/.hermes.
//
// Errors only when $HOME is unavailable and $HERMES_HOME is unset — a
// genuinely broken environment that no fallback can paper over.
func resolveHermesHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("HERMES_HOME")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve hermes home: $HOME unavailable: %w", err)
	}
	return filepath.Join(home, ".hermes"), nil
}

// extractPluginsToHermesHome mirrors extractBridgeToRuntimeDir but for the
// Hermes 0.11 plugin tree. The Hermes PluginManager discovers plugins at
// $HERMES_HOME/plugins/<name>/, so we extract there.
//
// Idempotent (SHA-256 match skips writes), strips __pycache__ / *.pyc /
// *.md / tests/. Removes stale files no longer in the embed, so a plugin
// renamed between belayer versions doesn't leave dangling modules.
func extractPluginsToHermesHome(hermesHome string) error {
	pluginsRoot := filepath.Join(hermesHome, "plugins")
	if err := os.MkdirAll(pluginsRoot, 0o755); err != nil {
		return fmt.Errorf("extract plugins: mkdir %s: %w", pluginsRoot, err)
	}

	// For each top-level plugin dir under the embed, manage only that dir's
	// subtree — we must not nuke unrelated user-installed plugins at this
	// level. Each plugin we own gets a full idempotent sync.
	entries, err := belayer.DefaultPlugins.ReadDir("plugins")
	if err != nil {
		return fmt.Errorf("extract plugins: read embedded root: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginName := entry.Name()
		if err := syncEmbeddedPlugin(pluginsRoot, pluginName); err != nil {
			return err
		}
	}

	return nil
}

// syncEmbeddedPlugin writes one embedded plugin subtree into
// $HERMES_HOME/plugins/<name>/, idempotently, and removes stale files
// within that plugin's directory only. Leaves other plugins alone.
func syncEmbeddedPlugin(pluginsRoot, pluginName string) error {
	embeddedRoot := filepath.Join("plugins", pluginName)
	dstRoot := filepath.Join(pluginsRoot, pluginName)
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return fmt.Errorf("extract plugin %s: mkdir: %w", pluginName, err)
	}

	embeddedPaths := make(map[string]struct{})
	err := fs.WalkDir(belayer.DefaultPlugins, embeddedRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == embeddedRoot {
			return nil
		}
		rel := strings.TrimPrefix(path, embeddedRoot+"/")
		base := filepath.Base(rel)
		if d.IsDir() && (base == "__pycache__" || base == "tests") {
			return fs.SkipDir
		}
		if !d.IsDir() && (strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".md")) {
			return nil
		}
		out := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			embeddedPaths[out] = struct{}{}
			return os.MkdirAll(out, 0o755)
		}
		embeddedPaths[out] = struct{}{}

		data, err := belayer.DefaultPlugins.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		if existing, err := os.ReadFile(out); err == nil {
			if len(existing) == len(data) && sha256.Sum256(existing) == sha256.Sum256(data) {
				return nil
			}
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("extract plugin %s: walk: %w", pluginName, err)
	}

	// Prune stale files within this plugin's dir only.
	type stalePath struct {
		path  string
		isDir bool
	}
	var stale []stalePath
	_ = filepath.WalkDir(dstRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == dstRoot {
			return nil
		}
		if _, ok := embeddedPaths[path]; !ok {
			stale = append(stale, stalePath{path, d.IsDir()})
		}
		return nil
	})
	for i := len(stale) - 1; i >= 0; i-- {
		_ = os.Remove(stale[i].path)
	}

	return nil
}

// ensureHermesPluginEnabled adds pluginName to the plugins.enabled list in
// $HERMES_HOME/config.yaml if not already present. Idempotent.
//
// The edit is line-preserving — we don't round-trip through a YAML library
// that would rewrite comments or reorder keys. Strategy:
//
//  1. No config.yaml → write a minimal one with just plugins.enabled.
//  2. config.yaml exists but no plugins: section → append the block.
//  3. plugins: section exists without enabled: → inject enabled: under it.
//  4. enabled: list exists → append our plugin if missing.
//
// Returns (changed, error). The caller can log "enabled X" when changed=true.
func ensureHermesPluginEnabled(hermesHome, pluginName string) (bool, error) {
	cfgPath := filepath.Join(hermesHome, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(hermesHome, 0o755); mkErr != nil {
			return false, fmt.Errorf("ensure hermes home dir: %w", mkErr)
		}
		content := fmt.Sprintf("plugins:\n  enabled:\n    - %s\n", pluginName)
		if wErr := os.WriteFile(cfgPath, []byte(content), 0o644); wErr != nil {
			return false, fmt.Errorf("write %s: %w", cfgPath, wErr)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", cfgPath, err)
	}

	lines := strings.Split(string(data), "\n")
	newLines, changed := injectEnabledPlugin(lines, pluginName)
	if !changed {
		return false, nil
	}

	out := strings.Join(newLines, "\n")
	if err := os.WriteFile(cfgPath, []byte(out), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", cfgPath, err)
	}
	return true, nil
}

// injectEnabledPlugin is the pure-function core of ensureHermesPluginEnabled,
// split out so it can be unit-tested without hitting the filesystem.
//
// Returns the possibly-modified lines and whether a change was made. The
// caller is responsible for writing back.
func injectEnabledPlugin(lines []string, pluginName string) ([]string, bool) {
	pluginsIdx := -1
	pluginsIndent := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "plugins:" || strings.HasPrefix(trimmed, "plugins:") {
			// Top-level "plugins:" only — avoid matching nested "plugins:" keys.
			if strings.HasPrefix(line, "plugins:") {
				pluginsIdx = i
				pluginsIndent = ""
				break
			}
		}
	}

	if pluginsIdx == -1 {
		// Case 2: no plugins section. Append block.
		addition := []string{"plugins:", "  enabled:", "    - " + pluginName}
		// If the file doesn't end with a newline, don't drop content.
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			addition = append([]string{""}, addition...)
		}
		return append(lines, addition...), true
	}

	// Find the enabled: sub-key within the plugins: block.
	enabledIdx := -1
	enabledIndent := ""
	childIndent := pluginsIndent + "  "
	// Find the end of the plugins: block (next line at pluginsIndent level
	// that isn't blank).
	blockEnd := len(lines)
	for i := pluginsIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if len(leading) <= len(pluginsIndent) {
			blockEnd = i
			break
		}
		if leading == childIndent && strings.HasPrefix(strings.TrimLeft(line, " \t"), "enabled:") {
			enabledIdx = i
			enabledIndent = leading
		}
	}

	if enabledIdx == -1 {
		// Case 3: plugins: block exists but no enabled: sub-key. Insert at top of block.
		insert := []string{
			childIndent + "enabled:",
			childIndent + "  - " + pluginName,
		}
		out := append([]string{}, lines[:pluginsIdx+1]...)
		out = append(out, insert...)
		out = append(out, lines[pluginsIdx+1:]...)
		return out, true
	}

	// Case 4: enabled: list exists. Scan list items under it, check for name.
	listIndent := enabledIndent + "  "
	listEnd := blockEnd
	for i := enabledIdx + 1; i < blockEnd; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !strings.HasPrefix(leading, listIndent) {
			listEnd = i
			break
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			item = strings.Trim(item, `"'`)
			if item == pluginName {
				return lines, false // already enabled
			}
		}
	}

	// Inline list ("enabled: [a, b]") — bail on the safe side.
	if strings.Contains(lines[enabledIdx], "[") {
		// Punt: keep the inline form, but append a block-style entry after.
		// A YAML parser would reject mixed syntax, so instead we transform
		// "enabled: [a, b]" to a block list including pluginName.
		raw := lines[enabledIdx]
		prefix := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
		// Parse items inside brackets.
		lb := strings.Index(raw, "[")
		rb := strings.LastIndex(raw, "]")
		var items []string
		if lb >= 0 && rb > lb {
			inner := raw[lb+1 : rb]
			for _, it := range strings.Split(inner, ",") {
				it = strings.TrimSpace(it)
				it = strings.Trim(it, `"'`)
				if it != "" {
					items = append(items, it)
				}
			}
		}
		for _, it := range items {
			if it == pluginName {
				return lines, false
			}
		}
		items = append(items, pluginName)
		replacement := []string{prefix + "enabled:"}
		for _, it := range items {
			replacement = append(replacement, prefix+"  - "+it)
		}
		out := append([]string{}, lines[:enabledIdx]...)
		out = append(out, replacement...)
		out = append(out, lines[enabledIdx+1:]...)
		return out, true
	}

	// Block list — insert our item at listEnd.
	newItem := listIndent + "- " + pluginName
	out := append([]string{}, lines[:listEnd]...)
	out = append(out, newItem)
	out = append(out, lines[listEnd:]...)
	return out, true
}
