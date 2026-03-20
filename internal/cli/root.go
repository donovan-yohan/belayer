package cli

import (
	"github.com/spf13/cobra"

	v2cli "github.com/donovan-yohan/belayer/internal/v2/cli"
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
		newCragCmd(),
		newExplorerSessionCmd(),
		newProblemCmd(),
		newStatusCmd(),
		newBelayerDaemonCmd(),
		newLogsCmd(),
		newSetterSessionCmd(),
		newMailCmd(),
		newMessageCmd(),
		newTrackerCmd(),
		newPRCmd(),
		newConfigCmd(),
		newEnvCmd(),
		newLearningsCmd(),
		v2cli.NewV2Cmd(),
	)

	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
