package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newDebugCmd() *cobra.Command {
	var socket string
	var tail int

	cmd := &cobra.Command{
		Use:   "debug <session-id-or-name>",
		Short: "Show aggregated diagnostics for a session",
		Long: `Show session metadata, recent events, Docker container health,
and compose file location. Designed for programmatic debugging.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))
			target := args[0]

			// Resolve session.
			sessionID, err := lookupSessionID(c, target)
			if err != nil {
				return fmt.Errorf("session not found: %w", err)
			}

			out := cmd.OutOrStdout()

			// 1. Session metadata.
			sessions, err := c.ListSessions()
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}
			fmt.Fprintln(out, "=== Session ===")
			for _, s := range sessions {
				if s.ID == sessionID {
					fmt.Fprintf(out, "  ID:       %s\n", s.ID)
					fmt.Fprintf(out, "  Name:     %s\n", s.Name)
					fmt.Fprintf(out, "  Status:   %s\n", s.Status)
					fmt.Fprintf(out, "  Template: %s\n", s.Template)
					fmt.Fprintf(out, "  Created:  %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
					break
				}
			}

			// 2. Recent events.
			events, err := c.GetEvents(sessionID)
			if err != nil {
				fmt.Fprintf(out, "\n=== Events ===\n  Error: %v\n", err)
			} else {
				fmt.Fprintf(out, "\n=== Events (last %d of %d) ===\n", min(tail, len(events)), len(events))
				start := 0
				if len(events) > tail {
					start = len(events) - tail
				}
				for _, e := range events[start:] {
					fmt.Fprintf(out, "  %s  %-24s  %s\n",
						e.Timestamp.Format("15:04:05.000"), e.Type, truncate(e.Data, 120))
				}
			}

			// 3. Docker container health.
			home, _ := os.UserHomeDir()
			composeDir := filepath.Join(home, ".belayer", "sandboxes", sessionID)
			composePath := filepath.Join(composeDir, "docker-compose.yml")

			if _, err := os.Stat(composePath); err == nil {
				fmt.Fprintf(out, "\n=== Docker Sandbox ===\n")
				fmt.Fprintf(out, "  Compose: %s\n", composePath)

				// Run docker compose ps.
				psCmd := exec.Command("docker", "compose", "-f", composePath, "ps", "--format", "table {{.Name}}\t{{.Status}}\t{{.State}}")
				psOut, err := psCmd.CombinedOutput()
				if err != nil {
					fmt.Fprintf(out, "  Containers: error (%v)\n", err)
				} else {
					lines := strings.Split(strings.TrimSpace(string(psOut)), "\n")
					for _, line := range lines {
						fmt.Fprintf(out, "  %s\n", line)
					}
				}

				// Show recent docker logs for any exited containers.
				stateCmd := exec.Command("docker", "compose", "-f", composePath, "ps", "--format", "{{.Name}} {{.State}}")
				stateOut, _ := stateCmd.CombinedOutput()
				for _, line := range strings.Split(strings.TrimSpace(string(stateOut)), "\n") {
					parts := strings.Fields(line)
					if len(parts) >= 2 && parts[1] == "exited" {
						fmt.Fprintf(out, "\n  --- Last 10 lines from %s ---\n", parts[0])
						logCmd := exec.Command("docker", "logs", "--tail", "10", parts[0])
						logOut, _ := logCmd.CombinedOutput()
						for _, logLine := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
							fmt.Fprintf(out, "    %s\n", logLine)
						}
					}
				}
			} else {
				fmt.Fprintf(out, "\n=== Docker Sandbox ===\n  Not found (local tmux mode or sandbox cleaned up)\n")
			}

			// 4. Sandbox files.
			if entries, err := os.ReadDir(composeDir); err == nil {
				fmt.Fprintf(out, "\n=== Sandbox Files ===\n")
				for _, entry := range entries {
					info, _ := entry.Info()
					if info != nil {
						fmt.Fprintf(out, "  %-30s  %d bytes\n", entry.Name(), info.Size())
					}
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().IntVar(&tail, "tail", 20, "Number of recent events to show")
	return cmd
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
