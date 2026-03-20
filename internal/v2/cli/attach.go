package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach [role]",
		Short: "Attach to an active interactive session",
		Long: `Attach to a tmux window running an interactive pipeline session.

Without arguments, lists all active sessions. With a role name,
attaches to that role's window directly.

Examples:
  belayer attach          # List active sessions
  belayer attach setter   # Attach to the setter session`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listSessions()
			}
			return attachToRole(args[0])
		},
	}
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
	fmt.Println("\nAttach with: belayer attach <role>")
	fmt.Println("Or directly: tmux attach -t belayer")
	return nil
}

func attachToRole(roleName string) error {
	windows, err := getTmuxWindows("belayer")
	if err != nil {
		return fmt.Errorf("no belayer tmux session found. Is a pipeline running?")
	}

	// Find window matching the role name.
	var target string
	for _, w := range windows {
		if strings.HasPrefix(w, roleName+"-") || w == roleName {
			target = w
			break
		}
	}

	if target == "" {
		fmt.Printf("No active session for role %q.\n", roleName)
		if len(windows) > 0 {
			fmt.Println("Available sessions:")
			for _, w := range windows {
				fmt.Printf("  %s\n", w)
			}
		}
		return nil
	}

	// Attach to the tmux session, selecting the target window.
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
