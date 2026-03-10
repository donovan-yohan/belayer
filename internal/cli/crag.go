package cli

import (
	"fmt"

	"github.com/donovan-yohan/belayer/internal/instance"
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

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new crag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if len(repos) == 0 {
				return fmt.Errorf("at least one repo URL is required (use --repos)")
			}

			cragDir, err := instance.Create(name, repos)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created crag %q at %s\n", name, cragDir)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repos", nil, "Comma-separated list of repository URLs")
	return cmd
}

func newCragListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all crags",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			crags, err := instance.List()
			if err != nil {
				return err
			}

			if len(crags) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No crags found. Create one with: belayer crag create <name> --repos <url>")
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

			if err := instance.Delete(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted crag %q\n", name)
			return nil
		},
	}
}
