package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/coordinator"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/intake"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}

	cmd.AddCommand(newTaskCreateCmd())
	return cmd
}

// instanceWorktreeAdapter adapts instance.CreateWorktree to the WorktreeCreator interface.
type instanceWorktreeAdapter struct {
	instanceDir string
}

func (a *instanceWorktreeAdapter) CreateWorktree(instanceDir, taskID, repoName string) (string, error) {
	return instance.CreateWorktree(a.instanceDir, taskID, repoName)
}

// claudeExecutor implements intake.AgenticExecutor using the real claude CLI.
type claudeExecutor struct {
	model string
}

func (e *claudeExecutor) Execute(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", e.model, "--output-format", "json", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude failed: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

func newTaskCreateCmd() *cobra.Command {
	var jira string
	var instanceName string
	var noBrainstorm bool

	cmd := &cobra.Command{
		Use:   "create [description]",
		Short: "Create a new task and start the coordinator",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var description string
			if len(args) > 0 {
				description = args[0]
			}
			jiraTickets := intake.ParseJiraTickets(jira)

			if description == "" && len(jiraTickets) == 0 {
				return fmt.Errorf("provide a description or --jira flag")
			}

			// Resolve instance
			if instanceName == "" {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				instanceName = cfg.DefaultInstance
				if instanceName == "" {
					return fmt.Errorf("no default instance set; use --instance or run `belayer instance create` first")
				}
			}

			instConfig, instanceDir, err := instance.Load(instanceName)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", instanceName, err)
			}

			// Collect repo names from instance config
			var repoNames []string
			for _, repo := range instConfig.Repos {
				repoNames = append(repoNames, repo.Name)
			}

			// Run intake pipeline
			coordConfig := coordinator.DefaultConfig()
			executor := &claudeExecutor{model: coordConfig.AgenticModel}
			pipeline := intake.NewPipeline(executor)

			intakeResult, err := pipeline.Run(cmd.Context(), intake.PipelineConfig{
				Description:  description,
				JiraTickets:  jiraTickets,
				RepoNames:    repoNames,
				NoBrainstorm: noBrainstorm,
				Stdin:        cmd.InOrStdin(),
				Stdout:       cmd.OutOrStdout(),
			})
			if err != nil {
				return fmt.Errorf("intake pipeline: %w", err)
			}

			// Open database
			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			// Create stores
			coordStore := coordinator.NewStore(database.Conn())
			leadStore := lead.NewStore(database.Conn())

			// Create task from pipeline result
			taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
			task := &model.Task{
				ID:                 taskID,
				InstanceID:         instanceName,
				Description:        intakeResult.Description,
				Source:             intakeResult.Source,
				SourceRef:          intakeResult.SourceRef,
				SufficiencyChecked: intakeResult.SufficiencyChecked,
				Status:             model.TaskStatusPending,
			}

			if err := coordStore.InsertTask(task); err != nil {
				return fmt.Errorf("creating task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created task %s\n", taskID)

			// Start coordinator with repo names for instance-aware decomposition
			leadRunner := lead.NewRunner(leadStore)
			worktrees := &instanceWorktreeAdapter{instanceDir: instanceDir}
			coordConfig.RepoNames = repoNames
			coord := coordinator.NewCoordinator(
				coordStore, leadRunner, worktrees, instanceDir, instanceName, coordConfig,
			)

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// Handle interrupt
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down coordinator...")
				cancel()
			}()

			fmt.Fprintf(cmd.OutOrStdout(), "Starting coordinator for instance %q...\n", instanceName)
			return coord.Start(ctx)
		},
	}

	cmd.Flags().StringVar(&jira, "jira", "", "Comma-separated Jira ticket ID(s)")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	cmd.Flags().BoolVar(&noBrainstorm, "no-brainstorm", false, "Skip interactive brainstorm even if context is insufficient")
	return cmd
}
