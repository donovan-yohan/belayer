package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "belayer",
		Short:        "Belayer — agent control plane for Nightshift",
		SilenceUsage: true,
		Long: `Belayer v7 — run-local agent control plane for Nightshift.

Coordinates supervisor + specialist agents via the Hermes bridge within a single
worker run. Daemon manages sessions, agent roster, messages, events, and artifacts
over SQLite.`,
	}

	cmd.Version = version
	cmd.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newSessionCmd(),
		newLogsCmd(),
		newStatusCmd(),
		newRecallCmd(),
		newSpawnCmd(),
		newFinishCmd(),
		newRosterCmd(),
		newMessageCmd(),
		newRequestCompletionCmd(),
		newRunCmd(),
		newArtifactCmd(),
		newInitCmd(),
		newArchiveCmd(),
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
