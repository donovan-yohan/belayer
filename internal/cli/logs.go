package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/session"
)

func newLogsCmd() *cobra.Command {
	var workDir string
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs [node-name]",
		Short: "Tail node logs from a running or completed pipeline",
		Long: `Tail the stdout/stderr log of a pipeline node.

Without arguments, lists available log files. With a node name,
tails that node's latest log. Use --follow (-f) to stream live.

Examples:
  belayer logs                   List available logs
  belayer logs implement         Show the implement node log
  belayer logs implement -f      Follow the implement node log live`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workDir == "" {
				var err error
				workDir, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			// Find the latest worktree (highest timestamp).
			worktreeParent := filepath.Join(workDir, ".belayer", "worktrees")
			entries, err := os.ReadDir(worktreeParent)
			if err != nil {
				return fmt.Errorf("no worktrees found (is a pipeline running?): %w", err)
			}
			if len(entries) == 0 {
				return fmt.Errorf("no worktrees found in %s", worktreeParent)
			}

			// Sort by name descending (timestamps) to find latest.
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() > entries[j].Name()
			})
			latestWorktree := filepath.Join(worktreeParent, entries[0].Name())
			logDir := session.LogDir(latestWorktree)

			if len(args) == 0 {
				return listLogs(logDir)
			}

			return tailLog(logDir, args[0], follow)
		},
	}

	cmd.Flags().StringVar(&workDir, "work-dir", "", "Working directory (default: current directory)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (like tail -f)")

	return cmd
}

// listLogs prints available log files in the log directory.
func listLogs(logDir string) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No logs yet (node hasn't started).")
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No logs yet (node hasn't started).")
		return nil
	}

	fmt.Println("Available logs:")
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			info, _ := e.Info()
			size := ""
			if info != nil {
				size = fmt.Sprintf("  (%s)", humanSize(info.Size()))
			}
			// Strip .log suffix for display.
			name := strings.TrimSuffix(e.Name(), ".log")
			fmt.Printf("  %s%s\n", name, size)
		}
	}
	return nil
}

// tailLog tails a specific node's log file.
func tailLog(logDir, nodeName string, follow bool) error {
	// Find matching log file — try exact match first, then prefix match.
	pattern := filepath.Join(logDir, nodeName+"*.log")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return fmt.Errorf("no log found for node %q in %s", nodeName, logDir)
	}
	// Use the latest (highest attempt number).
	sort.Strings(matches)
	logPath := matches[len(matches)-1]

	args := []string{"-n", "+1"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, logPath)

	tailCmd := exec.Command("tail", args...)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr
	return tailCmd.Run()
}

func humanSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
