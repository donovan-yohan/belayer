package cli

import (
	"fmt"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/coordinator"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show task and lead status",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve instance
			if instanceName == "" {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				instanceName = cfg.DefaultInstance
				if instanceName == "" {
					return fmt.Errorf("no default instance set; use --instance flag")
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

			store := coordinator.NewStore(database.Conn())
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "Instance: %s\n\n", instanceName)

			// Show tasks by status
			for _, status := range []model.TaskStatus{
				model.TaskStatusRunning,
				model.TaskStatusPending,
				model.TaskStatusDecomposing,
				model.TaskStatusAligning,
				model.TaskStatusComplete,
				model.TaskStatusFailed,
			} {
				tasks, err := store.GetTasksByStatus(status)
				if err != nil {
					return fmt.Errorf("querying tasks: %w", err)
				}
				for _, task := range tasks {
					fmt.Fprintf(out, "Task: %s [%s]\n", task.ID, task.Status)
					fmt.Fprintf(out, "  Description: %s\n", task.Description)

					// Show leads for this task
					leads, err := store.GetLeadsForTask(task.ID)
					if err != nil {
						fmt.Fprintf(out, "  Leads: error querying: %v\n", err)
						continue
					}
					if len(leads) > 0 {
						fmt.Fprintf(out, "  Leads:\n")
						for _, l := range leads {
							fmt.Fprintf(out, "    %s [%s] attempt=%d\n", l.ID, l.Status, l.Attempt)
						}
					}
					fmt.Fprintln(out)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}
