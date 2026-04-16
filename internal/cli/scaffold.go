package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newScaffoldCmd(use, short, nextStep string) *cobra.Command {
	return &cobra.Command{
		Use:          use,
		Short:        short,
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is reserved in the v6 baseline.\n%s\n", cmd.CommandPath(), nextStep)
		},
	}
}
