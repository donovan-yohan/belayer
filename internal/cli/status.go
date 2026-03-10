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
		Short: "Show problem and climb status",
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

			for _, status := range []model.ProblemStatus{
				model.ProblemStatusRunning,
				model.ProblemStatusPending,
				model.ProblemStatusReviewing,
				model.ProblemStatusComplete,
				model.ProblemStatusStuck,
			} {
				problems, err := s.GetProblemsByStatus(status)
				if err != nil {
					return fmt.Errorf("querying problems: %w", err)
				}
				for _, problem := range problems {
					fmt.Fprintf(out, "Problem: %s [%s]\n", problem.ID, problem.Status)

					climbs, err := s.GetClimbsForProblem(problem.ID)
					if err != nil {
						fmt.Fprintf(out, "  Climbs: error querying: %v\n", err)
						continue
					}
					if len(climbs) > 0 {
						fmt.Fprintf(out, "  Climbs:\n")
						for _, c := range climbs {
							fmt.Fprintf(out, "    %s [%s] repo=%s attempt=%d\n", c.ID, c.Status, c.RepoName, c.Attempt)
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
