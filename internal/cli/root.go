package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "belayer",
		Short:        "Orchestrate autonomous coding agents through sessions",
		SilenceUsage: true,
		Long: `Belayer v6 — session runtime for autonomous coding agents.

Commands:
  daemon    Start the belayer daemon (long-running supervisor)
  session   Create, list, and stop agent sessions
  attach    Attach to a session's tmux pane
  logs      Show session events
  status    Show running sessions
  recall    Search events via FTS5`,
	}

	cmd.Version = version
	cmd.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newSessionCmd(),
		newAttachCmd(),
		newLogsCmd(),
		newStatusCmd(),
		newRecallCmd(),
		newMessageCmd(),
		newContextCmd(),
		newNoteCmd(),
		newScaffoldCmd("climb", "Reserved v6 task/session entrypoint", "Implement the v6 session runtime before restoring climb behavior."),
		newScaffoldCmd("setup", "Reserved v6 bootstrap command", "Replace framework installation with session-runtime bootstrap once the new design lands."),
		newScaffoldCmd("submit", "Reserved v6 submission surface", "Restore submit after the new runtime decides how tasks enter the system."),
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
