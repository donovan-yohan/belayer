// Package worktree manages per-run git worktrees for multi-repo pipeline isolation.
// Each pipeline run gets isolated worktrees so concurrent runs don't conflict.
// Code is always pushed to remote before worktree cleanup — no silent work loss.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles git worktree lifecycle for pipeline runs.
type Manager struct {
	BaseDir string // Base directory for worktrees (typically .belayer/worktrees/)
}

// NewManager creates a worktree manager rooted at the given base directory.
func NewManager(baseDir string) *Manager {
	return &Manager{BaseDir: baseDir}
}

// SetupRunWorktrees creates git worktrees for all repos in a pipeline run.
// Returns a map of repoName → worktreePath.
func (m *Manager) SetupRunWorktrees(runID string, repos map[string]string) (map[string]string, error) {
	runDir := filepath.Join(m.BaseDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	worktrees := make(map[string]string, len(repos))
	for repoName, repoPath := range repos {
		wtPath := filepath.Join(runDir, repoName)
		branch := fmt.Sprintf("belayer/%s", runID)

		if err := CreateWorktree(repoPath, wtPath, branch); err != nil {
			// Cleanup already-created worktrees on failure.
			for _, cleanPath := range worktrees {
				RemoveWorktree(cleanPath)
			}
			return nil, fmt.Errorf("create worktree for %s: %w", repoName, err)
		}
		worktrees[repoName] = wtPath
	}
	return worktrees, nil
}

// CleanupRunWorktrees pushes branches and removes worktrees for a completed run.
// Returns warnings for any worktrees that were preserved due to unpushed commits.
func (m *Manager) CleanupRunWorktrees(runID string, repos map[string]string) []string {
	var warnings []string
	runDir := filepath.Join(m.BaseDir, runID)

	for repoName := range repos {
		wtPath := filepath.Join(runDir, repoName)
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			continue
		}

		warning := SafeCleanup(wtPath, runID)
		if warning != "" {
			warnings = append(warnings, fmt.Sprintf("%s: %s", repoName, warning))
		}
	}

	// Remove run directory if empty.
	entries, _ := os.ReadDir(runDir)
	if len(entries) == 0 {
		os.Remove(runDir)
	}

	return warnings
}

// CreateWorktree creates a git worktree at worktreePath from the given repoPath.
func CreateWorktree(repoPath, worktreePath, branchName string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, "-b", branchName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "worktree", "remove", worktreePath, "--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: just remove the directory.
		if rmErr := os.RemoveAll(worktreePath); rmErr != nil {
			return fmt.Errorf("git worktree remove failed (%s), manual rm also failed: %w", strings.TrimSpace(string(out)), rmErr)
		}
	}
	return nil
}

// HasUnpushedCommits checks if the worktree has commits not on the remote.
func HasUnpushedCommits(worktreePath string) (bool, error) {
	// Check if there are any commits at all.
	cmd := exec.Command("git", "-C", worktreePath, "log", "--oneline", "-1")
	if err := cmd.Run(); err != nil {
		return false, nil // No commits = nothing unpushed.
	}

	// Check for commits not on any remote tracking branch.
	cmd = exec.Command("git", "-C", worktreePath, "log", "--oneline", "@{upstream}..HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// No upstream = all commits are unpushed.
		cmd2 := exec.Command("git", "-C", worktreePath, "log", "--oneline", "-1")
		out2, _ := cmd2.CombinedOutput()
		return len(strings.TrimSpace(string(out2))) > 0, nil
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// PushBranch pushes the current branch to the remote.
func PushBranch(worktreePath, remote, branch string) error {
	cmd := exec.Command("git", "-C", worktreePath, "push", remote, branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// SafeCleanup pushes the branch if needed, then removes the worktree.
// Returns a warning string if the worktree was preserved (unpushed + push failed).
func SafeCleanup(worktreePath, runID string) string {
	branch := fmt.Sprintf("belayer/%s", runID)

	unpushed, err := HasUnpushedCommits(worktreePath)
	if err != nil {
		return fmt.Sprintf("could not check unpushed commits: %v — worktree preserved at %s", err, worktreePath)
	}

	if unpushed {
		// Try to push.
		if err := PushBranch(worktreePath, "origin", branch); err != nil {
			return fmt.Sprintf("has unpushed commits and push failed: %v — worktree preserved at %s", err, worktreePath)
		}
	}

	// Safe to remove.
	RemoveWorktree(worktreePath)
	return ""
}
