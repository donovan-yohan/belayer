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
	"time"

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
	RoleName     string
	RepoName     string // For multi-repo: which repo this session is for
	TaskID       string
	WorkDir      string
	InputJSON    json.RawMessage
	Provider     string // "claude", "codex", or custom command
	ExtraPrompt  string // Additional context prepended to the initial prompt
	ChannelPort  int    // HTTP port for this session's belayer channel (0 = no channel)
	ObserverPort int    // HTTP port of the observer session's channel (0 = no observer)
	HooksDir     string // Absolute path to channel/hooks/ directory
	ChannelScript string // Absolute path to channel/channel.ts
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
	var windowName string
	if opts.RepoName != "" {
		windowName = fmt.Sprintf("%s-%s-%s", opts.RoleName, opts.RepoName, opts.TaskID[:8])
	} else {
		windowName = fmt.Sprintf("%s-%s", opts.RoleName, opts.TaskID[:8])
	}

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
	belayerDir := filepath.Join(opts.WorkDir, ".belayer")
	if opts.InputJSON != nil && opts.WorkDir != "" {
		inputPath := filepath.Join(belayerDir, "input.json")
		if err := os.MkdirAll(filepath.Dir(inputPath), 0o755); err == nil {
			_ = os.WriteFile(inputPath, opts.InputJSON, 0o644)
		}
	}

	// Write per-session channel config if channel is enabled.
	channelFlag := ""
	if opts.ChannelPort > 0 && opts.ChannelScript != "" {
		if err := writeSessionMCPConfig(opts); err != nil {
			// Non-fatal: channel is best-effort.
			fmt.Fprintf(os.Stderr, "belayer: warning: could not write channel config: %v\n", err)
		}
		if err := writeSessionHooksConfig(opts); err != nil {
			fmt.Fprintf(os.Stderr, "belayer: warning: could not write hooks config: %v\n", err)
		}
		channelFlag = " --dangerously-load-development-channels server:belayer-channel"
	}

	// Build env exports for belayer context.
	var envExports string
	if opts.ChannelPort > 0 {
		envExports += fmt.Sprintf("export BELAYER_TASK_ID=%s && ", shellQuote(opts.TaskID))
		envExports += fmt.Sprintf("export BELAYER_ROLE=%s && ", shellQuote(opts.RoleName))
		envExports += fmt.Sprintf("export BELAYER_CHANNEL_PORT=%d && ", opts.ChannelPort)
		if opts.RepoName != "" {
			envExports += fmt.Sprintf("export BELAYER_REPO=%s && ", shellQuote(opts.RepoName))
		}
		if opts.ObserverPort > 0 {
			envExports += fmt.Sprintf("export BELAYER_OBSERVER_PORT=%d && ", opts.ObserverPort)
		}
	}

	// Build the system prompt with CLI callback instructions.
	systemPrompt := buildSystemPrompt(opts.RoleName, opts.TaskID, opts.RepoName)

	// Build the initial prompt with context.
	initialPrompt := opts.ExtraPrompt
	if initialPrompt == "" {
		initialPrompt = fmt.Sprintf("You are starting as the %s role. Check .belayer/input.json for your task context.", opts.RoleName)
	}

	// Build the command.
	cmd := fmt.Sprintf("%scd %s && claude --dangerously-skip-permissions%s --append-system-prompt %s %s 2>&1; echo 'Session exited'",
		envExports,
		shellQuote(opts.WorkDir),
		channelFlag,
		shellQuote(systemPrompt),
		shellQuote(initialPrompt))

	if err := c.tmux.SendKeys(tmuxSession, windowName, cmd); err != nil {
		return nil, fmt.Errorf("send keys: %w", err)
	}

	// Auto-confirm the development channels trust prompt.
	// --dangerously-load-development-channels shows an interactive confirmation.
	// Send Enter after a delay to accept it automatically.
	if opts.ChannelPort > 0 {
		go func() {
			time.Sleep(3 * time.Second)
			_ = c.tmux.SendKeysRaw(tmuxSession+":"+windowName, "Enter")
		}()
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
	var windowName string
	if opts.RepoName != "" {
		windowName = fmt.Sprintf("%s-%s-%s", opts.RoleName, opts.RepoName, opts.TaskID[:8])
	} else {
		windowName = fmt.Sprintf("%s-%s", opts.RoleName, opts.TaskID[:8])
	}

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
	systemPrompt := buildSystemPrompt(opts.RoleName, opts.TaskID, opts.RepoName)
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
// If repoName is non-empty, includes --repo flag in the instructions.
func buildSystemPrompt(roleName, taskID, repoName string) string {
	repoFlag := ""
	if repoName != "" {
		repoFlag = fmt.Sprintf(" --repo %s", repoName)
	}

	return fmt.Sprintf(`You are the %s in a belayer pipeline.

IMPORTANT: When you have completed your work, you MUST signal completion by running:
  belayer %s finish --task-id %s%s

If you need help or are stuck, run:
  belayer %s flare --task-id %s%s --message "describe the problem"

If you cannot complete the task, run:
  belayer %s fail --task-id %s%s --message "describe why"

These commands are how the pipeline knows you are done. Do not skip them.`,
		roleName,
		roleName, taskID, repoFlag,
		roleName, taskID, repoFlag,
		roleName, taskID, repoFlag)
}

// writeSessionMCPConfig writes a per-session .mcp.json so Claude Code finds the channel server.
func writeSessionMCPConfig(opts SessionOpts) error {
	mcpDir := filepath.Join(opts.WorkDir, ".belayer")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		return err
	}

	mcpConfig := fmt.Sprintf(`{
  "mcpServers": {
    "belayer-channel": {
      "command": "bun",
      "args": [%q],
      "env": {
        "BELAYER_CHANNEL_PORT": "%d"
      }
    }
  }
}`, opts.ChannelScript, opts.ChannelPort)

	// Write to the WorkDir so Claude Code picks it up.
	return os.WriteFile(filepath.Join(opts.WorkDir, ".mcp.json"), []byte(mcpConfig), 0o644)
}

// writeSessionHooksConfig writes a .claude/settings.local.json with belayer hooks.
func writeSessionHooksConfig(opts SessionOpts) error {
	if opts.HooksDir == "" {
		return nil
	}

	claudeDir := filepath.Join(opts.WorkDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	hooksConfig := fmt.Sprintf(`{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "%s/stop-insurance.sh"
          }
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "permission_prompt|idle_prompt",
        "hooks": [
          {
            "type": "command",
            "command": "%s/notification-router.sh"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "%s/session-start.sh"
          }
        ]
      }
    ]
  }
}`, opts.HooksDir, opts.HooksDir, opts.HooksDir)

	return os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(hooksConfig), 0o644)
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
