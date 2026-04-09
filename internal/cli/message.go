package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// resolveSessionID returns BELAYER_SESSION_ID if set, otherwise the --session flag value.
// Returns an error if neither is available.
func resolveSessionID(flagVal string) (string, error) {
	if id := os.Getenv("BELAYER_SESSION_ID"); id != "" {
		return id, nil
	}
	if flagVal != "" {
		return flagVal, nil
	}
	return "", fmt.Errorf("BELAYER_SESSION_ID is not set and --session flag is required")
}

// senderID returns BELAYER_AGENT_ID if set, otherwise "operator".
func senderID() string {
	if id := os.Getenv("BELAYER_AGENT_ID"); id != "" {
		return id
	}
	return "operator"
}

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "Send and list messages within a session",
	}
	cmd.AddCommand(
		newMessageSendCmd(),
		newMessageBroadcastCmd(),
		newMessageListCmd(),
	)
	return cmd
}

func newMessageSendCmd() *cobra.Command {
	var to, session, socket string
	var interrupt bool

	cmd := &cobra.Command{
		Use:   "send \"text\"",
		Short: "Send a message to an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			if to == "" {
				return fmt.Errorf("--to is required")
			}

			c := NewClient(resolveSocket(socket))
			msgID, err := c.SendMessage(sessID, to, args[0], "instruction", interrupt)
			if err != nil {
				return fmt.Errorf("send message: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Sent message %s to %s\n", msgID, to)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Recipient agent ID (required)")
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVar(&interrupt, "interrupt", false, "Send as an interrupt")
	return cmd
}

func newMessageBroadcastCmd() *cobra.Command {
	var session, socket string

	cmd := &cobra.Command{
		Use:   "broadcast \"text\"",
		Short: "Broadcast a message to all agents in the session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}

			c := NewClient(resolveSocket(socket))
			if err := c.BroadcastMessage(sessID, args[0], "instruction"); err != nil {
				return fmt.Errorf("broadcast message: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Broadcast sent")
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newMessageListCmd() *cobra.Command {
	var session, socket string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending messages for the session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}

			c := NewClient(resolveSocket(socket))
			messages, err := c.ListMessages(sessID)
			if err != nil {
				return fmt.Errorf("list messages: %w", err)
			}
			if len(messages) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No messages.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TIME\tTYPE\tDATA")
			for _, m := range messages {
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					m.Timestamp.Format("15:04:05.000"), m.Type, m.Data)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

// messageResponse is the response from the send-message endpoint.
type messageResponse struct {
	ID string `json:"id"`
}

// SendMessage posts a message to a specific agent within a session.
func (c *Client) SendMessage(sessionID, to, content, msgType string, interrupt bool) (string, error) {
	body := map[string]any{
		"to":        to,
		"content":   content,
		"type":      msgType,
		"interrupt": interrupt,
		"from":      senderID(),
	}
	resp, err := c.do("POST", "/sessions/"+sessionID+"/messages", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var r messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return r.ID, nil
}

// BroadcastMessage posts a broadcast message to all agents in the session.
func (c *Client) BroadcastMessage(sessionID, content, msgType string) error {
	body := map[string]any{
		"content": content,
		"type":    msgType,
		"from":    senderID(),
	}
	resp, err := c.do("POST", "/sessions/"+sessionID+"/messages/broadcast", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListMessages returns message events for a session.
type messageListEntry struct {
	Timestamp time.Time `json:"Timestamp"`
	Type      string    `json:"Type"`
	Data      string    `json:"Data"`
}

// ListMessages returns message events for a session.
func (c *Client) ListMessages(sessionID string) ([]messageListEntry, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/messages", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var entries []messageListEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	return entries, nil
}
