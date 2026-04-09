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
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newSessionStartCmd() *cobra.Command {
	var template, input, name, repo, socket, attachAgent string
	var attach, dockerMode bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a session from a template",
		Long: `Load a session template, create the session via the daemon, compile
agent prompts, and launch each agent in a tmux pane (or Docker container
with --docker).

Templates are loaded from .belayer/templates/<name>.yaml in the workspace,
falling back to built-in defaults.`,
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
				agentCfgs := make([]docker.AgentComposeConfig, 0, len(tmpl.Agents))
				for _, spec := range tmpl.Agents {
					agentCfgs = append(agentCfgs, docker.AgentComposeConfig{
						Name:    spec.Name,
						WorkDir: workDir,
						EnvVars: map[string]string{
							"BELAYER_SESSION_ID": sess.ID,
							"BELAYER_AGENT_ID":   spec.Name,
						},
					})
				}
				cfg := docker.SandboxConfig{
					ComposeConfig: docker.ComposeConfig{
						SessionID: sess.ID,
						Agents:    agentCfgs,
						Network:   docker.NetworkConfig{Type: "none"},
					},
				}
				sandbox, err := docker.NewSandbox(cfg)
				if err != nil {
					return fmt.Errorf("create sandbox: %w", err)
				}
				if err := sandbox.Create(); err != nil {
					return fmt.Errorf("create sandbox compose: %w", err)
				}
				if err := sandbox.Start(); err != nil {
					return fmt.Errorf("start sandbox: %w", err)
				}
				_ = c.LogEvent(sess.ID, "sandbox_started", mustJSON(map[string]string{
					"compose_dir": sandbox.ComposeDir(),
				}))
				fmt.Fprintf(cmd.OutOrStdout(), "Docker sandbox started at: %s\n", sandbox.ComposeDir())
			} else {
				runner := tmux.NewLocalRunner()

				for _, spec := range tmpl.Agents {
					others := make([]string, 0, len(agentNames)-1)
					for _, n := range agentNames {
						if n != spec.Name {
							others = append(others, n)
						}
					}

					cfg := agent.AgentConfig{
						Name:         spec.Name,
						Vendor:       spec.Vendor,
						Model:        spec.Model,
						SystemPrompt: spec.SystemPrompt,
					}

					compiled := agent.CompilePrompt(agent.PromptContext{
						Config:      cfg,
						TaskInput:   taskInput,
						SessionID:   sess.ID,
						OtherAgents: others,
					})

					sysPromptFile := filepath.Join(os.TempDir(), fmt.Sprintf("belayer-sysprompt-%s-%s.txt", name, spec.Name))
					if err := os.WriteFile(sysPromptFile, []byte(compiled), 0600); err != nil {
						return fmt.Errorf("write system prompt for %s: %w", spec.Name, err)
					}
					taskFile := filepath.Join(os.TempDir(), fmt.Sprintf("belayer-task-%s-%s.txt", name, spec.Name))
					if err := os.WriteFile(taskFile, []byte(taskInput), 0600); err != nil {
						return fmt.Errorf("write task file for %s: %w", spec.Name, err)
					}

					tmuxSessionName := fmt.Sprintf("%s-%s", name, spec.Name)
					launchCmd := buildLaunchCmd(spec, sess.ID, sysPromptFile, taskFile, workDir)

					if err := runner.CreateSession(tmuxSessionName, launchCmd); err != nil {
						return fmt.Errorf("create tmux session for %s: %w", spec.Name, err)
					}

					_ = c.LogEvent(sess.ID, "agent_launched", mustJSON(map[string]string{
						"agent":  spec.Name,
						"vendor": spec.Vendor,
						"model":  spec.Model,
					}))
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
	return cmd
}

// buildLaunchCmd constructs the shell command to run inside a tmux session for an agent.
func buildLaunchCmd(spec session.AgentSpec, sessionID, sysPromptFile, taskFile, workDir string) string {
	modelDisplay := spec.Model
	if modelDisplay == "" {
		modelDisplay = "default"
	}

	// Build optional flags from agent config.
	var mcpFlag, settingsFlag string
	if spec.MCPConfig != "" {
		mcpFlag = fmt.Sprintf(` --mcp-config '%s'`, spec.MCPConfig)
	}
	if spec.Settings != "" {
		settingsFlag = fmt.Sprintf(` --settings '%s'`, spec.Settings)
	}

	var vendorCmd string
	switch spec.Vendor {
	case "claude":
		vendorCmd = fmt.Sprintf(`claude --dangerously-skip-permissions%s%s --system-prompt-file '%s' "$(cat '%s')"`,
			mcpFlag, settingsFlag, sysPromptFile, taskFile)
	case "opencode":
		vendorCmd = fmt.Sprintf(`opencode -m %s --prompt "$(cat '%s' '%s')"`, spec.Model, sysPromptFile, taskFile)
	case "codex":
		vendorCmd = fmt.Sprintf(`codex --dangerously-bypass-approvals-and-sandbox -c "instructions=$(cat '%s')" "$(cat '%s')"`, sysPromptFile, taskFile)
	case "gemini":
		vendorCmd = fmt.Sprintf(`gemini --yolo -i "$(cat '%s' '%s')"`, sysPromptFile, taskFile)
	default:
		vendorCmd = fmt.Sprintf(`echo "No vendor CLI for: %s"; cat '%s'`, spec.Vendor, sysPromptFile)
	}

	// Export agent-level env vars.
	var envExports string
	for k, v := range spec.Env {
		envExports += fmt.Sprintf(` %s="%s"`, k, v)
	}

	return fmt.Sprintf(
		`bash -c 'export BELAYER_SESSION_ID="%s" BELAYER_AGENT_ID="%s"%s; cd "%s" 2>/dev/null; echo "=== Belayer Agent: %s (%s/%s) ==="; echo "Session: $BELAYER_SESSION_ID"; echo ""; %s; echo ""; echo "Agent exited. Dropping to shell for debugging."; exec bash'`,
		sessionID, spec.Name, envExports, workDir,
		spec.Name, spec.Vendor, modelDisplay,
		vendorCmd,
	)
}

// capitalize returns the string with its first letter upper-cased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
