package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/repo"
)

const (
	instanceConfigFile = "instance.json"
	reposDir           = "repos"
	tasksDir           = "tasks"
	dbFile             = "belayer.db"
)

// RepoEntry describes a repository within a crag.
type RepoEntry struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	BarePath string `json:"bare_path"` // Relative to crag dir
}

// CragConfig is the crag-level configuration persisted as instance.json.
type CragConfig struct {
	Name      string      `json:"name"`
	Repos     []RepoEntry `json:"repos"`
	CreatedAt time.Time   `json:"created_at"`
}

// Create creates a new belayer crag: directory structure, bare clones, DB, and config.
func Create(name string, repoURLs []string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("crag name cannot be empty")
	}

	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	if _, exists := cfg.Crags[name]; exists {
		return "", fmt.Errorf("crag %q already exists", name)
	}

	belayerDir, err := config.Dir()
	if err != nil {
		return "", err
	}
	instanceDir := filepath.Join(belayerDir, "crags", name)

	for _, dir := range []string{
		instanceDir,
		filepath.Join(instanceDir, reposDir),
		filepath.Join(instanceDir, tasksDir),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	var repos []RepoEntry
	for _, repoURL := range repoURLs {
		repoName, err := repo.RepoNameFromURL(repoURL)
		if err != nil {
			cleanup(instanceDir)
			return "", fmt.Errorf("extracting repo name from %q: %w", repoURL, err)
		}

		barePath := filepath.Join(reposDir, repoName+".git")
		fullBarePath := filepath.Join(instanceDir, barePath)

		if err := repo.CloneBare(repoURL, fullBarePath); err != nil {
			cleanup(instanceDir)
			return "", fmt.Errorf("cloning %s: %w", repoURL, err)
		}

		repos = append(repos, RepoEntry{
			Name:     repoName,
			URL:      repoURL,
			BarePath: barePath,
		})
	}

	database, err := db.Open(filepath.Join(instanceDir, dbFile))
	if err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(); err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("running migrations: %w", err)
	}

	now := time.Now().UTC()
	_, err = database.Conn().Exec(
		"INSERT INTO crags (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		name, name, instanceDir, now, now,
	)
	if err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("inserting crag record: %w", err)
	}

	instConfig := CragConfig{
		Name:      name,
		Repos:     repos,
		CreatedAt: now,
	}
	if err := saveInstanceConfig(instanceDir, &instConfig); err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("saving crag config: %w", err)
	}

	cfg.Crags[name] = instanceDir
	if cfg.DefaultCrag == "" {
		cfg.DefaultCrag = name
	}
	if err := config.Save(cfg); err != nil {
		// Crag is created on disk but not registered — not fatal but warn-worthy
		return instanceDir, fmt.Errorf("saving global config (crag created at %s): %w", instanceDir, err)
	}

	return instanceDir, nil
}

// Load reads a crag's configuration from disk.
func Load(name string) (*CragConfig, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", fmt.Errorf("loading config: %w", err)
	}

	instanceDir, exists := cfg.Crags[name]
	if !exists {
		return nil, "", fmt.Errorf("crag %q not found", name)
	}

	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return nil, "", err
	}

	return instConfig, instanceDir, nil
}

// List returns all registered crag names and their paths.
func List() (map[string]string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg.Crags, nil
}

// Delete removes a crag's directory and its global config entry.
func Delete(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	instanceDir, exists := cfg.Crags[name]
	if !exists {
		return fmt.Errorf("crag %q not found", name)
	}

	if err := os.RemoveAll(instanceDir); err != nil {
		return fmt.Errorf("removing crag directory: %w", err)
	}

	delete(cfg.Crags, name)
	if cfg.DefaultCrag == name {
		cfg.DefaultCrag = ""
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	return nil
}

// findRepoEntry looks up a repo by name in the crag config.
func findRepoEntry(instConfig *CragConfig, repoName string) (*RepoEntry, error) {
	for i := range instConfig.Repos {
		if instConfig.Repos[i].Name == repoName {
			return &instConfig.Repos[i], nil
		}
	}
	return nil, fmt.Errorf("repo %q not found in crag", repoName)
}

// CreateWorktree creates a git worktree for a specific repo within a problem.
func CreateWorktree(instanceDir, problemID, repoName string) (string, error) {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return "", err
	}

	entry, err := findRepoEntry(instConfig, repoName)
	if err != nil {
		return "", err
	}

	bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
	worktreePath := filepath.Join(instanceDir, tasksDir, problemID, repoName)
	branch := fmt.Sprintf("belayer/%s/%s", problemID, repoName)

	if err := repo.WorktreeAdd(bareRepoDir, worktreePath, branch); err != nil {
		return "", fmt.Errorf("creating worktree: %w", err)
	}

	return worktreePath, nil
}

// RemoveWorktree removes a git worktree for a specific repo within a problem.
func RemoveWorktree(instanceDir, problemID, repoName string) error {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return err
	}

	entry, err := findRepoEntry(instConfig, repoName)
	if err != nil {
		return err
	}

	bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
	worktreePath := filepath.Join(instanceDir, tasksDir, problemID, repoName)

	return repo.WorktreeRemove(bareRepoDir, worktreePath)
}

// CleanupProblemWorktrees removes all worktrees for a given problem.
func CleanupProblemWorktrees(instanceDir, problemID string) error {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return err
	}

	var errs []error
	for _, entry := range instConfig.Repos {
		worktreePath := filepath.Join(instanceDir, tasksDir, problemID, entry.Name)
		if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
			continue
		}
		bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
		if err := repo.WorktreeRemove(bareRepoDir, worktreePath); err != nil {
			errs = append(errs, fmt.Errorf("removing worktree for %s: %w", entry.Name, err))
		}
	}

	taskDir := filepath.Join(instanceDir, tasksDir, problemID)
	os.Remove(taskDir) // Best-effort; may fail if not empty

	if len(errs) > 0 {
		return fmt.Errorf("errors cleaning up worktrees: %v", errs)
	}
	return nil
}

func saveInstanceConfig(instanceDir string, cfg *CragConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling crag config: %w", err)
	}
	return os.WriteFile(filepath.Join(instanceDir, instanceConfigFile), data, 0644)
}

func loadInstanceConfig(instanceDir string) (*CragConfig, error) {
	data, err := os.ReadFile(filepath.Join(instanceDir, instanceConfigFile))
	if err != nil {
		return nil, fmt.Errorf("reading crag config: %w", err)
	}

	var cfg CragConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing crag config: %w", err)
	}
	return &cfg, nil
}

func cleanup(dir string) {
	os.RemoveAll(dir)
}
