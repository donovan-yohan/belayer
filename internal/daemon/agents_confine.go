package daemon

import (
	"log"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/climbpath"
)

// computeWriteRoots returns the Landlock write-root allow-list for a spawned
// agent. The allow-list is consumed by bridge.Config.WriteRoots and only
// enforced when daemon.Config.ConfineAgentWrites is true.
//
// Role rules (Landlock is allow-list only — no deny):
//
//   - supervisor: workspace top-level children (excluding .belayer/) PLUS the
//     three .belayer/ runtime sub-dirs it legitimately writes to:
//     climbs/, artifacts/, worktrees/.
//
//   - pm: climb dir only (needs to write its verification report artifact).
//
//   - branched specialist: worktree + run dir + git common dir (for git
//     metadata) + shared cache paths + /tmp.
//
//   - unbranched specialist (rare): run dir + /tmp.
//
// /tmp is always appended so compilers, package managers, and OS scratch usage
// work correctly. This is a known limitation: agents can exfiltrate data via
// /tmp, but the goal is preventing destructive writes to the workspace runtime,
// not full information isolation.
func computeWriteRoots(
	req agentSpawnRequest,
	workspace string, // original (pre-worktree) workdir — the workspace root
	worktreePath string, // empty when no branch was requested
	runDir string,
	sharedPaths []string,
) []string {
	identity := req.identityKey()

	var roots []string

	switch {
	case req.Name == "supervisor" || identity == "supervisor":
		// Supervisor needs to coordinate across the workspace but must NOT be
		// able to delete the .belayer/ runtime dirs. Enumerate top-level
		// workspace children, skipping .belayer/ entirely.
		entries, err := os.ReadDir(workspace)
		if err != nil {
			log.Printf("computeWriteRoots: ReadDir %s: %v (supervisor will be unconfined)", workspace, err)
			return nil // fall back to unconfined rather than over-restrict
		}
		for _, e := range entries {
			if e.Name() == ".belayer" {
				continue
			}
			roots = append(roots, filepath.Join(workspace, e.Name()))
		}
		// Re-add the three .belayer subdirs the supervisor must write to.
		roots = append(roots,
			climbpath.Root(workspace),
			filepath.Join(workspace, ".belayer", "artifacts"),
			filepath.Join(workspace, ".belayer", "worktrees"),
		)

	case identity == "pm":
		// PM writes only its own run dir (verification report artifact).
		roots = []string{runDir}

	case worktreePath != "":
		// Branched specialist: confined to its own worktree.
		roots = append(roots, worktreePath, runDir)
		// Git common dir (the worktrees/<name> entry under the parent .git) is
		// needed for git metadata: ORIG_HEAD, FETCH_HEAD, shallow, refs, etc.
		if gitDir := resolveGitCommonDir(worktreePath); gitDir != "" {
			roots = append(roots, gitDir)
		}
		// Shared cache paths (e.g. /workspace/.cache, pnpm store) that span
		// worktrees — injected from config.AgentSharedWritePaths.
		roots = append(roots, sharedPaths...)

	default:
		// Unbranched specialist (rare): run dir only.
		roots = []string{runDir}
	}

	// /tmp is always writable — compilers, pip, npm, and other tools write to
	// it. This is a known limitation: agents can exfiltrate content via /tmp.
	roots = append(roots, "/tmp")
	return roots
}
