package hermes

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/shell"
)

// LaunchConfig captures the minimum information needed to start a Hermes agent.
type LaunchConfig struct {
	Profile    string
	Workdir    string
	SocketPath string
	SessionID  string
	AgentID    string
	RunDir     string
	Skills     []string
	ExtraEnv   map[string]string
}

// BuildLaunchCmd returns a shell command suitable for running inside tmux.
func BuildLaunchCmd(cfg LaunchConfig) (string, error) {
	if strings.TrimSpace(cfg.Profile) == "" {
		return "", fmt.Errorf("hermes: profile is required")
	}
	if strings.TrimSpace(cfg.Workdir) == "" {
		return "", fmt.Errorf("hermes: workdir is required")
	}
	if strings.TrimSpace(cfg.RunDir) == "" {
		return "", fmt.Errorf("hermes: run dir is required")
	}
	if strings.TrimSpace(cfg.SessionID) == "" {
		return "", fmt.Errorf("hermes: session ID is required")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return "", fmt.Errorf("hermes: agent ID is required")
	}
	if strings.TrimSpace(cfg.SocketPath) == "" {
		return "", fmt.Errorf("hermes: socket path is required")
	}

	parts := []string{fmt.Sprintf("cd %s", shell.Quote(cfg.Workdir))}
	env := map[string]string{
		"BELAYER_SESSION_ID":          cfg.SessionID,
		"BELAYER_AGENT_ID":            cfg.AgentID,
		"BELAYER_SOCKET":              cfg.SocketPath,
		"BELAYER_RUN_DIR":             cfg.RunDir,
		"HERMES_ENABLE_PROJECT_PLUGINS": "true",
	}
	for k, v := range cfg.ExtraEnv {
		env[k] = v
	}
	for k, v := range env {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("export %s=%s", k, shell.Quote(v)))
	}
	cmd := fmt.Sprintf("hermes --profile %s", shell.Quote(cfg.Profile))
	if len(cfg.Skills) > 0 {
		cmd += " --skills " + shell.Quote(strings.Join(cfg.Skills, ","))
	}
	marker := shell.Quote(filepath.Join(cfg.RunDir, ".belayer-finished"))
	parts = append(parts, fmt.Sprintf("rm -f %s", marker))
	parts = append(parts, cmd)

	// Post-hermes parts use ";" so they run regardless of hermes exit code.
	postParts := []string{
		"status=$?",
		fmt.Sprintf("if [ ! -f %s ]; then belayer finish --blocked %s >/dev/null 2>&1; fi", marker, shell.Quote("hook: Hermes process exited without explicit belayer finish")),
		"exit $status",
	}
	return strings.Join(parts, " && ") + "; " + strings.Join(postParts, "; "), nil
}
