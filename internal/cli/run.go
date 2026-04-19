package cli

import (
	"encoding/json"
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
	var socket, name, task, supervisorProfile, workdir, reposFlag string
	var exitConditions []string
	var logLevel string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a run, spawn supervisor, and deliver the initial task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(task) == "" {
				return fmt.Errorf("--task is required")
			}

			// If --exit-condition was passed, render an override block that
			// the supervisor (and later the PM) will see at the top of the
			// initial task. This replaces config.yaml's exit_conditions for
			// this run only; the file on disk is untouched. Normalize first so
			// blank values (e.g. `--exit-condition ""`) fall through to the
			// project config instead of injecting an authoritative empty list.
			normalizedExitConditions := make([]string, 0, len(exitConditions))
			for _, c := range exitConditions {
				if c = strings.TrimSpace(c); c != "" {
					normalizedExitConditions = append(normalizedExitConditions, c)
				}
			}
			if len(normalizedExitConditions) > 0 {
				var b strings.Builder
				b.WriteString("<exit_conditions_override>\n")
				b.WriteString("These exit conditions replace .belayer/config.yaml#exit_conditions for this run. The PM must validate each one before marking the run complete.\n")
				for _, c := range normalizedExitConditions {
					b.WriteString("- ")
					b.WriteString(c)
					b.WriteString("\n")
				}
				b.WriteString("</exit_conditions_override>\n\n")
				b.WriteString(task)
				task = b.String()
			}
			if strings.TrimSpace(supervisorProfile) == "" {
				supervisorProfile = "default"
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

			// Scaffold .belayer/ if the user has not run `belayer init`.
			// Done before session creation so the supervisor's first lookup
			// of agent identities finds the project-local copies.
			if err := autoInitIfMissing(baseDir, cmd.OutOrStdout()); err != nil {
				return fmt.Errorf("auto-init .belayer/: %w", err)
			}

			// Create session with baseDir as workspace so the daemon can resolve
			// sandbox settings (.belayer/config.yaml) even when no repos are given.
			sess, err := c.CreateSession(sessionName, "nightshift", repos, baseDir, resolveRunLogLevel(logLevel))
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			// Provision workspace if repos are specified, then update session with the path.
			supervisorWorkdir := workdir
			if len(repos) > 0 {
				wsDir, err := provisionWorkspace(baseDir, sess.ID, repos)
				if err != nil {
					return fmt.Errorf("provision workspace: %w", err)
				}
				supervisorWorkdir = wsDir

				if err := c.UpdateSessionWorkspaceDir(sess.ID, wsDir); err != nil {
					return fmt.Errorf("update session workspace dir: %w", err)
				}
			}

			if _, err := c.UpdateSession(sess.ID, "running"); err != nil {
				return fmt.Errorf("mark session running: %w", err)
			}
			if _, err := c.SpawnAgent(sess.ID, spawnAgentRequest{Name: "supervisor", Role: "supervisor", Profile: supervisorProfile, Workdir: supervisorWorkdir}); err != nil {
				return fmt.Errorf("spawn supervisor: %w", err)
			}
			if _, err := c.SendMessage(sess.ID, "supervisor", task, "instruction", true); err != nil {
				return fmt.Errorf("deliver initial task: %w", err)
			}
			// Log the initial prompt as telemetry so post-mortems can see what started the run.
			initData, _ := json.Marshal(map[string]any{
				"task":               task,
				"supervisor_profile": supervisorProfile,
				"exit_conditions":    normalizedExitConditions,
			})
			_ = c.LogEvent(sess.ID, "run_initiated", string(initData))

			fmt.Fprintf(cmd.OutOrStdout(), "Run started: %s (%s)\n", sess.ID, sess.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "Supervisor: supervisor")
			if len(repos) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Repos:")
				for repoName, repoPath := range repos {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", repoName, repoPath)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", supervisorWorkdir)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Monitor with:")
			sockPath := resolveSocket(socket)
			fmt.Fprintf(cmd.OutOrStdout(), "  BELAYER_SESSION_ID=%s BELAYER_SOCKET=%s belayer roster\n", sess.ID, sockPath)
			fmt.Fprintf(cmd.OutOrStdout(), "  belayer logs %s\n", sess.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&name, "name", "", "Run/session name")
	cmd.Flags().StringVar(&task, "task", "", "Initial task text for the supervisor")
	cmd.Flags().StringVar(&supervisorProfile, "supervisor-profile", "default", "Hermes profile for the supervisor")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory (defaults to cwd)")
	cmd.Flags().StringVar(&reposFlag, "repos", "", "Repos to include: name=path,name=path (e.g. frontend=../fe,backend=../be)")
	cmd.Flags().StringArrayVar(&exitConditions, "exit-condition", nil, "Override .belayer/config.yaml#exit_conditions for this run. Repeatable. When present, these replace the file's list entirely.")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Log tier for this run: standard|verbose|trace. Falls back to BELAYER_LOG_LEVEL, then daemon default.")
	return cmd
}

// resolveRunLogLevel returns the log level for a run: the --log-level flag
// value if non-empty, otherwise BELAYER_LOG_LEVEL, otherwise empty (daemon
// applies its own default). Validation of the final string happens server-side
// in ValidateLogLevel.
func resolveRunLogLevel(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv("BELAYER_LOG_LEVEL")
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
//
// TODO(multi-repo): revisit the --repos symlink-synthesis approach. The cleaner
// pattern in practice is a meta-directory with sibling repos and no --repos
// flag. See docs/design-docs/2026-04-16-sandbox-runtime-and-crag-proof.md
// "Open Questions #6" for the design conversation, including the related
// question of per-agent starting cwd in agent.yaml.
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
