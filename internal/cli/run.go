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
	var socket, name, spec, supervisorProfile, workdir, reposFlag string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a run, spawn supervisor, and deliver the initial spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(spec) == "" {
				return fmt.Errorf("--spec is required")
			}
			if strings.TrimSpace(supervisorProfile) == "" {
				return fmt.Errorf("--supervisor-profile is required")
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

			// Create session — we need its ID first to provision the workspace.
			sess, err := c.CreateSession(sessionName, "nightshift", repos, "")
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

			// Persist the operator's spec to disk + register it as an artifact so
			// every agent (and the PM gate) reads the same source of truth instead
			// of mining `belayer message list` for the supervisor's instruction.
			specRel, err := writeSpecArtifact(c, baseDir, sess.ID, spec)
			if err != nil {
				return fmt.Errorf("persist spec: %w", err)
			}

			if _, err := c.SpawnAgent(sess.ID, spawnAgentRequest{Name: "supervisor", Role: "supervisor", Profile: supervisorProfile, Workdir: supervisorWorkdir}); err != nil {
				return fmt.Errorf("spawn supervisor: %w", err)
			}
			if _, err := c.SendMessage(sess.ID, "supervisor", spec, "instruction", true); err != nil {
				return fmt.Errorf("deliver initial spec: %w", err)
			}
			// Log the initial prompt as telemetry so post-mortems can see what started the run.
			initData, _ := json.Marshal(map[string]string{
				"spec":               spec,
				"spec_path":          specRel,
				"supervisor_profile": supervisorProfile,
			})
			_ = c.LogEvent(sess.ID, "run_initiated", string(initData))

			fmt.Fprintf(cmd.OutOrStdout(), "Run started: %s (%s)\n", sess.ID, sess.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "Supervisor: supervisor")
			fmt.Fprintf(cmd.OutOrStdout(), "Spec: %s\n", specRel)
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
	cmd.Flags().StringVar(&spec, "spec", "", "Initial spec text — written to .belayer/runs/<id>/SPEC.md and registered as the operator artifact")
	cmd.Flags().StringVar(&supervisorProfile, "supervisor-profile", "", "Hermes profile for the supervisor")
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

// writeSpecArtifact persists the operator's --spec text to
// .belayer/runs/<sessionID>/SPEC.md and registers it as an artifact with
// kind=spec, producer=operator. This is the canonical operator-input artifact:
// every agent (and the PM gate) reads SPEC.md instead of mining the message log
// for the supervisor's first instruction. Returns the absolute path written.
func writeSpecArtifact(c *Client, baseDir, sessionID, spec string) (string, error) {
	runDir := filepath.Join(baseDir, ".belayer", "runs", sessionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", fmt.Errorf("create run dir: %w", err)
	}
	specPath := filepath.Join(runDir, "SPEC.md")
	if err := os.WriteFile(specPath, []byte(spec), 0o600); err != nil {
		return "", fmt.Errorf("write SPEC.md: %w", err)
	}
	if _, err := c.CreateArtifact(sessionID, artifactCreateCLIRequest{
		Kind:     "spec",
		Path:     specPath,
		Producer: "operator",
		Summary:  "Initial run spec from operator (--spec text passed to belayer run start).",
	}); err != nil {
		return "", fmt.Errorf("register spec artifact: %w", err)
	}
	return specPath, nil
}
