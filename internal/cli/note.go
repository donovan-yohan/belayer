package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newNoteCmd() *cobra.Command {
	var session, socket string

	cmd := &cobra.Command{
		Use:   "note \"observation text\"",
		Short: "Log an observation note to the session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}

			c := NewClient(resolveSocket(socket))
			if err := c.LogNote(sessID, args[0]); err != nil {
				return fmt.Errorf("log note: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Note logged.")
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

// LogNote posts an agent_note event to the session.
func (c *Client) LogNote(sessionID, text string) error {
	body := map[string]any{
		"type": "agent_note",
		"data": mustJSON(map[string]string{
			"text":  text,
			"agent": senderID(),
		}),
	}
	resp, err := c.do("POST", "/sessions/"+sessionID+"/events", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
