package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/env"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage development environments",
	}
	cmd.AddCommand(
		newEnvCreateCmd(),
		newEnvAddWorktreeCmd(),
		newEnvRemoveWorktreeCmd(),
		newEnvDestroyCmd(),
		newEnvResetCmd(),
		newEnvStatusCmd(),
		newEnvLogsCmd(),
		newEnvListCmd(),
	)
	return cmd
}

// openCragStore resolves the crag name, loads the crag, and opens the store.
// Returns the store, cragDir, and a cleanup function. The caller must call cleanup().
func openCragStore(cragName string) (*store.Store, string, func(), error) {
	resolvedName, err := resolveCragName(cragName)
	if err != nil {
		return nil, "", nil, err
	}

	_, cragDir, err := crag.Load(resolvedName)
	if err != nil {
		return nil, "", nil, fmt.Errorf("loading crag %q: %w", resolvedName, err)
	}

	database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
	if err != nil {
		return nil, "", nil, fmt.Errorf("opening database: %w", err)
	}

	return store.New(database.Conn()), cragDir, func() { database.Close() }, nil
}

// printJSON marshals v to JSON and prints it, returning any error.
func printJSON(cmd *cobra.Command, v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	return nil
}

func newEnvCreateCmd() *cobra.Command {
	var cragName, name, snapshot string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			s, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := env.Create(s, cragDir, name, snapshot)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created environment %q\n", resp.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Snapshot to restore (optional)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvAddWorktreeCmd() *cobra.Command {
	var cragName, name, repo, branch, baseRef string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "add-worktree",
		Short: "Add a worktree to an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if repo == "" {
				return fmt.Errorf("--repo is required")
			}
			if branch == "" {
				return fmt.Errorf("--branch is required")
			}

			s, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := env.AddWorktree(s, cragDir, name, repo, branch, baseRef)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added worktree for %s at %s\n", resp.Repo, resp.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Repository name (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "Branch name (required)")
	cmd.Flags().StringVar(&baseRef, "base-ref", "", "Base ref to branch from (optional)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvRemoveWorktreeCmd() *cobra.Command {
	var cragName, name, repo, branch string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "remove-worktree",
		Short: "Remove a worktree from an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if repo == "" {
				return fmt.Errorf("--repo is required")
			}
			if branch == "" {
				return fmt.Errorf("--branch is required")
			}

			_, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := env.RemoveWorktree(cragDir, name, repo, branch); err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, map[string]string{"status": "ok"})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed worktree for %s in environment %q\n", repo, name)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "Repository name (required)")
	cmd.Flags().StringVar(&branch, "branch", "", "Branch name (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvDestroyCmd() *cobra.Command {
	var cragName, name string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy an environment and remove all its worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			s, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			if err := env.Destroy(s, cragDir, name); err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, map[string]string{"status": "ok"})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Destroyed environment %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvResetCmd() *cobra.Command {
	var cragName, name, snapshot string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			resp, err := env.Reset(name, snapshot)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reset environment %q\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Snapshot to restore (optional)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvStatusCmd() *cobra.Command {
	var cragName, name string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			s, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := env.Status(s, cragDir, name)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Environment %q: %s (%d worktrees)\n", resp.Name, resp.Status, len(resp.Worktrees))
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvLogsCmd() *cobra.Command {
	var cragName, name, service string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show logs for an environment service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			resp, err := env.Logs(name, service)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "No logs for environment %q service %q\n", name, service)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().StringVar(&name, "name", "", "Environment name (required)")
	cmd.Flags().StringVar(&service, "service", "", "Service name (optional)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newEnvListCmd() *cobra.Command {
	var cragName string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, cragDir, cleanup, err := openCragStore(cragName)
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := env.List(s, cragDir)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, resp)
			}
			if len(resp.Environments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No environments found.")
				return nil
			}
			for _, e := range resp.Environments {
				fmt.Fprintf(cmd.OutOrStdout(), "%-30s  created %s\n", e.Name, e.CreatedAt)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}
