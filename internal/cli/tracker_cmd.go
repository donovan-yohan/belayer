package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/repo"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/tracker"
	ghtracker "github.com/donovan-yohan/belayer/internal/tracker/github"
	jiratracker "github.com/donovan-yohan/belayer/internal/tracker/jira"
	"github.com/spf13/cobra"
)

func newTrackerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tracker",
		Short: "Manage issue tracker integration",
	}
	cmd.AddCommand(newTrackerSyncCmd())
	cmd.AddCommand(newTrackerListCmd())
	cmd.AddCommand(newTrackerShowCmd())
	return cmd
}

// createTracker creates the appropriate Tracker implementation from config.
func createTracker(cfg *belayerconfig.Config, cragConfig *instance.CragConfig, cragDir string) (tracker.Tracker, error) {
	switch cfg.Tracker.Provider {
	case "github":
		if len(cragConfig.Repos) == 0 {
			return nil, fmt.Errorf("no repos in crag for github tracker")
		}
		ownerRepo, err := repo.OwnerRepoFromURL(cragConfig.Repos[0].URL)
		if err != nil {
			return nil, fmt.Errorf("extracting owner/repo for tracker: %w", err)
		}
		return ghtracker.New(ownerRepo), nil
	case "jira":
		token := os.Getenv("JIRA_API_TOKEN")
		return jiratracker.New(cfg.Tracker.Jira.BaseURL, cfg.Tracker.Jira.Project, token), nil
	default:
		return nil, fmt.Errorf("unknown tracker provider: %q", cfg.Tracker.Provider)
	}
}

// loadTrackerDeps loads crag config, belayer config, opens the DB, and creates a tracker.
// It returns the tracker, store, cragConfig, and a cleanup func.
func loadTrackerDeps(cragName string) (tracker.Tracker, *store.Store, *instance.CragConfig, func(), error) {
	resolved, err := resolveCragName(cragName)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	cragConfig, cragDir, err := instance.Load(resolved)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("loading crag %q: %w", resolved, err)
	}

	globalDir, err := config.Dir()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("resolving config dir: %w", err)
	}

	bcfg, err := belayerconfig.Load(globalDir, cragDir)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("loading belayer config: %w", err)
	}

	database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("opening database: %w", err)
	}

	s := store.New(database.Conn())
	cleanup := func() { database.Close() }

	t, err := createTracker(bcfg, cragConfig, cragDir)
	if err != nil {
		database.Close()
		return nil, nil, nil, nil, fmt.Errorf("creating tracker: %w", err)
	}

	return t, s, cragConfig, cleanup, nil
}

func newTrackerSyncCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch issues from the tracker and import them into the local DB",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, s, _, cleanup, err := loadTrackerDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			issues, err := t.ListIssues(context.Background(), model.IssueFilter{})
			if err != nil {
				return fmt.Errorf("listing issues: %w", err)
			}

			now := time.Now().UTC()
			for _, issue := range issues {
				commentsJSON, _ := json.Marshal(issue.Comments)
				labelsJSON, _ := json.Marshal(issue.Labels)
				rawJSON, _ := json.Marshal(issue.Raw)

				ti := &model.TrackerIssue{
					ID:           issue.ID,
					Title:        issue.Title,
					Body:         issue.Body,
					CommentsJSON: string(commentsJSON),
					LabelsJSON:   string(labelsJSON),
					Priority:     issue.Priority,
					Assignee:     issue.Assignee,
					URL:          issue.URL,
					RawJSON:      string(rawJSON),
					SyncedAt:     now,
				}
				if err := s.InsertTrackerIssue(ti); err != nil {
					return fmt.Errorf("inserting issue %s: %w", issue.ID, err)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d issue(s)\n", len(issues))
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}

func newTrackerListCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Dry-run preview of matching tracker issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, _, _, cleanup, err := loadTrackerDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			issues, err := t.ListIssues(context.Background(), model.IssueFilter{})
			if err != nil {
				return fmt.Errorf("listing issues: %w", err)
			}

			if len(issues) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No issues found.")
				return nil
			}

			for _, issue := range issues {
				labels := strings.Join(issue.Labels, ", ")
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s  %-50s  [%s]\n", issue.ID, truncate(issue.Title, 50), labels)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}

func newTrackerShowCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "show <issue-id>",
		Short: "Display details for a single tracker issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, _, _, cleanup, err := loadTrackerDeps(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			issue, err := t.GetIssue(context.Background(), args[0])
			if err != nil {
				return fmt.Errorf("fetching issue %s: %w", args[0], err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "ID:       %s\n", issue.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Title:    %s\n", issue.Title)
			fmt.Fprintf(cmd.OutOrStdout(), "URL:      %s\n", issue.URL)
			fmt.Fprintf(cmd.OutOrStdout(), "Labels:   %s\n", strings.Join(issue.Labels, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "Assignee: %s\n", issue.Assignee)
			if issue.Priority != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Priority: %s\n", issue.Priority)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", issue.Body)

			if len(issue.Comments) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\n--- Comments ---")
				for _, c := range issue.Comments {
					fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Date, c.Author, c.Body)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
