package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newPRCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Manage monitored pull requests",
	}
	cmd.AddCommand(newPRListCmd())
	cmd.AddCommand(newPRShowCmd())
	cmd.AddCommand(newPRRetryCmd())
	return cmd
}

func loadPRDeps(cragName string) (*store.Store, string, func(), error) {
	resolved, err := resolveCragName(cragName)
	if err != nil {
		return nil, "", nil, err
	}

	cragCfg, cragDir, err := instance.Load(resolved)
	if err != nil {
		return nil, "", nil, fmt.Errorf("loading crag %q: %w", resolved, err)
	}

	database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
	if err != nil {
		return nil, "", nil, fmt.Errorf("opening database: %w", err)
	}

	s := store.New(database.Conn())
	cleanup := func() { database.Close() }

	return s, cragCfg.Name, cleanup, nil
}

func newPRListCmd() *cobra.Command {
	var cragName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show all PRs belayer is monitoring",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, cragID, cleanup, err := loadPRDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			prs, err := s.ListMonitoredPullRequests(cragID)
			if err != nil {
				return err
			}

			if len(prs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No monitored PRs.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PR#\tREPO\tCI\tREVIEW\tSTATE\tURL")
			for _, pr := range prs {
				fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\t%s\n",
					pr.PRNumber, pr.RepoName, pr.CIStatus, pr.ReviewStatus, pr.State, pr.URL)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
	return cmd
}

func newPRShowCmd() *cobra.Command {
	var cragName string
	cmd := &cobra.Command{
		Use:   "show <number>",
		Short: "Detailed PR view with checks, reviews, reaction history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, cragID, cleanup, err := loadPRDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			var prNumber int
			fmt.Sscanf(args[0], "%d", &prNumber)

			prs, err := s.ListMonitoredPullRequests(cragID)
			if err != nil {
				return err
			}

			for _, pr := range prs {
				if pr.PRNumber == prNumber {
					fmt.Printf("PR #%d (%s)\n", pr.PRNumber, pr.RepoName)
					fmt.Printf("  URL:      %s\n", pr.URL)
					fmt.Printf("  CI:       %s (fix count: %d)\n", pr.CIStatus, pr.CIFixCount)
					fmt.Printf("  Review:   %s\n", pr.ReviewStatus)
					fmt.Printf("  State:    %s\n", pr.State)
					fmt.Printf("  Problem:  %s\n", pr.ProblemID)

					reactions, _ := s.ListPRReactions(pr.ID)
					if len(reactions) > 0 {
						fmt.Println("\n  Reaction History:")
						for _, r := range reactions {
							fmt.Printf("    [%s] %s -> %s\n",
								r.CreatedAt.Format("2006-01-02 15:04"),
								r.TriggerType, r.ActionTaken)
						}
					}
					return nil
				}
			}

			return fmt.Errorf("PR #%d not found", prNumber)
		},
	}
	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
	return cmd
}

func newPRRetryCmd() *cobra.Command {
	var cragName string
	cmd := &cobra.Command{
		Use:   "retry <number>",
		Short: "Manually trigger a CI fix attempt (bypasses attempt cap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, cragID, cleanup, err := loadPRDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			var prNumber int
			fmt.Sscanf(args[0], "%d", &prNumber)

			prs, err := s.ListMonitoredPullRequests(cragID)
			if err != nil {
				return err
			}

			for _, pr := range prs {
				if pr.PRNumber == prNumber {
					if err := s.UpdatePullRequestCI(pr.ID, pr.CIStatus, 0); err != nil {
						return fmt.Errorf("resetting CI fix count: %w", err)
					}
					fmt.Printf("PR #%d marked for CI fix retry (fix count reset to 0)\n", prNumber)
					return nil
				}
			}

			return fmt.Errorf("PR #%d not found", prNumber)
		},
	}
	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
	return cmd
}
