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

// RepoEntry describes a repository within an instance.
type RepoEntry struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	BarePath string `json:"bare_path"` // Relative to instance dir
}

// InstanceConfig is the instance-level configuration persisted as instance.json.
type InstanceConfig struct {
	Name      string      `json:"name"`
	Repos     []RepoEntry `json:"repos"`
	CreatedAt time.Time   `json:"created_at"`
}

// Create creates a new belayer instance: directory structure, bare clones, DB, and config.
func Create(name string, repoURLs []string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("instance name cannot be empty")
	}

	// Load global config to check for conflicts
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	if _, exists := cfg.Instances[name]; exists {
		return "", fmt.Errorf("instance %q already exists", name)
	}

	// Determine instance path
	belayerDir, err := config.Dir()
	if err != nil {
		return "", err
	}
	instanceDir := filepath.Join(belayerDir, "instances", name)

	// Create directory structure
	for _, dir := range []string{
		instanceDir,
		filepath.Join(instanceDir, reposDir),
		filepath.Join(instanceDir, tasksDir),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Clone repos as bare
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

	// Initialize SQLite database
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

	// Insert instance record
	now := time.Now().UTC()
	_, err = database.Conn().Exec(
		"INSERT INTO instances (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		name, name, instanceDir, now, now,
	)
	if err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("inserting instance record: %w", err)
	}

	// Write instance.json
	instConfig := InstanceConfig{
		Name:      name,
		Repos:     repos,
		CreatedAt: now,
	}
	if err := saveInstanceConfig(instanceDir, &instConfig); err != nil {
		cleanup(instanceDir)
		return "", fmt.Errorf("saving instance config: %w", err)
	}

	// Register in global config
	cfg.Instances[name] = instanceDir
	if cfg.DefaultInstance == "" {
		cfg.DefaultInstance = name
	}
	if err := config.Save(cfg); err != nil {
		// Instance is created on disk but not registered — not fatal but warn-worthy
		return instanceDir, fmt.Errorf("saving global config (instance created at %s): %w", instanceDir, err)
	}

	return instanceDir, nil
}

// Load reads an instance's configuration from disk.
func Load(name string) (*InstanceConfig, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", fmt.Errorf("loading config: %w", err)
	}

	instanceDir, exists := cfg.Instances[name]
	if !exists {
		return nil, "", fmt.Errorf("instance %q not found", name)
	}

	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return nil, "", err
	}

	return instConfig, instanceDir, nil
}

// List returns all registered instance names and their paths.
func List() (map[string]string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg.Instances, nil
}

// Delete removes an instance's directory and its global config entry.
func Delete(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	instanceDir, exists := cfg.Instances[name]
	if !exists {
		return fmt.Errorf("instance %q not found", name)
	}

	if err := os.RemoveAll(instanceDir); err != nil {
		return fmt.Errorf("removing instance directory: %w", err)
	}

	delete(cfg.Instances, name)
	if cfg.DefaultInstance == name {
		cfg.DefaultInstance = ""
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	return nil
}

// CreateWorktree creates a git worktree for a specific repo within a task.
func CreateWorktree(instanceDir, taskID, repoName string) (string, error) {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return "", err
	}

	var entry *RepoEntry
	for i := range instConfig.Repos {
		if instConfig.Repos[i].Name == repoName {
			entry = &instConfig.Repos[i]
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("repo %q not found in instance", repoName)
	}

	bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
	worktreePath := filepath.Join(instanceDir, tasksDir, taskID, repoName)
	branch := fmt.Sprintf("belayer/%s/%s", taskID, repoName)

	if err := repo.WorktreeAdd(bareRepoDir, worktreePath, branch); err != nil {
		return "", fmt.Errorf("creating worktree: %w", err)
	}

	return worktreePath, nil
}

// RemoveWorktree removes a git worktree for a specific repo within a task.
func RemoveWorktree(instanceDir, taskID, repoName string) error {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return err
	}

	var entry *RepoEntry
	for i := range instConfig.Repos {
		if instConfig.Repos[i].Name == repoName {
			entry = &instConfig.Repos[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("repo %q not found in instance", repoName)
	}

	bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
	worktreePath := filepath.Join(instanceDir, tasksDir, taskID, repoName)

	return repo.WorktreeRemove(bareRepoDir, worktreePath)
}

// CleanupTaskWorktrees removes all worktrees for a given task.
func CleanupTaskWorktrees(instanceDir, taskID string) error {
	instConfig, err := loadInstanceConfig(instanceDir)
	if err != nil {
		return err
	}

	var errs []error
	for _, entry := range instConfig.Repos {
		worktreePath := filepath.Join(instanceDir, tasksDir, taskID, entry.Name)
		if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
			continue
		}
		bareRepoDir := filepath.Join(instanceDir, entry.BarePath)
		if err := repo.WorktreeRemove(bareRepoDir, worktreePath); err != nil {
			errs = append(errs, fmt.Errorf("removing worktree for %s: %w", entry.Name, err))
		}
	}

	// Remove the task directory if empty
	taskDir := filepath.Join(instanceDir, tasksDir, taskID)
	os.Remove(taskDir) // Best-effort; may fail if not empty

	if len(errs) > 0 {
		return fmt.Errorf("errors cleaning up worktrees: %v", errs)
	}
	return nil
}

func saveInstanceConfig(instanceDir string, cfg *InstanceConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling instance config: %w", err)
	}
	return os.WriteFile(filepath.Join(instanceDir, instanceConfigFile), data, 0644)
}

func loadInstanceConfig(instanceDir string) (*InstanceConfig, error) {
	data, err := os.ReadFile(filepath.Join(instanceDir, instanceConfigFile))
	if err != nil {
		return nil, fmt.Errorf("reading instance config: %w", err)
	}

	var cfg InstanceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing instance config: %w", err)
	}
	return &cfg, nil
}

func cleanup(dir string) {
	os.RemoveAll(dir)
}
