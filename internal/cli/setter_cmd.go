package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/manage"
	"github.com/spf13/cobra"
)

func newSetterSessionCmd() *cobra.Command {
	var instanceName string
	var yolo bool

	cmd := &cobra.Command{
		Use:   "setter",
		Short: "Start an interactive setter session for problem creation",
		Long:  "Launches a Claude Code session with belayer context. The session has slash commands for problem creation, status, messaging, and more.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			instConfig, instanceDir, err := instance.Load(name)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", name, err)
			}

			var repoNames []string
			for _, r := range instConfig.Repos {
				repoNames = append(repoNames, r.Name)
			}

			// Write .claude/ context into the instance directory (stable path avoids trust popup)
			if err := manage.PrepareManageDir(instanceDir, manage.PromptData{
				InstanceName: name,
				RepoNames:    repoNames,
			}); err != nil {
				return fmt.Errorf("preparing setter workspace: %w", err)
			}

			return execClaudeInDir(instanceDir, name, yolo)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Skip permission prompts (passes --dangerously-skip-permissions to claude)")
	return cmd
}

// execClaudeInDir replaces the current process with a claude session in the given directory.
// Sets BELAYER_INSTANCE env var so all belayer commands auto-resolve the instance.
var execClaudeInDir = func(dir string, instanceName string, skipPermissions bool) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// Deduplicate BELAYER_INSTANCE to avoid POSIX ambiguity with duplicate keys
	base := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "BELAYER_INSTANCE=") {
			base = append(base, e)
		}
	}
	env := append(base, "BELAYER_INSTANCE="+instanceName)

	// Change to the temp dir so claude picks up .claude/ files
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("changing to setter dir: %w", err)
	}

	args := []string{"claude"}
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	return syscall.Exec(claudePath, args, env)
}
