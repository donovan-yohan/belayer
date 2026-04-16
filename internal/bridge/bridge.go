// Package bridge manages Python bridge subprocesses for the Belayer daemon.
// Each bridge process wraps a single Hermes agent run, communicating via stdin
// (daemon -> bridge commands) and writing stdout/stderr to log files in RunDir.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// defaultPythonCmd returns the command used to launch the bridge subprocess.
// It uses Hermes's venv Python so that Hermes dependencies (openai, etc.) are available.
func defaultPythonCmd() []string {
	home, _ := os.UserHomeDir()
	venvPython := filepath.Join(home, ".hermes", "hermes-agent", "venv", "bin", "python3")
	if _, err := os.Stat(venvPython); err == nil {
		return []string{venvPython, "-m", "hermes_bridge"}
	}
	// Fallback to system python3 if venv not found.
	return []string{"python3", "-m", "hermes_bridge"}
}

// Config holds everything needed to spawn one bridge subprocess.
type Config struct {
	SessionID       string
	AgentID         string
	Role            string
	Profile         string
	Workdir         string
	SocketPath      string // daemon Unix socket path
	RunDir          string // e.g. /workspace/.belayer/runs/{session}/{agent}
	Model           string // optional model override
	Message         string // initial message/instructions for the agent
	SystemPrompt    string // optional system prompt injected via ephemeral_system_prompt
	HermesSessionID string // for crash recovery resume
	BelayerRoot     string // directory containing hermes_bridge/ package (for PYTHONPATH)
	Ephemeral       bool   // true = exit on task completion, false = stay alive for more work

	// Cmd overrides the default python3 -m hermes_bridge command.
	// If nil, pythonCmd is used. Useful for testing.
	Cmd []string
}

// Process wraps a running bridge subprocess.
type Process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{} // closed when process exits
	exitErr error        // set before done is closed
	mu      sync.Mutex
}

// Spawn starts a hermes-bridge subprocess with the given config.
// Stdout and stderr from the subprocess are tee'd to log files in RunDir.
// Returns a Process handle for monitoring and communication.
func Spawn(cfg Config) (*Process, error) {
	if err := os.MkdirAll(cfg.RunDir, 0o700); err != nil {
		return nil, fmt.Errorf("bridge: create run dir: %w", err)
	}

	argv := cfg.Cmd
	if len(argv) == 0 {
		argv = defaultPythonCmd()
	}

	//nolint:gosec // argv is controlled by internal callers, not user input
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.Workdir

	// Build environment: inherit parent env, then layer bridge-specific vars.
	env := os.Environ()

	// Set PYTHONPATH so the bridge can import hermes-agent modules and hermes_bridge.
	// Hermes's run_agent.py, hermes_state.py, tools/ etc. live in ~/.hermes/hermes-agent/.
	// The hermes_bridge package lives at BelayerRoot (the belayer repo root).
	home, _ := os.UserHomeDir()
	hermesAgent := filepath.Join(home, ".hermes", "hermes-agent")
	sep := string(os.PathListSeparator)
	pyPath := hermesAgent
	if cfg.BelayerRoot != "" {
		pyPath = cfg.BelayerRoot + sep + pyPath
	}
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		pyPath = pyPath + sep + existing
	}
	env = appendEnv(env, "PYTHONPATH", pyPath)

	env = appendEnv(env, "BELAYER_SESSION_ID", cfg.SessionID)
	env = appendEnv(env, "BELAYER_AGENT_ID", cfg.AgentID)
	env = appendEnv(env, "BELAYER_SOCKET", cfg.SocketPath)
	env = appendEnv(env, "BELAYER_RUN_DIR", cfg.RunDir)
	env = appendEnv(env, "BELAYER_ROLE", cfg.Role)
	env = appendEnv(env, "BELAYER_PROFILE", cfg.Profile)
	if cfg.Model != "" {
		env = appendEnv(env, "BELAYER_MODEL", cfg.Model)
	}
	if cfg.Message != "" {
		env = appendEnv(env, "BELAYER_MESSAGE", cfg.Message)
	}
	if cfg.SystemPrompt != "" {
		env = appendEnv(env, "BELAYER_SYSTEM_PROMPT", cfg.SystemPrompt)
	}
	if cfg.HermesSessionID != "" {
		env = appendEnv(env, "BELAYER_HERMES_SESSION_ID", cfg.HermesSessionID)
	}
	if !cfg.Ephemeral {
		env = appendEnv(env, "BELAYER_EPHEMERAL", "false")
	}
	cmd.Env = env

	// Pipe stdin so the daemon can send interrupt/stop commands.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge: create stdin pipe: %w", err)
	}

	// Log stdout and stderr to files in RunDir.
	stdoutLog, err := os.OpenFile(filepath.Join(cfg.RunDir, "bridge-stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("bridge: open stdout log: %w", err)
	}
	stderrLog, err := os.OpenFile(filepath.Join(cfg.RunDir, "bridge-stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		stdinPipe.Close()
		stdoutLog.Close()
		return nil, fmt.Errorf("bridge: open stderr log: %w", err)
	}

	cmd.Stdout = io.MultiWriter(os.Stdout, stdoutLog)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrLog)

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		stdoutLog.Close()
		stderrLog.Close()
		return nil, fmt.Errorf("bridge: start subprocess: %w", err)
	}

	p := &Process{
		cmd:   cmd,
		stdin: stdinPipe,
		done:  make(chan struct{}),
	}

	go func() {
		waitErr := cmd.Wait()
		stdoutLog.Close()
		stderrLog.Close()
		p.mu.Lock()
		p.exitErr = waitErr
		p.mu.Unlock()
		close(p.done)
	}()

	return p, nil
}

// WriteStdin marshals v to a JSON line and writes it to the bridge's stdin.
// It is safe to call from multiple goroutines.
func (p *Process) WriteStdin(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("bridge: marshal stdin payload: %w", err)
	}
	data = append(data, '\n')

	p.mu.Lock()
	defer p.mu.Unlock()

	_, err = p.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("bridge: write stdin: %w", err)
	}
	return nil
}

// Interrupt sends an interrupt command via stdin.
func (p *Process) Interrupt(from, content string) error {
	return p.WriteStdin(map[string]string{
		"type":    "interrupt",
		"from":    from,
		"content": content,
	})
}

// Stop sends a stop command via stdin and waits for the process to exit
// gracefully. If the process does not exit within timeout, it is killed.
func (p *Process) Stop(timeout time.Duration) error {
	if err := p.WriteStdin(map[string]string{"type": "stop"}); err != nil {
		// If writing fails (e.g. pipe already closed), fall through to kill.
		_ = err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case <-p.done:
		p.mu.Lock()
		err := p.exitErr
		p.mu.Unlock()
		return err
	case <-ctx.Done():
		// Graceful wait timed out — force kill.
		if killErr := p.cmd.Process.Kill(); killErr != nil {
			return fmt.Errorf("bridge: kill after stop timeout: %w", killErr)
		}
		<-p.done
		return fmt.Errorf("bridge: process killed after stop timeout (%s)", timeout)
	}
}

// Wait blocks until the process exits and returns the exit error (may be nil).
func (p *Process) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// Done returns a channel that is closed when the process exits.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// ExitErr returns the process exit error. Only valid after Done() is closed.
func (p *Process) ExitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// appendEnv appends KEY=VALUE to env, replacing any existing entry for KEY.
func appendEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
