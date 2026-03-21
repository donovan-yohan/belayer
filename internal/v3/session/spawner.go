package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

const belayerSession = "belayer-v3"

// SpawnOpts holds the parameters needed to spawn a Claude session for a pipeline node.
type SpawnOpts struct {
	NodeName    string
	TaskID      string
	Attempt     int
	WorkDir     string
	Description string
	HooksPath   string
	InputPrompt string
}

// WindowName returns the tmux window name for this spawn: "{NodeName}-{TaskID[:8]}".
func (o SpawnOpts) WindowName() string {
	id := o.TaskID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("%s-%s", o.NodeName, id)
}

// Spawner launches Claude sessions for pipeline nodes.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) error
}

// TmuxSpawner implements Spawner using a tmux.TmuxManager.
type TmuxSpawner struct {
	tm tmux.TmuxManager
}

// NewTmuxSpawner returns a new TmuxSpawner backed by the given TmuxManager.
func NewTmuxSpawner(tm tmux.TmuxManager) *TmuxSpawner {
	return &TmuxSpawner{tm: tm}
}

// Spawn ensures the "belayer-v3" tmux session exists, creates a window for the node,
// and sends the Claude command via SendKeys.
func (s *TmuxSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	if !s.tm.HasSession(belayerSession) {
		if err := s.tm.NewSession(belayerSession); err != nil {
			return fmt.Errorf("create tmux session %q: %w", belayerSession, err)
		}
	}

	win := opts.WindowName()
	if err := s.tm.NewWindow(belayerSession, win); err != nil {
		return fmt.Errorf("create tmux window %q: %w", win, err)
	}

	cmd := buildEnvExports(opts) + buildClaudeCommand(opts)
	if err := s.tm.SendKeys(belayerSession, win, cmd); err != nil {
		return fmt.Errorf("send keys to %s:%s: %w", belayerSession, win, err)
	}

	return nil
}

// buildClaudeCommand constructs the claude CLI invocation for a node session.
func buildClaudeCommand(opts SpawnOpts) string {
	return fmt.Sprintf(
		"claude --dangerously-skip-permissions --append-system-prompt %s --settings %s %s",
		shellQuote(opts.Description),
		shellQuote(opts.HooksPath),
		shellQuote(opts.InputPrompt),
	)
}

// buildEnvExports constructs the env variable exports to prepend to the command.
func buildEnvExports(opts SpawnOpts) string {
	return fmt.Sprintf(
		"export BELAYER_TASK_ID=%s && export BELAYER_NODE=%s && export BELAYER_ATTEMPT=%d && ",
		shellQuote(opts.TaskID),
		shellQuote(opts.NodeName),
		opts.Attempt,
	)
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
