package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/coordinator"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
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

func newTaskCreateCmd() *cobra.Command {
	var jira string
	var instanceName string

	cmd := &cobra.Command{
		Use:   "create [description]",
		Short: "Create a new task and start the coordinator",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var description string
			if jira != "" {
				description = fmt.Sprintf("Jira ticket: %s", jira)
			} else if len(args) > 0 {
				description = args[0]
			} else {
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

			_, instanceDir, err := instance.Load(instanceName)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", instanceName, err)
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

			// Create task
			taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
			source := "text"
			sourceRef := ""
			if jira != "" {
				source = "jira"
				sourceRef = jira
			}

			task := &model.Task{
				ID:          taskID,
				InstanceID:  instanceName,
				Description: description,
				Source:      source,
				SourceRef:   sourceRef,
				Status:      model.TaskStatusPending,
			}

			if err := coordStore.InsertTask(task); err != nil {
				return fmt.Errorf("creating task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created task %s\n", taskID)

			// Start coordinator
			leadRunner := lead.NewRunner(leadStore)
			worktrees := &instanceWorktreeAdapter{instanceDir: instanceDir}
			coordConfig := coordinator.DefaultConfig()
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

	cmd.Flags().StringVar(&jira, "jira", "", "Jira ticket ID(s)")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}
