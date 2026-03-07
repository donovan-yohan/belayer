package lead

import (
	"context"
	"fmt"
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
func (c *ClaudeSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	cmd := fmt.Sprintf("cd %s && claude -p %s --allowedTools '*' 2>&1; echo 'Claude session exited'",
		shellQuote(opts.WorkDir),
		shellQuote(opts.Prompt))

	return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
// This prevents shell injection when passing prompts as arguments.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
