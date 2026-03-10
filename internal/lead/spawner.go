package lead

import "context"

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
	AppendSystemPrompt string // Role-specific instructions appended to default system prompt via --append-system-prompt
}
