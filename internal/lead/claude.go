package lead

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// ClaudeSpawner implements AgentSpawner by launching Claude Code sessions
// in tmux windows via `claude -p`.
type ClaudeSpawner struct {
	tmux tmux.TmuxManager
}

// NewClaudeSpawner creates a ClaudeSpawner backed by the given TmuxManager.
func NewClaudeSpawner(tm tmux.TmuxManager) *ClaudeSpawner {
	return &ClaudeSpawner{tmux: tm}
}

// Spawn launches a Claude Code session in the specified tmux window.
// The prompt is written to a temp file and piped into claude via stdin
// to avoid shell quoting issues with multi-line prompts.
func (c *ClaudeSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	// Write prompt to a temp file in the workdir
	promptFile := filepath.Join(opts.WorkDir, ".belayer-prompt.md")
	if err := os.WriteFile(promptFile, []byte(opts.Prompt), 0o644); err != nil {
		return fmt.Errorf("writing prompt file: %w", err)
	}

	cmd := fmt.Sprintf("cd %s && claude -p --dangerously-skip-permissions < .belayer-prompt.md 2>&1; echo 'Claude session exited'",
		shellQuote(opts.WorkDir))

	return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
// This prevents shell injection when passing prompts as arguments.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
