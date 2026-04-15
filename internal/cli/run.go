package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Nightshift run lifecycle commands",
	}
	cmd.AddCommand(newRunStartCmd())
	return cmd
}

func newRunStartCmd() *cobra.Command {
	var socket, name, task, plannerProfile, workdir, reposFlag string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a run, spawn planner, and deliver the initial task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(task) == "" {
				return fmt.Errorf("--task is required")
			}
			if strings.TrimSpace(plannerProfile) == "" {
				return fmt.Errorf("--planner-profile is required")
			}

			// Parse repos.
			repos, err := parseRepos(reposFlag)
			if err != nil {
				return fmt.Errorf("parse --repos: %w", err)
			}

			c := NewClient(resolveSocket(socket))
			sessionName := name
			if sessionName == "" {
				sessionName = "nightshift-run"
			}

			// Determine base directory for workspace.
			baseDir := workdir
			if baseDir == "" {
				baseDir, _ = os.Getwd()
			}

			// Create session — we need its ID first to provision the workspace.
			sess, err := c.CreateSession(sessionName, "nightshift", repos, "")
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			// Provision workspace if repos are specified, then update session with the path.
			plannerWorkdir := workdir
			if len(repos) > 0 {
				wsDir, err := provisionWorkspace(baseDir, sess.ID, repos)
				if err != nil {
					return fmt.Errorf("provision workspace: %w", err)
				}
				plannerWorkdir = wsDir

				if err := c.UpdateSessionWorkspaceDir(sess.ID, wsDir); err != nil {
					return fmt.Errorf("update session workspace dir: %w", err)
				}
			}

			if _, err := c.UpdateSession(sess.ID, "running"); err != nil {
				return fmt.Errorf("mark session running: %w", err)
			}
			if _, err := c.SpawnAgent(sess.ID, spawnAgentRequest{Name: "planner", Role: "planner", Profile: plannerProfile, Workdir: plannerWorkdir}); err != nil {
				return fmt.Errorf("spawn planner: %w", err)
			}
			if _, err := c.SendMessage(sess.ID, "planner", task, "instruction", true); err != nil {
				return fmt.Errorf("deliver initial task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Run started: %s (%s)\n", sess.ID, sess.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "Planner: planner")
			if len(repos) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Repos:")
				for repoName, repoPath := range repos {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", repoName, repoPath)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", plannerWorkdir)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Monitor with:")
			fmt.Fprintf(cmd.OutOrStdout(), "  BELAYER_SESSION_ID=%s belayer roster\n", sess.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "  belayer logs %s\n", sess.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&name, "name", "", "Run/session name")
	cmd.Flags().StringVar(&task, "task", "", "Initial task text for the planner")
	cmd.Flags().StringVar(&plannerProfile, "planner-profile", "", "Hermes profile for the planner")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory (defaults to cwd)")
	cmd.Flags().StringVar(&reposFlag, "repos", "", "Repos to include: name=path,name=path (e.g. frontend=../fe,backend=../be)")
	return cmd
}

// parseRepos parses a --repos flag value like "frontend=/path/to/fe,backend=/path/to/be"
// into a map. Paths are resolved to absolute paths.
func parseRepos(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	repos := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid repo spec %q (expected name=/path)", pair)
		}
		repoName := strings.TrimSpace(parts[0])
		repoPath := strings.TrimSpace(parts[1])
		absPath, err := filepath.Abs(repoPath)
		if err != nil {
			return nil, fmt.Errorf("resolve path for repo %q: %w", repoName, err)
		}
		if _, err := os.Stat(absPath); err != nil {
			return nil, fmt.Errorf("repo %q path %q does not exist: %w", repoName, absPath, err)
		}
		repos[repoName] = absPath
	}
	if len(repos) == 0 {
		return nil, nil
	}
	return repos, nil
}

// provisionWorkspace creates a workspace directory with symlinks to each repo
// and a shared artifacts directory. Returns the workspace root path.
func provisionWorkspace(baseDir, sessionID string, repos map[string]string) (string, error) {
	workspaceDir := filepath.Join(baseDir, ".belayer", "runs", sessionID, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}

	// Symlink each repo into the workspace.
	for repoName, repoPath := range repos {
		link := filepath.Join(workspaceDir, repoName)
		if err := os.Symlink(repoPath, link); err != nil {
			return "", fmt.Errorf("symlink repo %q: %w", repoName, err)
		}
	}

	// Create shared artifacts directory.
	artifactsDir := filepath.Join(workspaceDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		return "", fmt.Errorf("create artifacts dir: %w", err)
	}

	return workspaceDir, nil
}
