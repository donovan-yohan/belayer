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
		Short: "Manage tasks",
	}

	cmd.AddCommand(newTaskCreateCmd())
	cmd.AddCommand(newTaskListCmd())
	return cmd
}

func newTaskCreateCmd() *cobra.Command {
	var specPath string
	var goalsPath string
	var jiraRef string
	var instanceName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new task from a spec and goals file",
		Long:  "Validates spec.md and goals.json, then writes the task and goals to SQLite.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if specPath == "" {
				return fmt.Errorf("--spec is required")
			}
			if goalsPath == "" {
				return fmt.Errorf("--goals is required")
			}

			specContent, err := os.ReadFile(specPath)
			if err != nil {
				return fmt.Errorf("reading spec file %q: %w", specPath, err)
			}
			if len(specContent) == 0 {
				return fmt.Errorf("spec file %q is empty", specPath)
			}

			goalsContent, err := os.ReadFile(goalsPath)
			if err != nil {
				return fmt.Errorf("reading goals file %q: %w", goalsPath, err)
			}

			var goalsFile model.GoalsFile
			if err := json.Unmarshal(goalsContent, &goalsFile); err != nil {
				return fmt.Errorf("parsing goals file %q: %w", goalsPath, err)
			}

			if err := store.ValidateGoalsFile(&goalsFile); err != nil {
				return fmt.Errorf("validating goals: %w", err)
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
			if err := store.ValidateGoalsRepos(&goalsFile, repoNames); err != nil {
				return err
			}

			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			s := store.New(database.Conn())

			taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
			task := &model.Task{
				ID:         taskID,
				InstanceID: resolvedName,
				Spec:       string(specContent),
				GoalsJSON:  string(goalsContent),
				JiraRef:    jiraRef,
				Status:     model.TaskStatusPending,
			}

			goals := store.GoalsFromFile(taskID, &goalsFile)

			if err := s.InsertTask(task, goals); err != nil {
				return fmt.Errorf("creating task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created task %s (%d goals across %d repos)\n",
				taskID, len(goals), len(goalsFile.Repos))
			return nil
		},
	}

	cmd.Flags().StringVar(&specPath, "spec", "", "Path to spec.md file (required)")
	cmd.Flags().StringVar(&goalsPath, "goals", "", "Path to goals.json file (required)")
	cmd.Flags().StringVar(&jiraRef, "jira", "", "Jira ticket reference (optional)")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}

func newTaskListCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks for an instance",
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
			tasks, err := s.ListTasksForInstance(resolvedName)
			if err != nil {
				return fmt.Errorf("listing tasks: %w", err)
			}

			if len(tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tasks found.")
				return nil
			}

			for _, t := range tasks {
				goals, _ := s.GetGoalsForTask(t.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s  %-20s  %d goals  %s\n",
					t.Status, t.ID, len(goals), t.CreatedAt.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}
