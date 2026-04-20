package daemon

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// resolveGitCommonDir returns the git common directory for a git worktree.
// For a linked worktree, <worktree>/.git is a file containing a line like:
//
//	gitdir: /path/to/parent/.git/worktrees/<name>
//
// This function reads that file and returns the resolved path. Callers add
// write permission to this directory so the agent can update git metadata
// (ORIG_HEAD, FETCH_HEAD, refs, objects) without writing into the main repo
// working tree.
//
// If <worktree>/.git is a directory (i.e. this is the main repo, not a
// worktree), resolveGitCommonDir returns empty string. Any I/O error also
// returns empty string and logs at debug level.
func resolveGitCommonDir(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	gitPath := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		log.Printf("gitcommon: stat %s: %v", gitPath, err)
		return ""
	}
	if info.IsDir() {
		// Main repo checkout — not a linked worktree; no separate common dir needed.
		return ""
	}
	// It's a file: read the gitdir pointer.
	data, err := os.ReadFile(gitPath)
	if err != nil {
		log.Printf("gitcommon: read %s: %v", gitPath, err)
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		log.Printf("gitcommon: %s: unexpected format %q", gitPath, line)
		return ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktreePath, gitdir)
	}
	return gitdir
}
