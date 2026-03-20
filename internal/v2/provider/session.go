// Package provider implements the two execution models for belayer pipeline roles.
//
// Type A (Pitch): JSON in/out via exec — see exec.go
// Type B (Ascent): Interactive session with CLI-callback — see session.go (this file)
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// SessionSpawner launches interactive sessions for Type B (ascent) roles.
// The session receives a system prompt instructing it to call
// `belayer <role> finish --task-id <id>` when done.
type SessionSpawner interface {
	Spawn(ctx context.Context, opts SessionOpts) (*SessionInfo, error)
}

// SessionOpts contains everything needed to spawn an interactive session.
type SessionOpts struct {
	RoleName    string
	TaskID      string
	WorkDir     string
	InputJSON   json.RawMessage
	Provider    string // "claude", "codex", or custom command
	ExtraPrompt string // Additional context prepended to the initial prompt
}

// SessionInfo is returned after a session is spawned.
type SessionInfo struct {
	TmuxSession string
	WindowName  string
}

// ClaudeSessionSpawner spawns interactive Claude Code sessions in tmux.
type ClaudeSessionSpawner struct {
	tmux tmux.TmuxManager
}

// NewClaudeSessionSpawner creates a spawner backed by the given TmuxManager.
func NewClaudeSessionSpawner(tm tmux.TmuxManager) *ClaudeSessionSpawner {
	return &ClaudeSessionSpawner{tmux: tm}
}

// Spawn launches a Claude Code session with a system prompt that includes
// the belayer CLI callback instructions.
func (c *ClaudeSessionSpawner) Spawn(ctx context.Context, opts SessionOpts) (*SessionInfo, error) {
	tmuxSession := "belayer"
	windowName := fmt.Sprintf("%s-%s", opts.RoleName, opts.TaskID[:8])

	// Ensure tmux session exists.
	if !c.tmux.HasSession(tmuxSession) {
		if err := c.tmux.NewSession(tmuxSession); err != nil {
			return nil, fmt.Errorf("create tmux session: %w", err)
		}
	}
	if err := c.tmux.NewWindow(tmuxSession, windowName); err != nil {
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	// Write input JSON to a file the session can read.
	if opts.InputJSON != nil && opts.WorkDir != "" {
		inputPath := filepath.Join(opts.WorkDir, ".belayer", "input.json")
		if err := os.MkdirAll(filepath.Dir(inputPath), 0o755); err == nil {
			_ = os.WriteFile(inputPath, opts.InputJSON, 0o644)
		}
	}

	// Build the system prompt with CLI callback instructions.
	systemPrompt := buildSystemPrompt(opts.RoleName, opts.TaskID)

	// Build the initial prompt with context.
	initialPrompt := opts.ExtraPrompt
	if initialPrompt == "" {
		initialPrompt = fmt.Sprintf("You are starting as the %s role. Check .belayer/input.json for your task context.", opts.RoleName)
	}

	// Build the command.
	cmd := fmt.Sprintf("cd %s && claude --dangerously-skip-permissions --append-system-prompt %s %s 2>&1; echo 'Session exited'",
		shellQuote(opts.WorkDir),
		shellQuote(systemPrompt),
		shellQuote(initialPrompt))

	if err := c.tmux.SendKeys(tmuxSession, windowName, cmd); err != nil {
		return nil, fmt.Errorf("send keys: %w", err)
	}

	return &SessionInfo{
		TmuxSession: tmuxSession,
		WindowName:  windowName,
	}, nil
}

// CodexSessionSpawner spawns interactive Codex sessions in tmux.
type CodexSessionSpawner struct {
	tmux tmux.TmuxManager
}

// NewCodexSessionSpawner creates a spawner backed by the given TmuxManager.
func NewCodexSessionSpawner(tm tmux.TmuxManager) *CodexSessionSpawner {
	return &CodexSessionSpawner{tmux: tm}
}

// Spawn launches a Codex session. Codex has no --append-system-prompt,
// so callback instructions are prepended to the prompt.
func (c *CodexSessionSpawner) Spawn(ctx context.Context, opts SessionOpts) (*SessionInfo, error) {
	tmuxSession := "belayer"
	windowName := fmt.Sprintf("%s-%s", opts.RoleName, opts.TaskID[:8])

	if !c.tmux.HasSession(tmuxSession) {
		if err := c.tmux.NewSession(tmuxSession); err != nil {
			return nil, fmt.Errorf("create tmux session: %w", err)
		}
	}
	if err := c.tmux.NewWindow(tmuxSession, windowName); err != nil {
		return nil, fmt.Errorf("create tmux window: %w", err)
	}

	// Write input JSON.
	if opts.InputJSON != nil && opts.WorkDir != "" {
		inputPath := filepath.Join(opts.WorkDir, ".belayer", "input.json")
		if err := os.MkdirAll(filepath.Dir(inputPath), 0o755); err == nil {
			_ = os.WriteFile(inputPath, opts.InputJSON, 0o644)
		}
	}

	// Codex: prepend callback instructions to the prompt.
	systemPrompt := buildSystemPrompt(opts.RoleName, opts.TaskID)
	prompt := systemPrompt + "\n\n" + opts.ExtraPrompt

	cmd := fmt.Sprintf("cd %s && codex --dangerously-bypass-approvals-and-sandbox %s 2>&1; echo 'Session exited'",
		shellQuote(opts.WorkDir),
		shellQuote(prompt))

	if err := c.tmux.SendKeys(tmuxSession, windowName, cmd); err != nil {
		return nil, fmt.Errorf("send keys: %w", err)
	}

	return &SessionInfo{
		TmuxSession: tmuxSession,
		WindowName:  windowName,
	}, nil
}

// buildSystemPrompt creates the CLI callback instructions for a role.
func buildSystemPrompt(roleName, taskID string) string {
	return fmt.Sprintf(`You are the %s in a belayer pipeline.

IMPORTANT: When you have completed your work, you MUST signal completion by running:
  belayer %s finish --task-id %s

If you need help or are stuck, run:
  belayer %s flare --task-id %s --message "describe the problem"

If you cannot complete the task, run:
  belayer %s fail --task-id %s --message "describe why"

These commands are how the pipeline knows you are done. Do not skip them.`,
		roleName,
		roleName, taskID,
		roleName, taskID,
		roleName, taskID)
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
