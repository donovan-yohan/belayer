package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage problems",
	}

	cmd.AddCommand(newTaskCreateCmd())
	cmd.AddCommand(newTaskListCmd())
	return cmd
}

func newTaskCreateCmd() *cobra.Command {
	var specPath string
	var climbsPath string
	var jiraRef string
	var instanceName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new problem from a spec and climbs file",
		Long:  "Validates spec.md and climbs.json, then writes the problem and climbs to SQLite.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if specPath == "" {
				return fmt.Errorf("--spec is required")
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

			resolvedName, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			instConfig, instanceDir, err := instance.Load(resolvedName)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", resolvedName, err)
			}

			var repoNames []string
			for _, repo := range instConfig.Repos {
				repoNames = append(repoNames, repo.Name)
			}
			if err := store.ValidateClimbsRepos(&climbsFile, repoNames); err != nil {
				return err
			}

			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())

			problemID := fmt.Sprintf("problem-%d", time.Now().UnixNano())
			problem := &model.Problem{
				ID:         problemID,
				InstanceID: resolvedName,
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

	cmd.Flags().StringVar(&specPath, "spec", "", "Path to spec.md file (required)")
	cmd.Flags().StringVar(&climbsPath, "climbs", "", "Path to climbs.json file (required)")
	cmd.Flags().StringVar(&jiraRef, "jira", "", "Jira ticket reference (optional)")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}

func newTaskListCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List problems for an instance",
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
			problems, err := s.ListProblemsForInstance(resolvedName)
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

	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}
