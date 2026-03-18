package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/crag"
	"github.com/donovan-yohan/belayer/internal/manage"
	"github.com/spf13/cobra"
)

func newSetterSessionCmd() *cobra.Command {
	var cragName string
	var yolo bool

	cmd := &cobra.Command{
		Use:   "setter",
		Short: "Start an interactive setter session for research and problem creation",
		Long:  "Launches a Claude Code session with belayer context. The session has slash commands for discovery research, problem planning, status, messaging, and more.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveCragName(cragName)
			if err != nil {
				return err
			}

			cragCfg, cragDir, err := crag.Load(name)
			if err != nil {
				return fmt.Errorf("loading crag %q: %w", name, err)
			}

			var repoNames []string
			for _, r := range cragCfg.Repos {
				repoNames = append(repoNames, r.Name)
			}

			// Write .claude/ context into the crag directory (stable path avoids trust popup)
			if err := manage.PrepareManageDir(cragDir, manage.PromptData{
				CragName:  name,
				RepoNames: repoNames,
			}); err != nil {
				return fmt.Errorf("preparing setter workspace: %w", err)
			}

			return execClaudeInDir(cragDir, name, yolo)
		},
	}

	cmd.Flags().StringVarP(&cragName, "crag", "c", "", "Crag name (defaults to default crag)")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Skip permission prompts (passes --dangerously-skip-permissions to claude)")
	return cmd
}

// execClaudeInDir replaces the current process with a claude session in the given directory.
// Sets BELAYER_CRAG env var so all belayer commands auto-resolve the crag.
var execClaudeInDir = func(dir string, cragName string, skipPermissions bool) error {
	return execClaudeSession(dir, map[string]string{"BELAYER_CRAG": cragName}, skipPermissions)
}

func buildClaudeEnv(baseEnv []string, envOverrides map[string]string) []string {
	cleaned := make([]string, 0, len(baseEnv)+len(envOverrides))
	for _, e := range baseEnv {
		if !strings.HasPrefix(e, "BELAYER_CRAG=") && !strings.HasPrefix(e, "BELAYER_INSTANCE=") {
			cleaned = append(cleaned, e)
		}
	}
	for key, value := range envOverrides {
		cleaned = append(cleaned, key+"="+value)
	}
	return cleaned
}

// execClaudeSession replaces the current process with a claude session in the given directory.
// It always clears stale BELAYER_CRAG/BELAYER_INSTANCE values before applying any overrides.
var execClaudeSession = func(dir string, envOverrides map[string]string, skipPermissions bool) error {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	env := buildClaudeEnv(os.Environ(), envOverrides)

	// Change to the workspace dir so claude picks up .claude/ files.
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("changing to workspace dir: %w", err)
	}

	args := []string{"claude"}
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	return syscall.Exec(claudePath, args, env)
}
