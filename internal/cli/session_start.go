package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/docker"
	"github.com/donovan-yohan/belayer/internal/session"
	"github.com/donovan-yohan/belayer/internal/shell"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newSessionStartCmd() *cobra.Command {
	var template, input, name, repo, socket, attachAgent, environment string
	var attach, dockerMode bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a session from a template",
		Long: `Load a session template, create the session via the daemon, compile
agent prompts, and launch each agent in a tmux pane (or Docker container
with --docker).

Templates are loaded from .belayer/templates/<name>.yaml in the workspace,
falling back to built-in defaults.

In Docker mode, agents run inside isolated containers with configurable
network access. Use --environment to specify a .belayer/environments/<name>.yaml
config that controls network isolation, compose extensions, and repos.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if template == "" {
				return fmt.Errorf("--template is required")
			}
			if input == "" {
				return fmt.Errorf("--input is required (task description or path to spec file)")
			}

			// 1. Resolve workspace directory and templates.
			wsDir := resolveWorkspaceDir()
			templatesDir := filepath.Join(wsDir, "templates")

			// 2. Require daemon is running.
			c := NewClient(resolveSocket(socket))
			if err := c.Health(); err != nil {
				return fmt.Errorf("daemon is not running — start it with: belayer daemon\n  %w", err)
			}

			// 3. Read input: file path or literal text.
			taskInput := input
			if info, err := os.Stat(input); err == nil && !info.IsDir() {
				data, err := os.ReadFile(input)
				if err != nil {
					return fmt.Errorf("read input file: %w", err)
				}
				taskInput = string(data)
			}

			// 4. Generate session name if not provided.
			if name == "" {
				name = fmt.Sprintf("%s-%d", template, time.Now().Unix())
			}

			// 5. Load template from workspace, falling back to built-in.
			tmpl, err := session.LoadTemplateFromDir(templatesDir, template)
			if err != nil {
				return fmt.Errorf("load template: %w", err)
			}
			if err := session.ValidateTemplate(tmpl); err != nil {
				return fmt.Errorf("validate template: %w", err)
			}

			// 6. Create session via daemon.
			sess, err := c.CreateSession(name, template)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			// 7. Build list of agent names.
			agentNames := make([]string, len(tmpl.Agents))
			for i, a := range tmpl.Agents {
				agentNames[i] = a.Name
			}

			// 8. Resolve working directory.
			workDir := repo
			if workDir == "" {
				workDir, _ = os.Getwd()
			}

			// 9. Launch agents.
			if dockerMode {
				if err := launchDocker(cmd, c, tmpl, sess, agentNames, taskInput, workDir, wsDir, environment, name); err != nil {
					return err
				}
			} else {
				if err := launchTmux(cmd, c, tmpl, sess, agentNames, taskInput, workDir, name); err != nil {
					return err
				}
			}

			// 10. Log session_started event and update status.
			_ = c.LogEvent(sess.ID, "session_started", mustJSON(map[string]string{
				"template": template,
				"name":     name,
			}))
			_, _ = c.UpdateSession(sess.ID, "running")

			// 11. Print summary.
			fmt.Fprintf(cmd.OutOrStdout(), "Session started: %s (template: %s)\n", sess.ID, template)
			if !dockerMode {
				for _, spec := range tmpl.Agents {
					label := fmt.Sprintf("%-14s", spec.Name+":")
					fmt.Fprintf(cmd.OutOrStdout(), "  %s belayer-%s-%s (%s/%s)\n",
						label, name, spec.Name, spec.Vendor, spec.Model)
				}
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), "Attach to agents:")
				for _, spec := range tmpl.Agents {
					fmt.Fprintf(cmd.OutOrStdout(), "  belayer attach %s --agent %s\n", name, spec.Name)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Monitor:")
			fmt.Fprintln(cmd.OutOrStdout(), "  belayer status")
			fmt.Fprintf(cmd.OutOrStdout(), "  belayer logs %s\n", sess.ID)

			// 12. Auto-attach if requested (local tmux only).
			if attach && !dockerMode {
				target := attachAgent
				if target == "" && len(tmpl.Agents) > 0 {
					target = tmpl.Agents[0].Name
				}
				tmuxTarget := "belayer-" + name + "-" + target
				fmt.Fprintf(cmd.OutOrStdout(), "\nAttaching to %s...\n", target)
				tmuxCmd := exec.Command("tmux", "attach-session", "-t", tmuxTarget)
				tmuxCmd.Stdin = os.Stdin
				tmuxCmd.Stdout = os.Stdout
				tmuxCmd.Stderr = os.Stderr
				return tmuxCmd.Run()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&template, "template", "", "Session template name (required)")
	cmd.Flags().StringVar(&input, "input", "", "Task description or path to spec file (required)")
	cmd.Flags().StringVar(&name, "name", "", "Session name (auto-generated if omitted)")
	cmd.Flags().StringVar(&repo, "repo", "", "Repository working directory (defaults to cwd)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVar(&attach, "attach", false, "Attach to the first agent's tmux pane after launch")
	cmd.Flags().StringVar(&attachAgent, "attach-agent", "", "Agent to attach to (requires --attach)")
	cmd.Flags().BoolVar(&dockerMode, "docker", false, "Run agents in Docker containers instead of local tmux")
	cmd.Flags().StringVar(&environment, "environment", "", "Environment config name (from .belayer/environments/<name>.yaml)")
	return cmd
}

// launchDocker starts agents in Docker containers with network isolation.
func launchDocker(cmd *cobra.Command, c *Client, tmpl session.SessionTemplate, sess sessionResponse, agentNames []string, taskInput, workDir, wsDir, environment, sessionName string) error {
	// 1. Load and validate environment config.
	var envCfg *docker.EnvironmentConfig
	if environment != "" {
		var err error
		envCfg, err = docker.LoadEnvironmentByName(wsDir, environment)
		if err != nil {
			return fmt.Errorf("load environment %q: %w", environment, err)
		}
		if err := docker.ValidateEnvironment(envCfg); err != nil {
			return fmt.Errorf("validate environment: %w", err)
		}
	} else {
		envCfg = docker.DefaultEnvironment()
	}

	// 2. Compute sandbox directory and create it.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sess.ID)
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return fmt.Errorf("create sandbox dir: %w", err)
	}

	// 3. Write task file to sandbox dir (shared across agents).
	taskFile := filepath.Join(sandboxDir, "task.txt")
	if err := os.WriteFile(taskFile, []byte(taskInput), 0o600); err != nil {
		return fmt.Errorf("write task file: %w", err)
	}

	// 4. Generate .env file for vendor auth.
	envFilePath := filepath.Join(sandboxDir, ".env")
	if err := os.WriteFile(envFilePath, docker.GenerateEnvFile(nil), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	// 5. Create per-agent worktrees for isolation.
	agentWorkDirs := make(map[string]string, len(tmpl.Agents))
	for _, spec := range tmpl.Agents {
		wt, err := createWorktree(workDir, sandboxDir, spec.Name, "")
		if err != nil {
			// Log warning but continue — fallback to shared workDir.
			fmt.Fprintf(cmd.OutOrStdout(), "  warning: worktree for %s: %v (using shared workdir)\n", spec.Name, err)
			agentWorkDirs[spec.Name] = workDir
		} else {
			agentWorkDirs[spec.Name] = wt
		}
	}

	// 6. Build agent configs.
	hostUID := fmt.Sprintf("%d", os.Getuid())
	hostGID := fmt.Sprintf("%d", os.Getgid())
	sessionVolume := sandboxDir + ":/belayer/session:ro"
	socketPath := filepath.Join(home, ".belayer", "daemon.sock")
	socketVolume := socketPath + ":/belayer/daemon.sock"

	agentCfgs := make([]docker.AgentComposeConfig, 0, len(tmpl.Agents))
	for _, spec := range tmpl.Agents {
		team := make([]agent.TeamMember, 0, len(tmpl.Agents)-1)
		for _, s := range tmpl.Agents {
			if s.Name != spec.Name {
				team = append(team, agent.TeamMember{
					Name:   s.Name,
					Vendor: s.Vendor,
					Model:  s.Model,
					Role:   s.Role,
				})
			}
		}

		cfg := agent.AgentConfig{
			Name:         spec.Name,
			Vendor:       spec.Vendor,
			Model:        spec.Model,
			SystemPrompt: spec.SystemPrompt,
		}

		compiled := agent.CompilePrompt(agent.PromptContext{
			Config:    cfg,
			TaskInput: taskInput,
			SessionID: sess.ID,
			Team:      team,
		})

		// Write agent-specific system prompt to sandbox dir.
		sysPromptFile := filepath.Join(sandboxDir, fmt.Sprintf("sysprompt-%s.txt", spec.Name))
		if err := os.WriteFile(sysPromptFile, []byte(compiled), 0o600); err != nil {
			return fmt.Errorf("write system prompt for %s: %w", spec.Name, err)
		}

		// Build the vendor CLI command using container paths.
		containerPrompt := fmt.Sprintf("/belayer/session/sysprompt-%s.txt", spec.Name)
		containerTask := "/belayer/session/task.txt"
		agentCmd := buildVendorCmd(spec, containerPrompt, containerTask)

		agentCfgs = append(agentCfgs, docker.AgentComposeConfig{
			Name:    spec.Name,
			WorkDir: agentWorkDirs[spec.Name],
			EnvFile: envFilePath,
			EnvVars: map[string]string{
				"BELAYER_SESSION_ID": sess.ID,
				"BELAYER_AGENT_ID":   spec.Name,
				"BELAYER_AGENT_CMD":  agentCmd,
				"BELAYER_HOST_UID":   hostUID,
				"BELAYER_HOST_GID":   hostGID,
				"BELAYER_SOCKET":     "/belayer/daemon.sock",
			},
			ExtraVolumes: []string{sessionVolume, socketVolume},
		})
	}

	// 6. Build compose config and bridge environment.
	composeCfg := docker.ComposeConfig{
		SessionID: sess.ID,
		Agents:    agentCfgs,
		Network:   docker.NetworkConfig{Type: envCfg.Networking.Type},
	}
	docker.BridgeEnvironment(envCfg, &composeCfg)

	// 7. Create and start sandbox.
	sandbox, err := docker.NewSandbox(docker.SandboxConfig{ComposeConfig: composeCfg})
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	if err := sandbox.Create(); err != nil {
		return fmt.Errorf("generate sandbox compose: %w", err)
	}
	if err := sandbox.Start(); err != nil {
		return fmt.Errorf("start sandbox: %w", err)
	}

	_ = c.LogEvent(sess.ID, "sandbox_started", mustJSON(map[string]string{
		"compose_dir": sandbox.ComposeDir(),
		"network":     envCfg.Networking.Type,
		"environment": environment,
	}))
	fmt.Fprintf(cmd.OutOrStdout(), "Docker sandbox started: %s (network: %s)\n", sandbox.ComposeDir(), envCfg.Networking.Type)

	return nil
}

// launchTmux starts agents in local tmux sessions.
func launchTmux(cmd *cobra.Command, c *Client, tmpl session.SessionTemplate, sess sessionResponse, agentNames []string, taskInput, workDir, sessionName string) error {
	runner := tmux.NewLocalRunner()

	// Create per-agent worktrees for isolation.
	wtBaseDir := filepath.Join(os.TempDir(), "belayer-worktrees", sessionName)
	agentWorkDirs := make(map[string]string, len(tmpl.Agents))
	for _, spec := range tmpl.Agents {
		wt, err := createWorktree(workDir, wtBaseDir, spec.Name, "")
		if err != nil {
			// Log warning but continue — fallback to shared workDir.
			fmt.Fprintf(cmd.OutOrStdout(), "  warning: worktree for %s: %v (using shared workdir)\n", spec.Name, err)
			agentWorkDirs[spec.Name] = workDir
		} else {
			agentWorkDirs[spec.Name] = wt
		}
	}

	for _, spec := range tmpl.Agents {
		team := make([]agent.TeamMember, 0, len(tmpl.Agents)-1)
		for _, s := range tmpl.Agents {
			if s.Name != spec.Name {
				team = append(team, agent.TeamMember{
					Name:   s.Name,
					Vendor: s.Vendor,
					Model:  s.Model,
					Role:   s.Role,
				})
			}
		}

		cfg := agent.AgentConfig{
			Name:         spec.Name,
			Vendor:       spec.Vendor,
			Model:        spec.Model,
			SystemPrompt: spec.SystemPrompt,
		}

		compiled := agent.CompilePrompt(agent.PromptContext{
			Config:    cfg,
			TaskInput: taskInput,
			SessionID: sess.ID,
			Team:      team,
		})

		sysPromptFile := filepath.Join(os.TempDir(), fmt.Sprintf("belayer-sysprompt-%s-%s.txt", sessionName, spec.Name))
		if err := os.WriteFile(sysPromptFile, []byte(compiled), 0600); err != nil {
			return fmt.Errorf("write system prompt for %s: %w", spec.Name, err)
		}
		taskFile := filepath.Join(os.TempDir(), fmt.Sprintf("belayer-task-%s-%s.txt", sessionName, spec.Name))
		if err := os.WriteFile(taskFile, []byte(taskInput), 0600); err != nil {
			return fmt.Errorf("write task file for %s: %w", spec.Name, err)
		}

		tmuxSessionName := fmt.Sprintf("%s-%s", sessionName, spec.Name)
		launchCmd := buildLaunchCmd(spec, sess.ID, sysPromptFile, taskFile, agentWorkDirs[spec.Name])

		if err := runner.CreateSession(tmuxSessionName, launchCmd); err != nil {
			return fmt.Errorf("create tmux session for %s: %w", spec.Name, err)
		}

		_ = c.LogEvent(sess.ID, "agent_launched", mustJSON(map[string]string{
			"agent":  spec.Name,
			"vendor": spec.Vendor,
			"model":  spec.Model,
		}))
	}

	return nil
}

// buildLaunchCmd constructs a safe shell command to run inside a tmux session.
// All user-controlled values are escaped via shell.Quote to prevent injection.
func buildLaunchCmd(spec session.AgentSpec, sessionID, sysPromptFile, taskFile, workDir string) string {
	modelDisplay := spec.Model
	if modelDisplay == "" {
		modelDisplay = "default"
	}

	// Build env exports with safe quoting.
	envExports := []string{
		"BELAYER_SESSION_ID=" + shell.Quote(sessionID),
		"BELAYER_AGENT_ID=" + shell.Quote(spec.Name),
	}
	for k, v := range spec.Env {
		if safe := shell.QuoteEnvKey(k); safe != "" {
			envExports = append(envExports, safe+"="+shell.Quote(v))
		}
	}

	var parts []string
	parts = append(parts, "export "+strings.Join(envExports, " "))
	parts = append(parts, fmt.Sprintf("cd %s 2>/dev/null", shell.Quote(workDir)))
	parts = append(parts, fmt.Sprintf("echo %s",
		shell.Quote(fmt.Sprintf("=== Belayer Agent: %s (%s/%s) ===", spec.Name, spec.Vendor, modelDisplay))))
	parts = append(parts, "echo \"Session: $BELAYER_SESSION_ID\"")
	parts = append(parts, "echo ''")
	parts = append(parts, buildVendorCmd(spec, sysPromptFile, taskFile))
	parts = append(parts, "echo ''")
	parts = append(parts, "echo 'Agent exited. Dropping to shell for debugging.'")
	parts = append(parts, "exec bash")

	return strings.Join(parts, "; ")
}

// createWorktree creates a git worktree from the repo at repoDir for the given session.
// Returns the worktree path. The worktree is created at baseDir/worktrees/<agentName>.
// If git worktree creation fails (e.g., not a git repo), falls back to repoDir.
func createWorktree(repoDir, baseDir, agentName, branch string) (string, error) {
	worktreePath := filepath.Join(baseDir, "worktrees", agentName)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		return repoDir, fmt.Errorf("create worktree dir: %w", err)
	}

	// Try to fetch latest from origin first (best effort).
	fetchCmd := exec.Command("git", "-C", repoDir, "fetch", "origin", "--quiet")
	fetchCmd.Run() // ignore errors — may be offline

	// Determine base branch.
	if branch == "" {
		branch = "origin/main"
		// Try origin/master if origin/main doesn't exist.
		checkCmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "origin/main")
		if checkCmd.Run() != nil {
			branch = "origin/master"
		}
	}

	// Create the worktree.
	args := []string{"-C", repoDir, "worktree", "add", worktreePath, "--detach", branch}
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return repoDir, fmt.Errorf("git worktree add: %s: %w", string(out), err)
	}

	return worktreePath, nil
}

// cleanupWorktrees removes git worktrees created for a session and returns any
// warnings encountered while attempting cleanup.
func cleanupWorktrees(baseDir string) []error {
	worktreeDir := filepath.Join(baseDir, "worktrees")
	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		return nil
	}
	var warnings []error
	for _, entry := range entries {
		if entry.IsDir() {
			wt := filepath.Join(worktreeDir, entry.Name())
			if err := removeGitWorktree(wt); err != nil {
				warnings = append(warnings, fmt.Errorf("remove worktree %s: %w", wt, err))
			}
		}
	}
	return warnings
}

func removeGitWorktree(worktreePath string) error {
	commonDirOut, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-common-dir").CombinedOutput()
	if err != nil {
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return fmt.Errorf("read git common dir: %w; fallback remove: %w", err, removeErr)
		}
		return fmt.Errorf("read git common dir: %w; fallback removed worktree path but git metadata may remain", err)
	}

	commonDir := strings.TrimSpace(string(commonDirOut))
	if commonDir == "" {
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return fmt.Errorf("empty git common dir; fallback remove: %w", removeErr)
		}
		return nil
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Clean(filepath.Join(worktreePath, commonDir))
	}

	repoDir := filepath.Dir(commonDir)
	if out, err := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath).CombinedOutput(); err != nil {
		trimmedOut := strings.TrimSpace(string(out))
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			return fmt.Errorf("git worktree remove: %w (%s); fallback remove: %w", err, trimmedOut, removeErr)
		}
		return fmt.Errorf("git worktree remove: %w (%s); fallback removed worktree path but git metadata may remain", err, trimmedOut)
	}
	return nil
}

// buildVendorCmd returns the vendor-specific CLI command string.
// All file paths and user-controlled values are safely quoted.
// Used by both tmux and Docker launch paths.
func buildVendorCmd(spec session.AgentSpec, sysPromptFile, taskFile string) string {
	qPrompt := shell.Quote(sysPromptFile)
	qTask := shell.Quote(taskFile)

	switch spec.Vendor {
	case "claude":
		cmd := "claude --dangerously-skip-permissions"
		if spec.MCPConfig != "" {
			cmd += " --mcp-config " + shell.Quote(spec.MCPConfig)
		}
		if spec.Settings != "" {
			cmd += " --settings " + shell.Quote(spec.Settings)
		}
		return cmd + " --system-prompt-file " + qPrompt + " \"$(cat " + qTask + ")\""
	case "opencode":
		model := "default"
		if spec.Model != "" {
			model = spec.Model
		}
		return "opencode -m " + shell.Quote(model) + " --prompt \"$(cat " + qPrompt + " " + qTask + ")\""
	case "codex":
		return "codex --dangerously-bypass-approvals-and-sandbox -c \"instructions=$(cat " + qPrompt + ")\" \"$(cat " + qTask + ")\""
	case "gemini":
		return "gemini --yolo -i \"$(cat " + qPrompt + " " + qTask + ")\""
	default:
		return "echo " + shell.Quote("No vendor CLI for: "+spec.Vendor) + "; cat " + qPrompt
	}
}
