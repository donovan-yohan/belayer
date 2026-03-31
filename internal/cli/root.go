package cli

import "github.com/spf13/cobra"

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "belayer",
		Short: "Pipeline orchestrator for autonomous coding agents",
		Long: `Belayer orchestrates autonomous coding agents through a declarative pipeline.

Define your pipeline in YAML, install a framework (belayer setup --framework),
and belayer handles execution via Temporal workflows.

Getting started:
  belayer setup --framework gstack        Install a framework
  belayer climb "description"             Start a pipeline run
  belayer status                          Check pipeline progress`,
	}

	cmd.Version = version

	cmd.AddCommand(
		NewClimbCmd(),
		NewNodeCompleteCmd(),
		newStatusCmd(),
		newWorkerCmd(),
		newStartCmd(),
		newSetupCmd(),
		newSubmitCmd(),
	)

	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
