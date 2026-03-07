package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Manage belayer instances",
	}

	cmd.AddCommand(newInstanceCreateCmd())
	return cmd
}

func newInstanceCreateCmd() *cobra.Command {
	var repos []string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "instance create: not implemented yet (name=%s, repos=%v)\n", args[0], repos)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&repos, "repos", nil, "Comma-separated list of repository URLs")
	return cmd
}
