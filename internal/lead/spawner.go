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

// SpawnerSet holds per-role spawners so different agent providers can be
// mixed and matched (e.g. Codex for leads, Claude for spotters).
type SpawnerSet struct {
	Lead    AgentSpawner
	Spotter AgentSpawner
	Anchor  AgentSpawner
}

// SpawnerSetConfig specifies the provider for each role.
// Empty strings fall back to DefaultProvider.
type SpawnerSetConfig struct {
	DefaultProvider  string
	LeadProvider     string
	SpotterProvider  string
	AnchorProvider   string
}

// NewSpawnerSet creates a SpawnerSet from per-role provider config.
// Empty role providers inherit from DefaultProvider.
func NewSpawnerSet(cfg SpawnerSetConfig, tm tmux.TmuxManager) (*SpawnerSet, error) {
	resolve := func(role, override string) (AgentSpawner, error) {
		provider := override
		if provider == "" {
			provider = cfg.DefaultProvider
		}
		sp, err := NewSpawner(provider, tm)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", role, err)
		}
		return sp, nil
	}

	leadSp, err := resolve("lead", cfg.LeadProvider)
	if err != nil {
		return nil, err
	}
	spotterSp, err := resolve("spotter", cfg.SpotterProvider)
	if err != nil {
		return nil, err
	}
	anchorSp, err := resolve("anchor", cfg.AnchorProvider)
	if err != nil {
		return nil, err
	}

	return &SpawnerSet{
		Lead:    leadSp,
		Spotter: spotterSp,
		Anchor:  anchorSp,
	}, nil
}
