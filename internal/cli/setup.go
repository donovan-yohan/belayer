package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/session"
	"github.com/donovan-yohan/belayer/internal/workspace"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var workspaceDir string
	var reposFile string
	var global bool

	cmd := &cobra.Command{
		Use:          "setup",
		Short:        "Bootstrap a .belayer/ workspace",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var belayerDir string
			switch {
			case workspaceDir != "":
				belayerDir = workspaceDir
			case global:
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("setup: resolve home directory: %w", err)
				}
				belayerDir = filepath.Join(home, ".belayer")
			default:
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("setup: resolve working directory: %w", err)
				}
				belayerDir = filepath.Join(cwd, ".belayer")
			}

			// 1. Create ~/.belayer/ if it doesn't exist.
			if err := os.MkdirAll(belayerDir, 0755); err != nil {
				return fmt.Errorf("setup: create workspace dir %q: %w", belayerDir, err)
			}

			// 2. Resolve and validate repos.json.
			defaultReposFile := filepath.Join(belayerDir, "repos.json")
			reposPath := ""
			var ws *workspace.Workspace

			if reposFile != "" {
				reposPath = reposFile
			} else if _, err := os.Stat(defaultReposFile); err == nil {
				reposPath = defaultReposFile
			}

			repoNames := []string{}
			if reposPath != "" {
				var err error
				ws, err = workspace.Load(reposPath)
				if err != nil {
					return fmt.Errorf("setup: load repos file %q: %w", reposPath, err)
				}
				if err := ws.EnsureReady(); err != nil {
					return fmt.Errorf("setup: %w", err)
				}
				for _, r := range ws.Repos() {
					repoNames = append(repoNames, r.Name)
				}
			}

			// 3. Write default session templates.
			templatesDir := filepath.Join(belayerDir, "templates")
			if err := session.WriteDefaultTemplates(templatesDir); err != nil {
				return fmt.Errorf("setup: write templates: %w", err)
			}

			// 4. Create agent memory directories.
			agentsBaseDir := filepath.Join(belayerDir, "agents")
			mem := agent.NewAgentMemory(agentsBaseDir)
			defaultAgents := []string{"pilot", "implementer", "reviewer"}
			for _, name := range defaultAgents {
				if err := mem.EnsureDir(name); err != nil {
					return fmt.Errorf("setup: create agent memory for %q: %w", name, err)
				}
			}

			// 4. Ensure socket directory exists (daemon.sock lives in belayerDir).
			socketDir := belayerDir
			if err := os.MkdirAll(socketDir, 0755); err != nil {
				return fmt.Errorf("setup: ensure socket directory: %w", err)
			}

			// 5. Print summary.
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Belayer workspace initialized:\n")
			fmt.Fprintf(out, "  Config:    %s/\n", belayerDir)
			fmt.Fprintf(out, "  Database:  %s/belayer.db (created on first daemon start)\n", belayerDir)
			fmt.Fprintf(out, "  Socket:    %s/daemon.sock\n", belayerDir)

			fmt.Fprintf(out, "  Templates: %s/\n", templatesDir)

			if len(repoNames) > 0 {
				fmt.Fprintf(out, "  Repos:     %d configured (%s)\n", len(repoNames), joinNames(repoNames))
			} else {
				fmt.Fprintf(out, "  Repos:     none configured (add %s/repos.json to configure)\n", belayerDir)
			}

			fmt.Fprintf(out, "  Agents:    %s/{pilot,implementer,reviewer}/memory/system/\n", agentsBaseDir)
			fmt.Fprintf(out, "\nNext steps:\n")
			fmt.Fprintf(out, "  belayer daemon          # Start the daemon\n")
			fmt.Fprintf(out, "  belayer session create  # Create a session\n")

			return nil
		},
	}

	cmd.Flags().StringVar(&workspaceDir, "workspace", "", "Explicit workspace directory path")
	cmd.Flags().BoolVar(&global, "global", false, "Initialize workspace at ~/.belayer/ instead of cwd")
	cmd.Flags().StringVar(&reposFile, "repos", "", "Path to repos.json workspace config")
	return cmd
}

// joinNames joins a slice of names with ", ".
func joinNames(names []string) string {
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}
