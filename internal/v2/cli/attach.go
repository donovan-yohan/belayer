package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newAttachCmd() *cobra.Command {
	var taskID string

	cmd := &cobra.Command{
		Use:   "attach [role]",
		Short: "Attach to an active interactive session",
		Long: `Attach to a tmux window running an interactive pipeline session.

Without arguments, lists all active sessions. With a role name,
attaches to that role's window. If multiple runs have the same role
active, use --task-id to pick one, or omit it to see a list.

Examples:
  belayer attach                          # List all sessions
  belayer attach setter                   # Attach (or list if ambiguous)
  belayer attach setter --task-id abc123  # Attach to a specific run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listSessions()
			}
			return attachToRole(args[0], taskID)
		},
	}

	cmd.Flags().StringVar(&taskID, "task-id", "", "Filter by task/workflow ID (prefix match)")

	return cmd
}

func listSessions() error {
	windows, err := getTmuxWindows("belayer")
	if err != nil {
		fmt.Println("No active belayer sessions.")
		fmt.Println("Start a pipeline with: belayer run \"your description\"")
		return nil
	}

	if len(windows) == 0 {
		fmt.Println("No active belayer sessions.")
		return nil
	}

	fmt.Println("Active sessions:")
	for _, w := range windows {
		fmt.Printf("  %s\n", w)
	}
	fmt.Println("\nAttach with: belayer attach <name>")
	fmt.Println("Or directly: tmux attach -t belayer")
	return nil
}

func attachToRole(roleName, taskID string) error {
	windows, err := getTmuxWindows("belayer")
	if err != nil {
		return fmt.Errorf("no belayer tmux session found. Is a pipeline running?")
	}

	// Collect all matching windows.
	var matches []string
	for _, w := range windows {
		if !strings.HasPrefix(w, roleName+"-") && w != roleName {
			continue
		}
		// If task-id filter is set, check suffix.
		if taskID != "" && !strings.Contains(w, taskID) {
			continue
		}
		matches = append(matches, w)
	}

	if len(matches) == 0 {
		fmt.Printf("No active session for role %q", roleName)
		if taskID != "" {
			fmt.Printf(" with task-id %q", taskID)
		}
		fmt.Println(".")
		if len(windows) > 0 {
			fmt.Println("\nAvailable sessions:")
			for _, w := range windows {
				fmt.Printf("  %s\n", w)
			}
		}
		return nil
	}

	if len(matches) > 1 {
		fmt.Printf("Multiple %s sessions active:\n", roleName)
		for _, w := range matches {
			fmt.Printf("  %s\n", w)
		}
		fmt.Println("\nSpecify which one:")
		fmt.Printf("  belayer attach %s --task-id <id-prefix>\n", roleName)
		fmt.Printf("  belayer attach %s   (exact window name)\n", matches[0])
		return nil
	}

	// Exactly one match — attach.
	target := matches[0]
	tmuxCmd := exec.Command("tmux", "attach", "-t", "belayer:"+target)
	tmuxCmd.Stdin = os.Stdin
	tmuxCmd.Stdout = os.Stdout
	tmuxCmd.Stderr = os.Stderr
	return tmuxCmd.Run()
}

func getTmuxWindows(session string) ([]string, error) {
	cmd := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
