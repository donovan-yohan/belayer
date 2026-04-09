package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "belayer",
		Short:        "Belayer v6 session-runtime scaffold",
		SilenceUsage: true,
		Long: `Belayer is on the v6 clean-break baseline.

Legacy v5 orchestration code (Temporal workers, YAML pipelines, framework installers,
plugin registries, and vendor adapters) has been removed from this branch.

What remains today:
  - CLI entrypoint scaffolding
  - Shared model types
  - Shared event types and logger
  - Documentation for the planned v6 session runtime

Use this branch as the base for all v6 implementation work.`,
	}

	cmd.Version = version
	cmd.AddCommand(
		newVersionCmd(),
		newScaffoldCmd("climb", "Reserved v6 task/session entrypoint", "Implement the v6 session runtime before restoring climb behavior."),
		newScaffoldCmd("logs", "Reserved v6 runtime log viewer", "Restore log plumbing once the daemon/session model exists."),
		newScaffoldCmd("node-complete", "Reserved v6 completion hook surface", "Reintroduce completion semantics as part of the v6 runtime contract."),
		newScaffoldCmd("setup", "Reserved v6 bootstrap command", "Replace framework installation with session-runtime bootstrap once the new design lands."),
		newScaffoldCmd("start", "Reserved v6 operator session entrypoint", "Wire start to the new daemon/session runtime instead of the removed v5 worker flow."),
		newScaffoldCmd("status", "Reserved v6 runtime status command", "Reconnect status once runtime state is backed by SQLite."),
		newScaffoldCmd("submit", "Reserved v6 submission surface", "Restore submit after the new runtime decides how tasks enter the system."),
		newScaffoldCmd("worker", "Reserved v6 daemon command", "Implement the v6 daemon/supervisor instead of the removed Temporal worker."),
	)
	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the belayer build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
