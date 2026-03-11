package lead

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// AgentSpawner launches agent sessions in tmux windows.
// This is the vendor abstraction boundary — implementations exist for
// Claude Code, Codex, or any other agent runtime.
type AgentSpawner interface {
	// Spawn launches an agent in the given tmux window with the given prompt.
	Spawn(ctx context.Context, opts SpawnOpts) error
}

// SpawnOpts contains everything needed to spawn a lead session.
type SpawnOpts struct {
	TmuxSession        string
	WindowName         string
	WorkDir            string
	InitialPrompt      string
	AppendSystemPrompt string            // Role-specific instructions appended to default system prompt via --append-system-prompt
	Env                map[string]string // Per-window environment variables (injected via export, not tmux session env)
}

// NewSpawner creates an AgentSpawner for the given provider.
// Supported providers: "claude", "codex".
func NewSpawner(provider string, tm tmux.TmuxManager) (AgentSpawner, error) {
	switch provider {
	case "claude":
		return NewClaudeSpawner(tm), nil
	case "codex":
		return NewCodexSpawner(tm), nil
	default:
		return nil, fmt.Errorf("unknown agent provider: %q (expected \"claude\" or \"codex\")", provider)
	}
}
