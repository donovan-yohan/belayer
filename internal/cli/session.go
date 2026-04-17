package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage belayer sessions",
	}
	cmd.AddCommand(
		newSessionListCmd(),
		newSessionStopCmd(),
	)
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
		Use:   "stop <session-id-or-name>",
		Short: "Stop a running session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))

			sessionID, err := lookupSessionID(c, args[0])
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			sess, err := c.UpdateSession(sessionID, "stopped")
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			_ = c.LogEvent(sessionID, "session_completed", mustJSON(map[string]string{
				"name":   sess.Name,
				"status": "stopped",
			}))

			fmt.Fprintf(cmd.OutOrStdout(), "Stopped session %s (%s)\n", sess.ID, sess.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

// lookupSessionID resolves a session name, ID prefix, or full ID to a full session ID.
func lookupSessionID(c *Client, arg string) (string, error) {
	sessions, err := c.ListSessions()
	if err != nil {
		return arg, nil
	}
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, arg) || s.Name == arg {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no session found matching %q", arg)
}

func newLogsCmd() *cobra.Command {
	var socket string
	var follow bool
	var since int

	cmd := &cobra.Command{
		Use:   "logs <session-id>",
		Short: "Show session events",
		Long:  "Show session events. Use --follow to tail in real-time.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			sessionID := args[0]

			if resolved, err := lookupSessionID(c, sessionID); err == nil {
				sessionID = resolved
			}

			events, err := c.GetEvents(sessionID)
			if err != nil {
				return fmt.Errorf("get events: %w", err)
			}

			cutoff := time.Time{}
			if since > 0 {
				cutoff = time.Now().Add(-time.Duration(since) * time.Minute)
			}

			lastSeen := time.Time{}
			var lastSeenID int64
			for _, e := range events {
				if !cutoff.IsZero() && e.Timestamp.Before(cutoff) {
					continue
				}
				printEvent(cmd, e)
				if e.Timestamp.After(lastSeen) {
					lastSeen = e.Timestamp
				}
				if e.ID > lastSeenID {
					lastSeenID = e.ID
				}
			}

			if !follow {
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "--- following (Ctrl+C to stop) ---")
			for {
				events, err := c.GetEventsAfter(sessionID, lastSeenID, 30*time.Second)
				if err != nil {
					continue
				}

				for _, e := range events {
					printEvent(cmd, e)
					if e.Timestamp.After(lastSeen) {
						lastSeen = e.Timestamp
					}
					if e.ID > lastSeenID {
						lastSeenID = e.ID
					}
				}
			}
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow events in real-time")
	cmd.Flags().IntVar(&since, "since", 0, "Show events from the last N minutes")
	return cmd
}

func newWatchCmd() *cobra.Command {
	var (
		socket       string
		sessionsFlag string
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream events from one or more sessions as they happen",
		Long:  "Stream events from one or more sessions via the daemon SSE endpoint.",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionArgs := strings.TrimSpace(sessionsFlag)
			if sessionArgs == "" {
				sessionArgs = strings.Join(args, ",")
			}
			if sessionArgs == "" {
				return fmt.Errorf("--sessions is required")
			}

			c := NewClient(resolveSocket(socket))
			rawSessions := strings.Split(sessionArgs, ",")
			sessionIDs := make([]string, 0, len(rawSessions))
			var afterID int64
			for _, raw := range rawSessions {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				sessionID, err := lookupSessionID(c, raw)
				if err != nil {
					return err
				}
				sessionIDs = append(sessionIDs, sessionID)
				events, err := c.GetEvents(sessionID)
				if err == nil {
					for _, evt := range events {
						if evt.ID > afterID {
							afterID = evt.ID
						}
					}
				}
			}
			if len(sessionIDs) == 0 {
				return fmt.Errorf("no sessions resolved")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Fprintf(cmd.OutOrStdout(), "Watching sessions: %s\n", strings.Join(sessionIDs, ", "))
			return c.WatchSessions(ctx, sessionIDs, afterID, func(evt eventResponse) error {
				printEvent(cmd, evt)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&sessionsFlag, "sessions", "", "Comma-separated session IDs or names")
	return cmd
}

func printEvent(cmd *cobra.Command, e eventResponse) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s  %-24s  %s\n",
		e.Timestamp.Format("15:04:05.000"), e.Type, e.Data)
}

func newStatusCmd() *cobra.Command {
	var socket string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show running sessions with status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			if _, err := c.Health(); err != nil {
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
			c := NewClient(resolveSocket(socket))
			resp, err := c.do("GET", "/search?q="+url.QueryEscape(args[0]), nil)
			if err != nil || resp.StatusCode != http.StatusOK {
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
	if envSocket := os.Getenv("BELAYER_SOCKET"); envSocket != "" {
		return envSocket
	}
	return DefaultSocketPath()
}
