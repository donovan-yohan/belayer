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
