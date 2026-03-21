package cli

import "github.com/spf13/cobra"

func RegisterV3Commands(root *cobra.Command) {
	root.AddCommand(
		NewClimbCmd(),
		NewNodeCompleteCmd(),
		newStatusCmd(),
		newWorkerCmd(),
		newStartCmd(),
	)
}
