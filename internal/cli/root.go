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
  implement   Launch an implementation session (pilot + implementer + reviewer)
  daemon      Start the belayer daemon (long-running supervisor)
  session     Create, list, and stop agent sessions
  attach      Attach to a session's agent tmux panes
  setup       Bootstrap a .belayer/ workspace
  status      Show running sessions
  logs        Show session events
  watch       Stream events from one or more sessions
  recall      Search events via FTS5`,
	}

	cmd.Version = version
	cmd.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newRunCmd(),
		newSessionCmd(),
		newAttachCmd(),
		newLogsCmd(),
		newWatchCmd(),
		newStatusCmd(),
		newDebugCmd(),
		newRecallCmd(),
		newMessageCmd(),
		newArtifactCmd(),
		newRosterCmd(),
		newSpawnCmd(),
		newFinishCmd(),
		newContextCmd(),
		newNoteCmd(),
		newSetupCmd(),
		newToolCmd(),
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
