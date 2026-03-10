package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/manage"
	"github.com/spf13/cobra"
)

func newManageCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Start an interactive agent session for task creation",
		Long:  "Launches a Claude Code session with belayer context. The session has slash commands for task creation, status, messaging, and more.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			instConfig, _, err := instance.Load(name)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", name, err)
			}

			var repoNames []string
			for _, r := range instConfig.Repos {
				repoNames = append(repoNames, r.Name)
			}

			// Create temp workspace with .claude/ context
			tmpDir, err := os.MkdirTemp("", "belayer-manage-*")
			if err != nil {
				return fmt.Errorf("creating temp dir: %w", err)
			}
			// Note: no defer cleanup — process is replaced by exec

			if err := manage.PrepareManageDir(tmpDir, manage.PromptData{
				InstanceName: name,
				RepoNames:    repoNames,
			}); err != nil {
				return fmt.Errorf("preparing manage workspace: %w", err)
			}

			return execClaudeInDir(tmpDir, name)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	return cmd
}

// execClaudeInDir replaces the current process with a claude session in the given directory.
// Sets BELAYER_INSTANCE env var so all belayer commands auto-resolve the instance.
var execClaudeInDir = func(dir string, instanceName string) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	env := append(os.Environ(), "BELAYER_INSTANCE="+instanceName)

	// Change to the temp dir so claude picks up .claude/ files
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("changing to manage dir: %w", err)
	}

	return syscall.Exec(claudePath, []string{"claude"}, env)
}
