package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/v2/provider"
	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

func newWorkerCmd() *cobra.Command {
	var workDir string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the Temporal worker for pipeline execution",
		Long: `Start a Temporal worker that picks up pipeline runs and executes them.
The worker registers the Route workflow and all activity implementations.
It runs until interrupted (Ctrl+C).

Start this BEFORE running 'belayer run'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startWorker(workDir)
		},
	}

	cmd.Flags().StringVar(&workDir, "work-dir", "", "Working directory for sessions (default: current directory)")

	return cmd
}

func startWorker(workDir string) error {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal. Run 'belayer temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})

	// Wire real providers into the activities.
	tm := tmux.NewRealTmux()
	channelScript, hooksDir := resolveChannelPaths()
	if channelScript != "" {
		fmt.Printf("  Channel:    %s\n", channelScript)
		fmt.Printf("  Hooks:      %s\n", hooksDir)
	}

	activities := &beltemporal.Activities{
		SessionSpawner: &workerSessionSpawner{
			claude:        provider.NewClaudeSessionSpawner(tm),
			codex:         provider.NewCodexSessionSpawner(tm),
			nextPort:      8791, // 8790 = observer, workers start at 8791
			channelScript: channelScript,
			hooksDir:      hooksDir,
		},
		ExecProvider: &provider.ExecProvider{},
		WorkDir:      workDir,
	}

	w.RegisterWorkflow(beltemporal.RouteWorkflow)
	w.RegisterActivity(activities)

	fmt.Printf("Belayer worker started\n")
	fmt.Printf("  Task queue: %s\n", beltemporal.TaskQueueName)
	fmt.Printf("  Work dir:   %s\n", workDir)
	fmt.Printf("  Providers:  Claude Code + Codex (Type B), Exec (Type A)\n")
	fmt.Printf("\nWaiting for pipeline runs... (Ctrl+C to stop)\n")

	return w.Run(worker.InterruptCh())
}

// workerSessionSpawner delegates to Claude or Codex based on provider config.
type workerSessionSpawner struct {
	claude        *provider.ClaudeSessionSpawner
	codex         *provider.CodexSessionSpawner
	nextPort      int // Sequential port assignment starting at 8791 (8790 = observer)
	channelScript string
	hooksDir      string
}

func (w *workerSessionSpawner) Spawn(ctx context.Context, roleName, taskID, workDir string, input json.RawMessage) (string, error) {
	port := w.nextPort
	w.nextPort++

	opts := provider.SessionOpts{
		RoleName:      roleName,
		TaskID:        taskID,
		WorkDir:       workDir,
		InputJSON:     input,
		ChannelPort:   port,
		ObserverPort:  8790, // Observer is always on 8790
		ChannelScript: w.channelScript,
		HooksDir:      w.hooksDir,
	}
	// Default to Claude for now. Provider selection will come from role config.
	info, err := w.claude.Spawn(ctx, opts)
	if err != nil {
		return "", err
	}
	return info.WindowName, nil
}
