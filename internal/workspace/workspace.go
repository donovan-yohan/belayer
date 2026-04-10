package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoConfig describes a single repository in the workspace.
type RepoConfig struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Path          string `json:"path"`           // local path override
	Branch        string `json:"branch"`          // working branch
	DefaultBranch string `json:"default_branch"`  // e.g. "main" or "master"
}

// WorkspaceConfig is the top-level structure parsed from repos.json.
type WorkspaceConfig struct {
	Repos   []RepoConfig `json:"repos"`
	BaseDir string       `json:"base_dir"` // workspace root directory
}

// Workspace manages a collection of repositories under a common base directory.
type Workspace struct {
	config WorkspaceConfig
	dir    string // resolved base directory
}

// Load reads and parses a repos.json config file at configPath and returns a
// ready-to-use Workspace. The base directory is resolved relative to the
// config file's parent directory when it is not an absolute path.
func Load(configPath string) (*Workspace, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: load config %q: %w", configPath, err)
	}

	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: parse config %q: %w", configPath, err)
	}

	// Resolve base_dir: if empty, use the directory containing the config file.
	baseDir := cfg.BaseDir
	if baseDir == "" {
		baseDir = filepath.Dir(configPath)
	} else if !filepath.IsAbs(baseDir) {
		baseDir = filepath.Join(filepath.Dir(configPath), baseDir)
	}

	return &Workspace{
		config: cfg,
		dir:    baseDir,
	}, nil
}

// Dir returns the resolved workspace root directory.
func (w *Workspace) Dir() string {
	return w.dir
}

// Repos returns all configured repos.
func (w *Workspace) Repos() []RepoConfig {
	return w.config.Repos
}

// RepoPath returns the local filesystem path for the named repo. Returns an
// error if no repo with that name exists in the config.
//
// Path resolution order:
//  1. If RepoConfig.Path is set and absolute, use it as-is.
//  2. If RepoConfig.Path is set but relative, resolve it relative to the
//     workspace base directory.
//  3. If RepoConfig.Path is empty, derive the path as <base_dir>/<name>.
func (w *Workspace) RepoPath(repoName string) (string, error) {
	repo, ok := w.findRepo(repoName)
	if !ok {
		return "", fmt.Errorf("workspace: repo %q not found in config", repoName)
	}
	return w.resolvePath(repo), nil
}

// Clone clones the named repo into its resolved local path if it is not
// already present. If the target directory already exists it is assumed to be
// a valid clone and no action is taken.
func (w *Workspace) Clone(repoName string) error {
	repo, ok := w.findRepo(repoName)
	if !ok {
		return fmt.Errorf("workspace: clone: repo %q not found in config", repoName)
	}
	if repo.URL == "" {
		return fmt.Errorf("workspace: clone: repo %q has no URL configured", repoName)
	}

	dest := w.resolvePath(repo)

	// If the destination already exists, skip cloning.
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	cmd := exec.Command("git", "clone", repo.URL, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("workspace: clone %q: %w", repoName, err)
	}
	return nil
}

// EnsureReady validates that all configured repos are cloned (their local
// paths exist). Returns an error that lists every missing repo.
func (w *Workspace) EnsureReady() error {
	var missing []string
	for _, repo := range w.config.Repos {
		dest := w.resolvePath(repo)
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			missing = append(missing, fmt.Sprintf("%s (%s)", repo.Name, dest))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("workspace: repos not cloned: %s", strings.Join(missing, ", "))
	}
	return nil
}

// findRepo looks up a repo by name and reports whether it was found.
func (w *Workspace) findRepo(name string) (RepoConfig, bool) {
	for _, r := range w.config.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return RepoConfig{}, false
}

// resolvePath returns the local filesystem path for a repo following the
// three-step resolution described in RepoPath.
func (w *Workspace) resolvePath(repo RepoConfig) string {
	if repo.Path != "" {
		if filepath.IsAbs(repo.Path) {
			return repo.Path
		}
		return filepath.Join(w.dir, repo.Path)
	}
	return filepath.Join(w.dir, repo.Name)
}
