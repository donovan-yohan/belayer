package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}

	cmd.AddCommand(newTaskCreateCmd())
	return cmd
}

func newTaskCreateCmd() *cobra.Command {
	var jira string

	cmd := &cobra.Command{
		Use:   "create [description]",
		Short: "Create a new task",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jira != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "task create: not implemented yet (jira=%s)\n", jira)
			} else if len(args) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "task create: not implemented yet (description=%s)\n", args[0])
			} else {
				return fmt.Errorf("provide a description or --jira flag")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&jira, "jira", "", "Jira ticket ID(s)")
	return cmd
}
