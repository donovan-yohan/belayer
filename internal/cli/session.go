package cli

import (
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

// lookupSessionID resolves a session name, ID prefix, or full ID to a full
// session ID. Exact full-ID or exact name matches always win. Otherwise we
// require exactly one prefix match — ambiguous prefixes are rejected so
// commands do not silently address the wrong session.
func lookupSessionID(c *Client, arg string) (string, error) {
	sessions, err := c.ListSessions()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	return resolveSessionArg(sessions, arg)
}

// resolveSessionArg applies the name/ID/prefix resolution rules to an
// in-memory session slice. Extracted so tests can exercise the ambiguity
// branch without a live daemon.
func resolveSessionArg(sessions []sessionResponse, arg string) (string, error) {
	var prefixMatches []string
	for _, s := range sessions {
		if s.ID == arg || s.Name == arg {
			return s.ID, nil
		}
		if strings.HasPrefix(s.ID, arg) {
			prefixMatches = append(prefixMatches, s.ID)
		}
	}
	switch len(prefixMatches) {
	case 0:
		return "", fmt.Errorf("no session found matching %q", arg)
	case 1:
		return prefixMatches[0], nil
	default:
		return "", fmt.Errorf("ambiguous session identifier %q (matches %d sessions: %s)",
			arg, len(prefixMatches), strings.Join(prefixMatches, ", "))
	}
}

func newLogsCmd() *cobra.Command {
	var socket string
	var follow bool
	var since time.Duration
	var rawMode bool
	var agentName string
	var typePrefix string
	var tier string
	var format string
	var tail int
	var noColor bool

	cmd := &cobra.Command{
		Use:   "logs <session-id>",
		Short: "Show session events",
		Long:  "Show session events. Use --follow to tail in real-time." + ChannelsFooter,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if rawMode {
				if agentName == "" {
					return fmt.Errorf("--raw requires --agent <name>")
				}
				if since > 0 {
					return fmt.Errorf("--raw cannot be combined with --since (raw mode serves bytes, not events)")
				}
				c := NewClient(resolveSocket(socket))
				sessionID := args[0]
				if resolved, err := lookupSessionID(c, sessionID); err == nil {
					sessionID = resolved
				}
				return c.BridgeStdoutStream(cmd.Context(), sessionID, agentName, 0, follow, cmd.OutOrStdout())
			}

			if err := validateFormat(format); err != nil {
				return err
			}

			c := NewClient(resolveSocket(socket))
			sessionID := args[0]
			if resolved, err := lookupSessionID(c, sessionID); err == nil {
				sessionID = resolved
			}

			events, err := c.GetEvents(sessionID)
			if err != nil {
				return fmt.Errorf("get events: %w", err)
			}

			// Apply client-side filters for backfill. The SSE path uses
			// server-side filters; we mirror them here so one-shot mode matches
			// follow mode semantics.
			filtered := make([]eventResponse, 0, len(events))
			cutoff := time.Time{}
			if since > 0 {
				cutoff = time.Now().Add(-since)
			}
			for _, e := range events {
				if !cutoff.IsZero() && e.Timestamp.Before(cutoff) {
					continue
				}
				if !matchesFilters(e, agentName, typePrefix, tier) {
					continue
				}
				filtered = append(filtered, e)
			}
			if tail > 0 && len(filtered) > tail {
				filtered = filtered[len(filtered)-tail:]
			}

			var lastSeenID int64
			for _, e := range filtered {
				printEventFormat(cmd, e, format, noColor)
				if e.ID > lastSeenID {
					lastSeenID = e.ID
				}
			}

			if !follow {
				return nil
			}

			// Track highest ID across unfiltered backfill so follow resumes from
			// the true tip of the log, not just the last matching event.
			for _, e := range events {
				if e.ID > lastSeenID {
					lastSeenID = e.ID
				}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return c.SubscribeEvents(ctx, sessionID, EventFilters{
				Agent:      agentName,
				TypePrefix: typePrefix,
				Tier:       tier,
				AfterID:    lastSeenID,
			}, func(e eventResponse) error {
				printEventFormat(cmd, e, format, noColor)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow events in real-time via SSE")
	cmd.Flags().DurationVar(&since, "since", 0, "Show events from the last duration (e.g. 10m, 1h)")
	cmd.Flags().BoolVar(&rawMode, "raw", false, "Tail raw bridge stdout file for --agent")
	cmd.Flags().StringVar(&agentName, "agent", "", "Filter events by agent name (server-side in follow mode)")
	cmd.Flags().StringVar(&typePrefix, "type", "", "Filter events by type prefix (e.g. bridge:, trace:)")
	cmd.Flags().StringVar(&tier, "tier", "", "Cap events at log tier (standard|verbose|trace)")
	cmd.Flags().StringVar(&format, "format", "pretty", "Output format (pretty|ndjson). 'json' is accepted as an alias for ndjson — both emit one JSON object per line so --follow can stream incrementally; a true JSON array would require buffering the whole stream. Pipe through `jq -s` if you need an array.")
	cmd.Flags().IntVar(&tail, "tail", 0, "Limit backfill to the last N matching events")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable ANSI color (auto when stdout is not a TTY)")
	return cmd
}

func validateFormat(format string) error {
	switch format {
	case "", "pretty", "ndjson", "json":
		return nil
	}
	return fmt.Errorf("unknown --format %q (want pretty|ndjson|json)", format)
}

// matchesFilters mirrors the server's SSE output filters for client-side
// backfill. agent == "" matches any agent; typePrefix == "" matches any type;
// tier == "" disables the tier cap.
func matchesFilters(e eventResponse, agent, typePrefix, tier string) bool {
	if typePrefix != "" && !strings.HasPrefix(e.Type, typePrefix) {
		return false
	}
	if agent != "" {
		// Parse e.Data as JSON and read the top-level "agent" field. A prior
		// substring peek was cheaper but also matched embedded JSON-in-JSON
		// (e.g. a supervisor message whose content field contained an escaped
		// "agent":"pm"), yielding false positives that crossed agent scope.
		var payload struct {
			Agent string `json:"agent"`
		}
		if err := json.Unmarshal([]byte(e.Data), &payload); err != nil {
			return false
		}
		if payload.Agent != agent {
			return false
		}
	}
	if tier != "" {
		cap := tierRank(tier)
		if cap < 0 {
			// Unknown tier name — behave as "no cap" to match SSE which rejects
			// bad values at the HTTP layer; backfill is best-effort here.
			return true
		}
		if tierRank(eventTier(e)) > cap {
			return false
		}
	}
	return true
}

// eventTier mirrors the server's classification (internal/daemon.eventTier) so
// --tier filtering is consistent between one-shot backfill and --follow.
func eventTier(e eventResponse) string {
	if e.TraceFile != "" ||
		strings.HasPrefix(e.Type, "trace:") ||
		strings.Contains(e.Data, `"full_input":`) ||
		strings.Contains(e.Data, `"full_result":`) {
		return "trace"
	}
	if strings.HasPrefix(e.Type, "verbose:") ||
		strings.HasPrefix(e.Type, "agent_status:") ||
		strings.HasPrefix(e.Type, "bridge:") {
		return "verbose"
	}
	return "standard"
}

// tierRank matches internal/daemon.tierRank. Returns -1 for unknown names.
func tierRank(tier string) int {
	switch tier {
	case "standard":
		return 0
	case "verbose":
		return 1
	case "trace":
		return 2
	}
	return -1
}

func printEventFormat(cmd *cobra.Command, e eventResponse, format string, noColor bool) {
	switch format {
	case "ndjson", "json":
		// Both batch and streaming modes emit one object per line — "json" as
		// a single array would require buffering the entire follow stream,
		// which defeats the purpose of --follow. Consumers who want an array
		// can pipe ndjson through `jq -s`.
		b, _ := json.Marshal(e)
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	default:
		printEvent(cmd, e)
	}
	_ = noColor
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
			fmt.Fprintln(w, "ID\tNAME\tSTATUS\tTEMPLATE\tLOG")
			for _, s := range sessions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID[:8], s.Name, s.Status, s.Template, s.LogLevel)
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
