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
				return fmt.Errorf("BELAYER_SESSION_ID is not set; context is only available inside a session sandbox")
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
