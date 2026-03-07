package cli

import (
	"fmt"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show task and goal status",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedName, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "Instance: %s\n\n", resolvedName)

			for _, status := range []model.TaskStatus{
				model.TaskStatusRunning,
				model.TaskStatusPending,
				model.TaskStatusReviewing,
				model.TaskStatusComplete,
				model.TaskStatusStuck,
			} {
				tasks, err := s.GetTasksByStatus(status)
				if err != nil {
					return fmt.Errorf("querying tasks: %w", err)
				}
				for _, task := range tasks {
					fmt.Fprintf(out, "Task: %s [%s]\n", task.ID, task.Status)

					goals, err := s.GetGoalsForTask(task.ID)
					if err != nil {
						fmt.Fprintf(out, "  Goals: error querying: %v\n", err)
						continue
					}
					if len(goals) > 0 {
						fmt.Fprintf(out, "  Goals:\n")
						for _, g := range goals {
							fmt.Fprintf(out, "    %s [%s] repo=%s attempt=%d\n", g.ID, g.Status, g.RepoName, g.Attempt)
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
