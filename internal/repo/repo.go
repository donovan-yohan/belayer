package repo

import (
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoNameFromURL extracts a repository name from a git URL.
// Handles HTTPS (https://github.com/org/repo.git) and SSH (git@github.com:org/repo.git) formats.
func RepoNameFromURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty URL")
	}

	var path string

	// SSH format: git@host:org/repo.git
	if strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		parts := strings.SplitN(rawURL, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid SSH URL: %s", rawURL)
		}
		path = parts[1]
	} else {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
		}
		path = u.Path
	}

	// Strip trailing slash, then .git suffix
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")
	name := filepath.Base(path)
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("cannot extract repo name from %q", rawURL)
	}

	return name, nil
}

// CloneBare clones a git repository as a bare repo into the target directory.
// targetDir should end in .git by convention (e.g., repos/my-repo.git).
func CloneBare(repoURL, targetDir string) error {
	cmd := exec.Command("git", "clone", "--bare", repoURL, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone --bare %s: %s: %w", repoURL, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// WorktreeAdd creates a new git worktree from a bare repo.
// bareRepoDir is the path to the bare clone.
// worktreePath is the target directory for the worktree.
// branch is the branch name to create (e.g., belayer/<task-id>/<repo-name>).
func WorktreeAdd(bareRepoDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "-C", bareRepoDir, "worktree", "add", "-b", branch, worktreePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// WorktreeRemove removes a git worktree and prunes stale entries.
func WorktreeRemove(bareRepoDir, worktreePath string) error {
	cmd := exec.Command("git", "-C", bareRepoDir, "worktree", "remove", "--force", worktreePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(output)), err)
	}

	// Prune stale worktree entries
	pruneCmd := exec.Command("git", "-C", bareRepoDir, "worktree", "prune")
	if pruneOutput, pruneErr := pruneCmd.CombinedOutput(); pruneErr != nil {
		return fmt.Errorf("git worktree prune: %s: %w", strings.TrimSpace(string(pruneOutput)), pruneErr)
	}

	return nil
}

// WorktreeDiff returns the git diff for changes on the current branch relative to the default branch.
// Uses three-dot diff (main...HEAD) to capture only branch-specific changes.
func WorktreeDiff(worktreePath string) (string, error) {
	// Determine the default branch name
	baseBranch := detectBaseBranch(worktreePath)

	cmd := exec.Command("git", "-C", worktreePath, "diff", baseBranch+"...HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff %s...HEAD in %s: %s: %w", baseBranch, worktreePath, strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

// WorktreeDiffStat returns a summary of changes (files changed, insertions, deletions).
func WorktreeDiffStat(worktreePath string) (string, error) {
	baseBranch := detectBaseBranch(worktreePath)

	cmd := exec.Command("git", "-C", worktreePath, "diff", "--stat", baseBranch+"...HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff --stat %s...HEAD in %s: %s: %w", baseBranch, worktreePath, strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

// detectBaseBranch determines the default branch name (main, master, etc.).
func detectBaseBranch(worktreePath string) string {
	// Try to get the remote HEAD reference
	cmd := exec.Command("git", "-C", worktreePath, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.CombinedOutput()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		// refs/remotes/origin/main -> main
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Fallback: check if main or master exists
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", branch)
		if err := cmd.Run(); err == nil {
			return branch
		}
	}

	return "main"
}

// PushBranch pushes the current branch to the origin remote.
func PushBranch(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "push", "-u", "origin", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push in %s: %s: %w", worktreePath, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// CreatePR creates a pull request using the GitHub CLI and returns the PR URL.
func CreatePR(worktreePath, title, body string) (string, error) {
	cmd := exec.Command("gh", "pr", "create", "--title", title, "--body", body)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create in %s: %s: %w", worktreePath, strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// WorktreeList lists all worktrees for a bare repo.
func WorktreeList(bareRepoDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", bareRepoDir, "worktree", "list", "--porcelain")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %s: %w", strings.TrimSpace(string(output)), err)
	}

	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths, nil
}
