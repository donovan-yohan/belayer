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
		Long:  "Launches a Claude Code session trained on belayer CLI usage. The agent can brainstorm tasks, fetch Jira tickets, generate spec.md and goals.json, and create tasks.",
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

			prompt, err := manage.BuildPrompt(manage.PromptData{
				InstanceName: name,
				RepoNames:    repoNames,
			})
			if err != nil {
				return fmt.Errorf("building manage prompt: %w", err)
			}

			return execClaude(prompt)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	return cmd
}

// execClaude replaces the current process with a claude session.
// Extracted for testability — tests can override this via the package-level var.
var execClaude = func(prompt string) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	return syscall.Exec(claudePath, []string{"claude", "-p", prompt, "--allowedTools", "*"}, os.Environ())
}
