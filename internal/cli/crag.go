package cli

import (
	"fmt"

	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/repo"
	"github.com/spf13/cobra"
)

func newCragCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crag",
		Short: "Manage belayer crags",
	}

	cmd.AddCommand(
		newCragCreateCmd(),
		newCragListCmd(),
		newCragDeleteCmd(),
	)
	return cmd
}

func newCragCreateCmd() *cobra.Command {
	var repos []string
	var localPaths bool

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new crag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if len(repos) == 0 {
				return fmt.Errorf("at least one repo source is required (use --repos)")
			}

			for _, repoSource := range repos {
				if err := repo.ValidateRepoSource(repoSource, localPaths); err != nil {
					return err
				}
			}

			cragDir, err := crag.Create(name, repos)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created crag %q at %s\n", name, cragDir)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repos", nil, "Comma-separated or repeated list of repository clone sources")
	cmd.Flags().BoolVar(&localPaths, "local-paths", false, "Allow local filesystem paths in --repos")
	return cmd
}

func newCragListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all crags",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			crags, err := crag.List()
			if err != nil {
				return err
			}

			if len(crags) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No crags found. Create one with: belayer crag create <name> --repos <repo-source>")
				return nil
			}

			for name, path := range crags {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, path)
			}
			return nil
		},
	}
}

func newCragDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name>",
		Short:   "Delete a crag",
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if err := crag.Delete(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted crag %q\n", name)
			return nil
		},
	}
}
