package lead

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// CodexSpawner implements AgentSpawner by launching interactive Codex CLI
// sessions in tmux windows.
type CodexSpawner struct {
	tmux tmux.TmuxManager
}

// NewCodexSpawner creates a CodexSpawner backed by the given TmuxManager.
func NewCodexSpawner(tm tmux.TmuxManager) *CodexSpawner {
	return &CodexSpawner{tmux: tm}
}

// Spawn launches an interactive Codex CLI session in the specified tmux window.
// Codex has no --append-system-prompt equivalent, so role instructions are
// prepended to the initial prompt text (role context before task).
func (c *CodexSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	// Build env exports for per-window isolation
	var envExports string
	for k, v := range opts.Env {
		envExports += fmt.Sprintf("export %s=%s && ", k, shellQuote(v))
	}

	// Prepend role instructions to the prompt since Codex has no system prompt flag
	prompt := opts.InitialPrompt
	if opts.AppendSystemPrompt != "" {
		prompt = opts.AppendSystemPrompt + "\n\n---\n\n" + opts.InitialPrompt
	}

	cmd := fmt.Sprintf("%scd %s && codex --dangerously-bypass-approvals-and-sandbox %s 2>&1; echo 'Codex session exited'",
		envExports,
		shellQuote(opts.WorkDir),
		shellQuote(prompt))

	return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}
