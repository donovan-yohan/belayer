package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage belayer sessions",
	}
	cmd.AddCommand(
		newSessionCreateCmd(),
		newSessionListCmd(),
		newSessionStopCmd(),
	)
	return cmd
}

func newSessionCreateCmd() *cobra.Command {
	var name, template, socket string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			c := NewClient(resolveSocket(socket))
			sess, err := c.CreateSession(name, template)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created session %s (%s)\n", sess.ID, sess.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Session name (required)")
	cmd.Flags().StringVar(&template, "template", "", "Session template (explore, climb, summit)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newSessionListCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			sessions, err := c.ListSessions()
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSTATUS\tTEMPLATE\tCREATED")
			for _, s := range sessions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					s.ID[:8], s.Name, s.Status, s.Template, s.CreatedAt.Format("15:04:05"))
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newSessionStopCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "stop <session-id>",
		Short: "Stop a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			sess, err := c.UpdateSession(args[0], "stopped")
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stopped session %s (%s)\n", sess.ID, sess.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <session-name>",
		Short: "Attach to a session's tmux pane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tmuxCmd := exec.Command("tmux", "attach-session", "-t", "belayer-"+args[0])
			tmuxCmd.Stdin = os.Stdin
			tmuxCmd.Stdout = os.Stdout
			tmuxCmd.Stderr = os.Stderr
			return tmuxCmd.Run()
		},
	}
	return cmd
}

func newLogsCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "logs <session-id>",
		Short: "Show session events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			events, err := c.GetEvents(args[0])
			if err != nil {
				return fmt.Errorf("get events: %w", err)
			}
			for _, e := range events {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %-24s  %s\n",
					e.Timestamp.Format("15:04:05.000"), e.Type, e.Data)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newStatusCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show running sessions with status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			if err := c.Health(); err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon: offline")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon: online")

			sessions, err := c.ListSessions()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No active sessions.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSTATUS\tTEMPLATE")
			for _, s := range sessions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID[:8], s.Name, s.Status, s.Template)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newRecallCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "Search events via FTS5",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Recall searches directly via daemon search endpoint.
			// For now, we search events across all sessions.
			c := NewClient(resolveSocket(socket))
			resp, err := c.do("GET", "/search?q="+url.QueryEscape(args[0]), nil)
			if err != nil {
				// Fallback: search per-session
				sessions, err2 := c.ListSessions()
				if err2 != nil {
					return fmt.Errorf("recall: %w", err)
				}
				for _, s := range sessions {
					events, err3 := c.GetEvents(s.ID)
					if err3 != nil {
						continue
					}
					for _, e := range events {
						data, _ := json.Marshal(e)
						raw := string(data)
						if strings.Contains(raw, args[0]) {
							fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s  %s  %s\n",
								s.Name, e.Timestamp.Format("15:04:05"), e.Type, e.Data)
						}
					}
				}
				return nil
			}
			defer resp.Body.Close()
			var events []eventResponse
			json.NewDecoder(resp.Body).Decode(&events)
			for _, e := range events {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n",
					e.Timestamp.Format("15:04:05"), e.Type, e.Data)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func resolveSocket(override string) string {
	if override != "" {
		return override
	}
	return DefaultSocketPath()
}
