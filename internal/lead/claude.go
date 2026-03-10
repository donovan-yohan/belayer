package lead

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// ClaudeSpawner implements AgentSpawner by launching interactive Claude Code
// sessions in tmux windows.
type ClaudeSpawner struct {
	tmux tmux.TmuxManager
}

// NewClaudeSpawner creates a ClaudeSpawner backed by the given TmuxManager.
func NewClaudeSpawner(tm tmux.TmuxManager) *ClaudeSpawner {
	return &ClaudeSpawner{tmux: tm}
}

// Spawn launches an interactive Claude Code session in the specified tmux window.
// The initial prompt is passed as a positional argument.
func (c *ClaudeSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	// Build env exports for per-window isolation
	var envExports string
	for k, v := range opts.Env {
		envExports += fmt.Sprintf("export %s=%s && ", k, shellQuote(v))
	}

	// Build command: full interactive session with positional prompt
	var appendPromptFlag string
	if opts.AppendSystemPrompt != "" {
		appendPromptFlag = " --append-system-prompt " + shellQuote(opts.AppendSystemPrompt)
	}

	cmd := fmt.Sprintf("%scd %s && claude --dangerously-skip-permissions%s %s 2>&1; echo 'Claude session exited'",
		envExports,
		shellQuote(opts.WorkDir),
		appendPromptFlag,
		shellQuote(opts.InitialPrompt))

	return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
// This prevents shell injection when passing prompts as arguments.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
