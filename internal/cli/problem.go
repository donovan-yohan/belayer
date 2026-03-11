package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newProblemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "problem",
		Short: "Manage problems",
	}

	cmd.AddCommand(newProblemCreateCmd())
	cmd.AddCommand(newProblemListCmd())
	return cmd
}

func newProblemCreateCmd() *cobra.Command {
	var specPath string
	var climbsPath string
	var jiraRef string
	var ticketID string
	var cragName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new problem from a spec and climbs file",
		Long:  "Validates spec.md and climbs.json, then writes the problem and climbs to SQLite.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if specPath == "" && ticketID == "" {
				return fmt.Errorf("--spec or --ticket is required")
			}
			if climbsPath == "" {
				return fmt.Errorf("--climbs is required")
			}

			specContent, err := os.ReadFile(specPath)
			if err != nil {
				return fmt.Errorf("reading spec file %q: %w", specPath, err)
			}
			if len(specContent) == 0 {
				return fmt.Errorf("spec file %q is empty", specPath)
			}

			climbsContent, err := os.ReadFile(climbsPath)
			if err != nil {
				return fmt.Errorf("reading climbs file %q: %w", climbsPath, err)
			}

			var climbsFile model.ClimbsFile
			if err := json.Unmarshal(climbsContent, &climbsFile); err != nil {
				return fmt.Errorf("parsing climbs file %q: %w", climbsPath, err)
			}

			if err := store.ValidateClimbsFile(&climbsFile); err != nil {
				return fmt.Errorf("validating climbs: %w", err)
			}

			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			cragConfig, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			var repoNames []string
			for _, repo := range cragConfig.Repos {
				repoNames = append(repoNames, repo.Name)
			}
			if err := store.ValidateClimbsRepos(&climbsFile, repoNames); err != nil {
				return err
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())

			problemID := fmt.Sprintf("problem-%d", time.Now().UnixNano())
			problem := &model.Problem{
				ID:         problemID,
				CragID:     resolvedName,
				Spec:       string(specContent),
				ClimbsJSON: string(climbsContent),
				JiraRef:    jiraRef,
				Status:     model.ProblemStatusPending,
			}

			climbs := store.ClimbsFromFile(problemID, &climbsFile)

			if err := s.InsertProblem(problem, climbs); err != nil {
				return fmt.Errorf("creating problem: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created problem %s (%d climbs across %d repos)\n",
				problemID, len(climbs), len(climbsFile.Repos))
			return nil
		},
	}

	cmd.Flags().StringVar(&specPath, "spec", "", "Path to spec.md file (required unless --ticket is set)")
	cmd.Flags().StringVar(&climbsPath, "climbs", "", "Path to climbs.json file (required)")
	cmd.Flags().StringVar(&jiraRef, "jira", "", "Jira ticket reference (optional)")
	cmd.Flags().StringVar(&ticketID, "ticket", "", "Tracker issue ID to fetch and use as spec")
	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}

func newProblemListCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List problems for a crag",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedName, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			_, cragDir, err := crag.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", resolvedName, err)
			}

			dbPath := filepath.Join(cragDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())
			problems, err := s.ListProblemsForCrag(resolvedName)
			if err != nil {
				return fmt.Errorf("listing problems: %w", err)
			}

			if len(problems) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No problems found.")
				return nil
			}

			for _, p := range problems {
				climbs, _ := s.GetClimbsForProblem(p.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s  %-20s  %d climbs  %s\n",
					p.Status, p.ID, len(climbs), p.CreatedAt.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	return cmd
}
