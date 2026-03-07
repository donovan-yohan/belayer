package cli

import (
	"fmt"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/spf13/cobra"
)

func newInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Manage belayer instances",
	}

	cmd.AddCommand(
		newInstanceCreateCmd(),
		newInstanceListCmd(),
		newInstanceDeleteCmd(),
	)
	return cmd
}

func newInstanceCreateCmd() *cobra.Command {
	var repos []string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if len(repos) == 0 {
				return fmt.Errorf("at least one repo URL is required (use --repos)")
			}

			instanceDir, err := instance.Create(name, repos)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created instance %q at %s\n", name, instanceDir)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repos", nil, "Comma-separated list of repository URLs")
	return cmd
}

func newInstanceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all instances",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := instance.List()
			if err != nil {
				return err
			}

			if len(instances) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No instances found. Create one with: belayer instance create <name> --repos <url>")
				return nil
			}

			for name, path := range instances {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, path)
			}
			return nil
		},
	}
}

func newInstanceDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an instance",
		Aliases: []string{"rm"},
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if err := instance.Delete(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted instance %q\n", name)
			return nil
		},
	}
}
