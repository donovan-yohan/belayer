package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		newSessionStartCmd(),
		newSessionListCmd(),
		newSessionStopCmd(),
		newSessionWakeCmd(),
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
	cmd.Flags().StringVar(&template, "template", "", "Session template (intake, implement, deliver)")
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
	var force bool

	cmd := &cobra.Command{
		Use:   "stop <session-id-or-name>",
		Short: "Stop a session and terminate its agent tmux panes",
		Long: `Stop a belayer session and kill all associated agent tmux sessions.

Before terminating, checks each agent's working directory for uncommitted
git changes. If uncommitted changes are found, the command will abort
unless --force is specified.

WARNING: --force will terminate agents even if they have uncommitted code
changes. Any work not committed will be lost.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))

			// Resolve name-or-prefix to full session ID.
			sessionID, err := lookupSessionID(c, args[0])
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			// Get session info before stopping.
			sess, err := c.GetSession(sessionID)
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			// Find associated tmux sessions.
			tmuxOut, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
			prefix := "belayer-" + sess.Name + "-"
			var agentSessions []string
			for _, line := range strings.Split(strings.TrimSpace(string(tmuxOut)), "\n") {
				if strings.HasPrefix(line, prefix) {
					agentSessions = append(agentSessions, line)
				}
			}

			// Check for uncommitted changes in agent working directories.
			if len(agentSessions) > 0 && !force {
				var dirty []string
				for _, tmuxSess := range agentSessions {
					agentName := strings.TrimPrefix(tmuxSess, prefix)
					// Capture the agent pane's cwd via tmux.
					cwdOut, err := exec.Command("tmux", "display-message", "-t", tmuxSess, "-p", "#{pane_current_path}").CombinedOutput()
					if err != nil {
						continue
					}
					paneDir := strings.TrimSpace(string(cwdOut))
					if paneDir == "" {
						continue
					}
					// Check git status in that directory.
					gitCmd := exec.Command("git", "-C", paneDir, "status", "--porcelain")
					gitOut, err := gitCmd.CombinedOutput()
					if err != nil {
						continue // not a git repo or git not available
					}
					if len(strings.TrimSpace(string(gitOut))) > 0 {
						dirty = append(dirty, agentName+" ("+paneDir+")")
					}
				}

				if len(dirty) > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: Uncommitted changes detected in agent working directories:\n")
					for _, d := range dirty {
						fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", d)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "\nUse --force to stop anyway (uncommitted changes will be lost).\n")
					return fmt.Errorf("aborting: uncommitted changes found (use --force to override)")
				}
			}

			// Stop session via daemon.
			sess2, err := c.UpdateSession(sessionID, "stopped")
			if err != nil {
				return fmt.Errorf("stop session: %w", err)
			}

			// Kill associated tmux sessions.
			var killed []string
			for _, tmuxSess := range agentSessions {
				exec.Command("tmux", "kill-session", "-t", tmuxSess).Run() //nolint:errcheck
				killed = append(killed, strings.TrimPrefix(tmuxSess, prefix))
			}

			// Log session_completed event for reflection trigger.
			_ = c.LogEvent(sessionID, "session_completed", mustJSON(map[string]string{
				"name":   sess.Name,
				"status": "stopped",
			}))

			fmt.Fprintf(cmd.OutOrStdout(), "Stopped session %s (%s)\n", sess2.ID, sess2.Name)
			if len(killed) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Terminated agents: %s\n", strings.Join(killed, ", "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Reflection will run on next daemon cycle (trigger: post-session).")

			// Clean up Docker sandbox if it exists.
			home, _ := os.UserHomeDir()
			sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sessionID)
			composePath := filepath.Join(sandboxDir, "docker-compose.yml")
			if _, err := os.Stat(composePath); err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Stopping Docker sandbox...")
				stopCmd := exec.Command("docker", "compose", "-f", composePath, "down")
				stopCmd.Stdout = cmd.OutOrStdout()
				stopCmd.Stderr = cmd.ErrOrStderr()
				if err := stopCmd.Run(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: docker compose down failed: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Docker sandbox stopped.")
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVar(&force, "force", false, "Force stop even with uncommitted changes (DANGEROUS: uncommitted code will be lost)")
	return cmd
}

func newSessionWakeCmd() *cobra.Command {
	var agentName, socket string

	cmd := &cobra.Command{
		Use:   "wake <session-id-or-name>",
		Short: "Restart a crashed agent with compiled context",
		Long: `Restart a stopped agent in a session. Compiles restart context
from the session event history and relaunches the agent with
that context prepended to its prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentName == "" {
				return fmt.Errorf("--agent is required (which agent to wake)")
			}

			c := NewClient(resolveSocket(socket))
			target := args[0]

			sessionID, err := lookupSessionID(c, target)
			if err != nil {
				return fmt.Errorf("session not found: %w", err)
			}

			// Get session info.
			sessions, err := c.ListSessions()
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}
			var sessionName, sessionTemplate string
			for _, s := range sessions {
				if s.ID == sessionID {
					sessionName = s.Name
					sessionTemplate = s.Template
					break
				}
			}

			// Compile restart context from events.
			events, err := c.GetEvents(sessionID)
			if err != nil {
				return fmt.Errorf("get events: %w", err)
			}

			var contextLines []string
			contextLines = append(contextLines, fmt.Sprintf("=== RESTART CONTEXT (session: %s, agent: %s) ===", sessionID[:8], agentName))
			contextLines = append(contextLines, fmt.Sprintf("You are being restarted. Here is what happened before you stopped:\n"))

			for _, e := range events {
				// Include relevant events for context.
				switch e.Type {
				case "session_started", "agent_launched", "sandbox_started",
					"session_status_changed", "agent_note":
					contextLines = append(contextLines,
						fmt.Sprintf("[%s] %s: %s", e.Timestamp.Format("15:04:05"), e.Type, e.Data))
				}
			}
			contextLines = append(contextLines, "\n=== END RESTART CONTEXT ===\n")
			contextLines = append(contextLines, "Continue from where you left off. Check git status and test results to understand current state.\n")

			restartContext := strings.Join(contextLines, "\n")

			// Log the wake event.
			_ = c.LogEvent(sessionID, "agent_wake", mustJSON(map[string]string{
				"agent":         agentName,
				"context_lines": fmt.Sprintf("%d", len(events)),
			}))

			fmt.Fprintf(cmd.OutOrStdout(), "Waking agent %q in session %s (template: %s)\n", agentName, sessionName, sessionTemplate)
			fmt.Fprintf(cmd.OutOrStdout(), "Restart context: %d events compiled\n", len(events))
			fmt.Fprintf(cmd.OutOrStdout(), "\nContext will be prepended to the agent's prompt on next launch.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Restart context saved. Relaunch the session to apply:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  belayer session start --template %s --input <spec> --name %s\n", sessionTemplate, sessionName)

			// Write restart context to a file for the next session start to pick up.
			home, _ := os.UserHomeDir()
			restartDir := filepath.Join(home, ".belayer", "restart")
			os.MkdirAll(restartDir, 0o700)
			restartFile := filepath.Join(restartDir, sessionID+"-"+agentName+".txt")
			if err := os.WriteFile(restartFile, []byte(restartContext), 0o600); err != nil {
				return fmt.Errorf("write restart context: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Restart context: %s\n", restartFile)

			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "Agent to wake (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

// lookupSessionID resolves a session name, ID prefix, or full ID to a full session ID.
// It lists sessions from the daemon and matches against ID prefix or exact name.
// If the daemon is unavailable, the original arg is returned as-is.
func lookupSessionID(c *Client, arg string) (string, error) {
	sessions, err := c.ListSessions()
	if err != nil {
		// Daemon may be offline; fall back to treating arg as a raw ID.
		return arg, nil
	}
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, arg) || s.Name == arg {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no session found matching %q", arg)
}

func newAttachCmd() *cobra.Command {
	var agentName, socket string

	cmd := &cobra.Command{
		Use:   "attach <session-id-or-name>",
		Short: "Attach to a session's agent tmux panes",
		Long: `Attach to agent tmux sessions for a belayer session.

Without --agent, lists all agent panes for the session.
With --agent, attaches directly to that agent's tmux pane.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			// Try to resolve session name via daemon.
			c := NewClient(resolveSocket(socket))
			sessionName := target
			if err := c.Health(); err == nil {
				if sessionID, err := lookupSessionID(c, target); err == nil {
					// Look up name from the resolved ID.
					if sessions, err := c.ListSessions(); err == nil {
						for _, s := range sessions {
							if s.ID == sessionID {
								sessionName = s.Name
								break
							}
						}
					}
				}
			}

			// If --agent specified, attach directly.
			if agentName != "" {
				tmuxTarget := "belayer-" + sessionName + "-" + agentName
				tmuxCmd := exec.Command("tmux", "attach-session", "-t", tmuxTarget)
				tmuxCmd.Stdin = os.Stdin
				tmuxCmd.Stdout = os.Stdout
				tmuxCmd.Stderr = os.Stderr
				err := tmuxCmd.Run()
				if err != nil {
					// Fallback: try docker exec into tmux inside container
					dockerCmd := exec.Command("docker", "exec", "-it",
						"belayer-"+sessionName+"-"+agentName+"-1",
						"tmux", "attach", "-t", "agent")
					dockerCmd.Stdin = os.Stdin
					dockerCmd.Stdout = os.Stdout
					dockerCmd.Stderr = os.Stderr
					return dockerCmd.Run()
				}
				return nil
			}

			// List all agent tmux sessions for this session.
			out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
			if err != nil {
				out = nil
			}

			prefix := "belayer-" + sessionName + "-"
			var agents []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.HasPrefix(line, prefix) {
					agents = append(agents, strings.TrimPrefix(line, prefix))
				}
			}

			// If no tmux sessions found, try Docker containers.
			if len(agents) == 0 {
				// Look for running containers with belayer session label
				dockerOut, err := exec.Command("docker", "ps", "--filter",
					"label=belayer.session="+sessionName, "--format", "{{.Names}}").CombinedOutput()
				if err == nil {
					for _, line := range strings.Split(strings.TrimSpace(string(dockerOut)), "\n") {
						if line != "" {
							// Extract agent name from container name (format: belayer-{session}-{agent}-1)
							parts := strings.Split(line, "-")
							if len(parts) >= 3 {
								agents = append(agents, parts[len(parts)-2])
							}
						}
					}
				}
			}

			if len(agents) == 0 {
				return fmt.Errorf("no agent tmux sessions found for %q (prefix: %s)", target, prefix)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n\n", sessionName)
			fmt.Fprintln(cmd.OutOrStdout(), "Agents:")
			for _, a := range agents {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-16s tmux attach -t %s%s\n", a, prefix, a)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nAttach directly:\n  belayer attach %s --agent <name>\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "Attach to a specific agent (e.g., pilot, implementer, reviewer)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
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

			// Try to resolve name to ID.
			if resolved, err := lookupSessionID(c, sessionID); err == nil {
				sessionID = resolved
			}

			events, err := c.GetEvents(sessionID)
			if err != nil {
				return fmt.Errorf("get events: %w", err)
			}

			// Filter by --since if set.
			cutoff := time.Time{}
			if since > 0 {
				cutoff = time.Now().Add(-time.Duration(since) * time.Minute)
			}

			// Print existing events.
			lastSeen := time.Time{}
			for _, e := range events {
				if !cutoff.IsZero() && e.Timestamp.Before(cutoff) {
					continue
				}
				printEvent(cmd, e)
				if e.Timestamp.After(lastSeen) {
					lastSeen = e.Timestamp
				}
			}

			if !follow {
				return nil
			}

			// Polling loop for --follow.
			fmt.Fprintln(cmd.OutOrStdout(), "--- following (Ctrl+C to stop) ---")
			for {
				time.Sleep(2 * time.Second)

				events, err := c.GetEvents(sessionID)
				if err != nil {
					continue // transient error, keep polling
				}

				for _, e := range events {
					if !lastSeen.IsZero() && !e.Timestamp.After(lastSeen) {
						continue
					}
					printEvent(cmd, e)
					if e.Timestamp.After(lastSeen) {
						lastSeen = e.Timestamp
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
			if err != nil || resp.StatusCode != http.StatusOK {
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
	// Inside Docker containers, BELAYER_SOCKET points to the mounted daemon socket.
	if envSocket := os.Getenv("BELAYER_SOCKET"); envSocket != "" {
		return envSocket
	}
	return DefaultSocketPath()
}
