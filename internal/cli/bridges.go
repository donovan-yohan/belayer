package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newBridgesCmd groups bridge-related subcommands under `belayer bridges`.
func newBridgesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bridges",
		Short: "Inspect per-agent bridge subprocess state",
		Long:  "Inspect per-agent bridge subprocess state (raw stdout, status).",
	}
	cmd.AddCommand(newBridgesTailCmd())
	return cmd
}

// newBridgesTailCmd implements `belayer bridges tail <session> <agent>`, a
// shorthand that streams the raw bridge stdout file via the same HTTP
// endpoint `belayer logs --raw --agent` uses. Always runs in follow mode;
// pass --no-follow for a one-shot fetch.
func newBridgesTailCmd() *cobra.Command {
	var socket string
	var follow bool
	cmd := &cobra.Command{
		Use:   "tail <session> <agent>",
		Short: "Tail a single agent's raw bridge stdout file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionArg := args[0]
			agent := args[1]
			if agent == "" {
				return fmt.Errorf("agent name is required")
			}
			c := NewClient(resolveSocket(socket))
			sessionID := sessionArg
			if resolved, err := lookupSessionID(c, sessionArg); err == nil {
				sessionID = resolved
			}
			return c.BridgeStdoutStream(cmd.Context(), sessionID, agent, 0, follow, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVarP(&follow, "follow", "f", true, "Follow the file (default true; pass --follow=false for one-shot)")
	return cmd
}
