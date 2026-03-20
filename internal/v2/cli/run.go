package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/pipeline"
	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

const observerPort = 8790

func newRunCmd() *cobra.Command {
	var pipelineFile, fromRole, toRole, inputFile string
	var detach bool

	cmd := &cobra.Command{
		Use:   "run [description]",
		Short: "Start a pipeline run (ascent)",
		Long: `Start a Temporal workflow and open an observer session.

The observer session receives live pipeline events (phase transitions,
role completions, flares, risk gates) via the belayer channel.

Use --detach for fire-and-forget mode (prints workflow ID and exits).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			description := strings.Join(args, " ")
			if detach {
				return runPipelineDetached(cmd.Context(), description, pipelineFile, fromRole, toRole, inputFile)
			}
			return runPipelineWithObserver(cmd.Context(), description, pipelineFile, fromRole, toRole, inputFile)
		},
	}

	cmd.Flags().StringVar(&pipelineFile, "pipeline", "", "Pipeline DSL file (default: belayer-pipeline.yaml)")
	cmd.Flags().StringVar(&fromRole, "from", "", "Start from this role (pipeline slicing)")
	cmd.Flags().StringVar(&toRole, "to", "", "Stop after this role")
	cmd.Flags().StringVar(&inputFile, "input", "", "JSON input file for --from role")
	cmd.Flags().BoolVar(&detach, "detach", false, "Fire-and-forget mode (print workflow ID and exit)")

	return cmd
}

// runPipelineWithObserver starts the workflow and opens an observer Claude Code session.
func runPipelineWithObserver(ctx context.Context, description, pipelineFile, fromRole, toRole, inputFile string) error {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal server. Run 'belayer temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	route, source, err := findAndParsePipeline(pipelineFile)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	fmt.Printf("Using pipeline: %s (%s)\n", route.Name, source)

	routeJSON, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("serialize pipeline: %w", err)
	}

	input := model.RouteInput{
		Description:  description,
		PipelineFile: pipelineFile,
		RouteJSON:    routeJSON,
		FromRole:     fromRole,
		ToRole:       toRole,
	}

	opts := client.StartWorkflowOptions{
		ID:        fmt.Sprintf("belayer-route-%d", time.Now().UnixMilli()),
		TaskQueue: beltemporal.TaskQueueName,
	}

	run, err := c.ExecuteWorkflow(ctx, opts, beltemporal.RouteWorkflow, input)
	if err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	fmt.Printf("\nPipeline started!\n")
	fmt.Printf("  Workflow:  %s\n", run.GetID())
	fmt.Printf("  Web UI:    http://localhost:8233/namespaces/default/workflows/%s/%s\n",
		run.GetID(), run.GetRunID())

	// Resolve paths for channel and hooks.
	channelScript, _ := resolveChannelPaths()

	if channelScript == "" {
		fmt.Printf("\nChannel server not found. Running in detached mode.\n")
		fmt.Printf("Use 'belayer status' to check progress.\n")
		return nil
	}

	// Write .mcp.json for the observer session.
	cwd, _ := os.Getwd()
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
}`, channelScript, observerPort)

	mcpPath := filepath.Join(cwd, ".mcp.json")
	_ = os.WriteFile(mcpPath, []byte(mcpConfig), 0o644)

	// Build the observer system prompt.
	observerPrompt := fmt.Sprintf(`You are the belayer observer for pipeline run %s.

Pipeline events arrive as <channel source="belayer-channel"> tags. Your job:
- Report pipeline status to the user as events arrive
- Alert immediately on flare events (a session needs help)
- Help the user with risk gate decisions when they arrive
- Summarize progress when asked

The pipeline is: %s
Description: %s

Worker sessions are running in tmux. Use 'belayer attach <role>' in another terminal to view them.`, run.GetID(), route.Name, description)

	fmt.Printf("\nOpening observer session. Pipeline events will stream here.\n")
	fmt.Printf("Workers run in tmux — use 'belayer attach <role>' to view.\n\n")

	// Exec into Claude Code as the observer.
	claudeArgs := []string{
		"--dangerously-skip-permissions",
		"--dangerously-load-development-channels", "server:belayer-channel",
		"--append-system-prompt", observerPrompt,
		fmt.Sprintf("Pipeline '%s' started for: %s. Waiting for events...", route.Name, description),
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// Set env vars for the observer.
	os.Setenv("BELAYER_TASK_ID", run.GetID())
	os.Setenv("BELAYER_ROLE", "observer")
	os.Setenv("BELAYER_CHANNEL_PORT", fmt.Sprintf("%d", observerPort))

	// Exec replaces this process with claude.
	claudeCmd := exec.CommandContext(ctx, claudePath, claudeArgs...)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Dir = cwd
	return claudeCmd.Run()
}

// runPipelineDetached is the fire-and-forget mode (original behavior).
func runPipelineDetached(ctx context.Context, description, pipelineFile, fromRole, toRole, inputFile string) error {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal server. Run 'belayer temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	route, source, err := findAndParsePipeline(pipelineFile)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	fmt.Printf("Using pipeline: %s (%s)\n", route.Name, source)

	routeJSON, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("serialize pipeline: %w", err)
	}

	input := model.RouteInput{
		Description:  description,
		PipelineFile: pipelineFile,
		RouteJSON:    routeJSON,
		FromRole:     fromRole,
		ToRole:       toRole,
	}

	opts := client.StartWorkflowOptions{
		ID:        fmt.Sprintf("belayer-route-%d", time.Now().UnixMilli()),
		TaskQueue: beltemporal.TaskQueueName,
	}

	run, err := c.ExecuteWorkflow(ctx, opts, beltemporal.RouteWorkflow, input)
	if err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	fmt.Printf("\nPipeline started!\n")
	fmt.Printf("  Run ID:    %s\n", run.GetRunID())
	fmt.Printf("  Workflow:  %s\n", run.GetID())
	fmt.Printf("  Web UI:    http://localhost:8233/namespaces/default/workflows/%s/%s\n",
		run.GetID(), run.GetRunID())
	fmt.Printf("\nUse 'belayer status' to check progress.\n")
	fmt.Printf("Use 'belayer attach <role>' to view sessions.\n")

	return nil
}

// findAndParsePipeline locates and parses the pipeline YAML file.
func findAndParsePipeline(pipelineFlag string) (*pipeline.Route, string, error) {
	if pipelineFlag != "" {
		route, err := pipeline.ParseRouteFile(pipelineFlag)
		if err != nil {
			return nil, "", err
		}
		if err := pipeline.ValidateOrError(route); err != nil {
			return nil, "", err
		}
		return route, pipelineFlag, nil
	}

	const defaultFile = "belayer-pipeline.yaml"
	if _, err := os.Stat(defaultFile); err == nil {
		route, err := pipeline.ParseRouteFile(defaultFile)
		if err != nil {
			return nil, "", err
		}
		if err := pipeline.ValidateOrError(route); err != nil {
			return nil, "", err
		}
		return route, defaultFile, nil
	}

	route, err := pipeline.ParseRoute([]byte(pipeline.DefaultPipelineYAML))
	if err != nil {
		return nil, "", fmt.Errorf("parse default pipeline: %w", err)
	}
	return route, "built-in default (solo)", nil
}
