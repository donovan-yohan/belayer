package intake

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

// GenerateWorkflowID returns a stable workflow ID in the form
// "{pipelineName}/{source}/{externalID}".
func GenerateWorkflowID(pipelineName, source, externalID string) string {
	return fmt.Sprintf("%s/%s/%s", pipelineName, source, externalID)
}

// ResolvePipelineYAML returns pipeline YAML bytes and a name by checking known locations.
// Returns an error if no pipeline file is found (run 'belayer setup --framework' first).
func ResolvePipelineYAML(cwd string) ([]byte, string, error) {
	candidates := []string{
		filepath.Join(cwd, "belayer-pipeline.yaml"),
		filepath.Join(cwd, ".belayer", "pipeline.yaml"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("read pipeline %q: %w", path, err)
		}
		cfg, err := pipeline.ParsePipeline(data)
		if err != nil {
			return nil, "", fmt.Errorf("parse pipeline %q: %w", path, err)
		}
		return data, cfg.Name, nil
	}

	return nil, "", fmt.Errorf("no pipeline found (checked %s and %s). Run 'belayer setup --framework claude-tmux' to install one", candidates[0], candidates[1])
}

// CreateGitWorktree creates a new git worktree at worktreeDir on a new branch.
// The main repo stays on its current branch — all pipeline work happens in the worktree.
func CreateGitWorktree(repoDir, worktreeDir, branch string) error {
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return fmt.Errorf("create worktree parent dir: %w", err)
	}
	cmd := exec.Command("git", "worktree", "add", worktreeDir, "-b", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// slugPattern splits on non-alphanumeric characters for branch slug generation.
var slugPattern = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// GenerateBranchSlug creates a short branch-friendly slug from the description.
// Uses first 4 meaningful words (3+ chars), kebab-case, max 40 chars.
func GenerateBranchSlug(description string) string {
	words := slugPattern.Split(strings.ToLower(description), -1)
	var meaningful []string
	for _, w := range words {
		if len(w) >= 3 {
			meaningful = append(meaningful, w)
		}
		if len(meaningful) == 4 {
			break
		}
	}
	if len(meaningful) == 0 {
		return "impl"
	}
	slug := strings.Join(meaningful, "-")
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "-")
	}
	return slug
}
