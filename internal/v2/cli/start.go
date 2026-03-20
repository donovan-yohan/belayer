package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Open a belayer session (brainstorm, submit specs, observe pipelines)",
		Long: `Open a Claude Code session connected to the belayer pipeline.

This is your workspace for brainstorming, research, and drafting specs.
When a spec is ready, tell Claude to submit it — the pipeline starts
automatically. Events from running pipelines stream back to you.

Use in a repo directory for single-repo work, or with --crag for multi-repo.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startSession(cragName)
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Named crag for multi-repo context")

	return cmd
}

func startSession(cragName string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Resolve channel paths.
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

	// Build the system prompt.
	systemPrompt := `You are the user's belayer session — a brainstorming partner and pipeline observer.

YOUR ROLE:
- Help the user research, brainstorm, and draft implementation specs
- When a spec is ready, use the submit tool to start a pipeline run
- Report pipeline events that arrive as <channel> tags
- Alert immediately on flare events (a session needs help)
- Help with risk gate decisions when they arrive

TOOLS AVAILABLE:
- submit: Start a pipeline run with a spec. Call when the user says "send it", "submit", or "start the pipeline".
- status: Check active pipeline runs. Call when the user asks "what's running?"

PIPELINE EVENTS:
Events arrive as <channel source="belayer-channel" event="..." pipeline_id="..."> tags.
- pipeline_started: A new run began
- role_completed: A role finished (report which repo)
- flare: A session needs help (ALERT THE USER IMMEDIATELY)
- pipeline_completed: A run finished

When multiple pipelines are running, group updates by pipeline name.
Flares always interrupt — tell the user immediately what needs attention.`

	fmt.Println("Opening belayer session...")
	fmt.Println("  Brainstorm freely. When a spec is ready, tell me to submit it.")
	fmt.Println("  Pipeline events will stream in as they happen.")
	fmt.Println("  Use 'belayer attach <role>' in another terminal to view worker sessions.")
	fmt.Println()

	// Find claude binary.
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// Set env vars.
	os.Setenv("BELAYER_ROLE", "observer")
	os.Setenv("BELAYER_CHANNEL_PORT", "8790")

	// Build claude args.
	claudeArgs := []string{
		"--dangerously-skip-permissions",
		"--dangerously-load-development-channels", "server:belayer-channel",
		"--append-system-prompt", systemPrompt,
	}

	// Exec into Claude Code.
	claudeCmd := exec.Command(claudePath, claudeArgs...)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Dir = cwd
	return claudeCmd.Run()
}
