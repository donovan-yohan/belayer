package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newContextCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Show session context (messaging plane only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID := os.Getenv("BELAYER_SESSION_ID")
			if sessID == "" {
				// Not inside a session sandbox; show management-plane view
				fmt.Fprintf(cmd.OutOrStdout(), "No active session context (BELAYER_SESSION_ID not set).\n\n")
				fmt.Fprintf(cmd.OutOrStdout(), "You are on the management plane. Use these commands:\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  belayer status            Show daemon health and sessions\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  belayer session create    Create a new session\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  belayer session list      List all sessions\n\n")
				fmt.Fprintf(cmd.OutOrStdout(), "To get session context, run from inside a belayer agent sandbox\n")
				fmt.Fprintf(cmd.OutOrStdout(), "or set BELAYER_SESSION_ID manually for testing.\n")
				return nil
			}

			c := NewClient(resolveSocket(socket))
			sess, err := c.GetSession(sessID)
			if err != nil {
				return fmt.Errorf("get session: %w", err)
			}

			out := map[string]any{
				"session_id": sess.ID,
				"name":       sess.Name,
				"status":     sess.Status,
				"template":   sess.Template,
				"agent_id":   os.Getenv("BELAYER_AGENT_ID"),
			}

			// Add note if BELAYER_AGENT_ID is not set
			if os.Getenv("BELAYER_AGENT_ID") == "" {
				out["note"] = "BELAYER_AGENT_ID not set — defaulting to 'operator'"
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(out); err != nil {
				return fmt.Errorf("encode context: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}
