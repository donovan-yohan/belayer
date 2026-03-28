package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Open a belayer session (brainstorm, submit specs, observe pipelines)",
		Long: `Open a Claude Code session connected to the belayer pipeline.

This is your workspace for brainstorming, research, and drafting specs.
When a spec is ready, tell Claude to submit it — the pipeline starts
automatically. Events from running pipelines stream back to you.

Requires 'belayer worker' to be running.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startSession()
		},
	}

	return cmd
}

func startSession() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	channelScript, _ := resolveChannelPaths()
	if channelScript == "" {
		return fmt.Errorf("channel server not found. Run from the belayer repo directory or install channel to ~/.belayer/channel/")
	}

	// Write .mcp.json for the channel server.
	mcpConfig := fmt.Sprintf(`{
  "mcpServers": {
    "belayer-channel": {
      "command": "bun",
      "args": [%q],
      "env": {
        "BELAYER_CHANNEL_PORT": "8790",
        "BELAYER_WORKER_PORT": "8780"
      }
    }
  }
}`, channelScript)

	mcpPath := filepath.Join(cwd, ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte(mcpConfig), 0o644); err != nil {
		return fmt.Errorf("write .mcp.json: %w", err)
	}

	systemPrompt := `You are the user's belayer session — a brainstorming partner and pipeline observer.

YOUR ROLE:
- Help the user research, brainstorm, and draft implementation specs
- When a spec is ready, use the submit tool to start a pipeline run
- Report pipeline events that arrive as <channel> tags
- Alert immediately on flare events (a session needs help)

TOOLS AVAILABLE:
- submit: Start a pipeline run with a spec
- status: Check active pipeline runs

PIPELINE EVENTS:
Events arrive as <channel source="belayer-channel" event="..." pipeline_id="..."> tags.
- pipeline_started: A new run began
- role_completed: A role finished
- flare: A session needs help (ALERT THE USER IMMEDIATELY)
- pipeline_completed: A run finished`

	fmt.Println("Opening belayer session...")
	fmt.Println("  Brainstorm freely. When a spec is ready, tell me to submit it.")
	fmt.Println("  Pipeline events will stream in as they happen.")
	fmt.Println()

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	os.Setenv("BELAYER_ROLE", "observer")
	os.Setenv("BELAYER_CHANNEL_PORT", "8790")

	claudeCmd := exec.Command(claudePath,
		"--dangerously-skip-permissions",
		"--dangerously-load-development-channels", "server:belayer-channel",
		"--append-system-prompt", systemPrompt,
	)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Dir = cwd
	return claudeCmd.Run()
}
