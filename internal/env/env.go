package env

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/envprovider"
	"github.com/donovan-yohan/belayer/internal/store"
)

// Create inserts a new environment record and returns a CreateEnvResponse.
// name is used as both the problem_id key and the env_name.
func Create(s *store.Store, cragDir, name, snapshot string) (*envprovider.CreateEnvResponse, error) {
	if err := s.InsertEnvironment(name, "belayer env", name, "{}"); err != nil {
		return nil, fmt.Errorf("inserting environment: %w", err)
	}
	return &envprovider.CreateEnvResponse{
		Status: "ok",
		Name:   name,
		Index:  0,
	}, nil
}

// AddWorktree creates a git worktree for a repo within an environment.
// envName is used as the problemID for worktree path computation.
func AddWorktree(s *store.Store, cragDir, envName, repo, branch, baseRef string) (*envprovider.AddWorktreeResponse, error) {
	path, err := crag.CreateWorktree(cragDir, envName, repo)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}
	return &envprovider.AddWorktreeResponse{
		Status: "ok",
		Repo:   repo,
		Branch: fmt.Sprintf("belayer/%s/%s", envName, repo),
		Path:   path,
	}, nil
}

// RemoveWorktree removes a git worktree for a repo within an environment.
func RemoveWorktree(cragDir, envName, repo, branch string) error {
	return crag.RemoveWorktree(cragDir, envName, repo)
}

// Destroy removes all worktrees for an environment and deletes its store record.
func Destroy(s *store.Store, cragDir, envName string) error {
	if err := crag.CleanupProblemWorktrees(cragDir, envName); err != nil {
		return fmt.Errorf("cleaning up worktrees: %w", err)
	}
	return s.DeleteEnvironment(envName)
}

// Reset returns a success response (no-op for the builtin provider).
func Reset(name, snapshot string) (*envprovider.ResetEnvResponse, error) {
	return &envprovider.ResetEnvResponse{
		Status:     "ok",
		DurationMs: 0,
		Snapshot:   snapshot,
	}, nil
}

// Status returns the current status of an environment, discovering worktrees from disk.
func Status(s *store.Store, cragDir, envName string) (*envprovider.StatusEnvResponse, error) {
	if _, err := s.GetEnvironment(envName); err != nil {
		return nil, fmt.Errorf("getting environment: %w", err)
	}

	tasksDir := filepath.Join(cragDir, "tasks", envName)
	var worktrees []envprovider.WorktreeStatusInfo
	if entries, err := os.ReadDir(tasksDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			worktrees = append(worktrees, envprovider.WorktreeStatusInfo{
				Repo:   entry.Name(),
				Branch: fmt.Sprintf("belayer/%s/%s", envName, entry.Name()),
				Path:   filepath.Join(tasksDir, entry.Name()),
				Dirty:  false,
			})
		}
	}

	return &envprovider.StatusEnvResponse{
		Status:    "ok",
		Name:      envName,
		Index:     0,
		Services:  map[string]envprovider.ServiceStatus{},
		Worktrees: worktrees,
	}, nil
}

// Logs returns empty log lines (no services in the builtin provider).
func Logs(name, service string) (*envprovider.LogsEnvResponse, error) {
	return &envprovider.LogsEnvResponse{
		Status:  "ok",
		Service: service,
		Lines:   []envprovider.LogLine{},
	}, nil
}

// List returns a summary of all environments.
func List(s *store.Store, cragDir string) (*envprovider.ListEnvsResponse, error) {
	envs, err := s.ListEnvironments()
	if err != nil {
		return nil, fmt.Errorf("listing environments: %w", err)
	}

	summaries := make([]envprovider.EnvSummary, len(envs))
	for i, e := range envs {
		var wtCount int
		tasksDir := filepath.Join(cragDir, "tasks", e.EnvName)
		if entries, err := os.ReadDir(tasksDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					wtCount++
				}
			}
		}
		summaries[i] = envprovider.EnvSummary{
			Name:          e.EnvName,
			Index:         i,
			WorktreeCount: wtCount,
			CreatedAt:     e.CreatedAt.Format(time.RFC3339),
		}
	}

	return &envprovider.ListEnvsResponse{
		Status:       "ok",
		Environments: summaries,
	}, nil
}
