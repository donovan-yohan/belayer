package cli

import (
	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "belayer",
		Short: "Multi-repo coding agent orchestrator",
		Long:  "Belayer orchestrates autonomous coding agents across multiple repositories, decomposing work items into per-repo tasks and validating cross-repo alignment.",
	}

	cmd.Version = version

	cmd.AddCommand(
		newInitCmd(),
		newInstanceCmd(),
		newTaskCmd(),
		newStatusCmd(),
		newSetterCmd(),
		newLogsCmd(),
		newManageCmd(),
		newMailCmd(),
		newMessageCmd(),
	)

	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
