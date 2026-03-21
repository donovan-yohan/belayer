package intake

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// GenerateWorkflowID returns a stable workflow ID in the form
// "{pipelineName}/{source}/{externalID}".
func GenerateWorkflowID(pipelineName, source, externalID string) string {
	return fmt.Sprintf("%s/%s/%s", pipelineName, source, externalID)
}

// ResolvePipelineYAML returns pipeline YAML bytes and a name by checking known locations,
// falling back to the built-in default.
func ResolvePipelineYAML(cwd string) ([]byte, string, error) {
	candidates := []string{
		filepath.Join(cwd, "belayer-pipeline.yaml"),
		filepath.Join(cwd, ".belayer", "pipeline.yaml"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cfg, err := pipeline.ParsePipeline(data)
		if err != nil {
			return nil, "", fmt.Errorf("parse pipeline %q: %w", path, err)
		}
		return data, cfg.Name, nil
	}

	// Built-in default.
	cfg, err := pipeline.ParsePipeline([]byte(pipeline.DefaultPipelineYAML))
	if err != nil {
		return nil, "", fmt.Errorf("parse default pipeline: %w", err)
	}
	return []byte(pipeline.DefaultPipelineYAML), cfg.Name, nil
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

// GenerateBranchSlug uses claude -p haiku to create a short branch-friendly slug
// from the description. Falls back to "impl" if claude is unavailable or slow.
func GenerateBranchSlug(description string) string {
	// Truncate input to keep the prompt small.
	input := description
	if len(input) > 500 {
		input = input[:500]
	}

	prompt := fmt.Sprintf(`Generate a short git branch name slug (2-4 words, kebab-case, lowercase, no special characters) that summarizes this work. Output ONLY the slug, nothing else.

%s`, input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", prompt)
	out, err := cmd.Output()
	if err != nil {
		return "impl"
	}

	slug := strings.TrimSpace(string(out))
	// Sanitize: only allow lowercase alphanumeric and hyphens.
	var sanitized strings.Builder
	for _, r := range slug {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-':
			sanitized.WriteRune(r)
		case unicode.IsUpper(r):
			sanitized.WriteRune(unicode.ToLower(r))
		case r == ' ' || r == '_':
			sanitized.WriteRune('-')
		}
	}
	result := sanitized.String()
	result = strings.Trim(result, "-")
	if result == "" {
		return "impl"
	}
	// Cap length.
	if len(result) > 40 {
		result = result[:40]
		result = strings.TrimRight(result, "-")
	}
	return result
}
