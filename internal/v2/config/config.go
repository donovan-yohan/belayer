// Package config manages belayer's global configuration at ~/.belayer/config.json.
// Stores the repo registry (name → path) and crag registry (name → path).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config is the global belayer configuration.
type Config struct {
	Repos map[string]RepoEntry `json:"repos"`
	Crags map[string]string    `json:"crags"` // name → directory path
}

// RepoEntry holds metadata for a registered repository.
type RepoEntry struct {
	Path string `json:"path"`
}

// DefaultConfig returns an empty config with initialized maps.
func DefaultConfig() *Config {
	return &Config{
		Repos: make(map[string]RepoEntry),
		Crags: make(map[string]string),
	}
}

// configPath returns the path to ~/.belayer/config.json.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".belayer", "config.json"), nil
}

// Load reads the global config from disk. Returns a default if the file doesn't exist.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Repos == nil {
		cfg.Repos = make(map[string]RepoEntry)
	}
	if cfg.Crags == nil {
		cfg.Crags = make(map[string]string)
	}
	return &cfg, nil
}

// Save writes the config to disk using atomic write (temp file + rename).
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// AddRepo registers a repo by name with a validated absolute path.
func (c *Config) AddRepo(name, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if err := validateRepoPath(absPath); err != nil {
		return fmt.Errorf("repo %q: %w", name, err)
	}
	c.Repos[name] = RepoEntry{Path: absPath}
	return nil
}

// RemoveRepo removes a repo from the registry.
func (c *Config) RemoveRepo(name string) error {
	if _, exists := c.Repos[name]; !exists {
		return fmt.Errorf("repo %q not registered", name)
	}
	delete(c.Repos, name)
	return nil
}

// ResolveRepoPath returns the absolute path for a registered repo name.
func (c *Config) ResolveRepoPath(name string) (string, error) {
	entry, exists := c.Repos[name]
	if !exists {
		return "", fmt.Errorf("repo %q not registered. Run: belayer repo add %s <path>", name, name)
	}
	return entry.Path, nil
}

// ResolveRepoPaths resolves multiple repo names to their paths.
func (c *Config) ResolveRepoPaths(names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	for _, name := range names {
		path, err := c.ResolveRepoPath(name)
		if err != nil {
			return nil, err
		}
		result[name] = path
	}
	return result, nil
}

// ValidateRepoPaths checks that all named repos have valid paths (exist + are git repos).
func (c *Config) ValidateRepoPaths(names []string) error {
	var errs []string
	for _, name := range names {
		path, err := c.ResolveRepoPath(name)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := validateRepoPath(path); err != nil {
			errs = append(errs, fmt.Sprintf("repo %q (%s): %s", name, path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("repo validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// DetectRepoPath attempts to find a repo by name in common locations.
// Checks: CWD siblings (../<name>), ~/projects/<name>.
func DetectRepoPath(name string) (string, error) {
	// Check CWD sibling.
	cwd, _ := os.Getwd()
	if cwd != "" {
		sibling := filepath.Join(filepath.Dir(cwd), name)
		if isGitRepo(sibling) {
			return sibling, nil
		}
	}

	// Check ~/projects/<name>.
	home, _ := os.UserHomeDir()
	if home != "" {
		projects := filepath.Join(home, "projects", name)
		if isGitRepo(projects) {
			return projects, nil
		}
		// Also check ~/Documents/Programs/personal/<name> (common macOS path).
		personal := filepath.Join(home, "Documents", "Programs", "personal", name)
		if isGitRepo(personal) {
			return personal, nil
		}
	}

	return "", fmt.Errorf("could not auto-detect path for %q. Specify explicitly: belayer repo add %s /path/to/repo", name, name)
}

// AddCrag registers a named crag at the given directory path.
func (c *Config) AddCrag(name, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("crag path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("crag path %q is not a directory", absPath)
	}
	c.Crags[name] = absPath
	return nil
}

// ResolveCragPath returns the directory path for a named crag.
func (c *Config) ResolveCragPath(name string) (string, error) {
	path, exists := c.Crags[name]
	if !exists {
		return "", fmt.Errorf("crag %q not registered. Run: belayer crag init --name %s", name, name)
	}
	return path, nil
}

func validateRepoPath(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	if !isGitRepo(path) {
		return fmt.Errorf("path is not a git repository: %s", path)
	}
	return nil
}

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}
