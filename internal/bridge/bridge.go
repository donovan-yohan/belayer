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
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/daemon/bridgelog"
)

// defaultPythonCmd returns the command used to launch the bridge subprocess.
// It uses Hermes's venv Python so that Hermes dependencies (openai, etc.) are available.
func defaultPythonCmd() []string {
	root := hermesAgentRoot()
	if root != "" {
		venvPython := filepath.Join(root, "venv", "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil {
			return []string{venvPython, "-m", "hermes_bridge"}
		}
	}
	// Fallback to system python3 if venv not found. Expected in bundled
	// images where `pip install --break-system-packages` populates the
	// system site-packages instead of a dedicated venv.
	return []string{"python3", "-m", "hermes_bridge"}
}

// hermesAgentRoot returns the directory containing the hermes-agent source
// tree and (optionally) its venv. It honours HERMES_AGENT_PATH so containerised
// builds can relocate the install off the user home, then falls back to
// $HOME/.hermes/hermes-agent for dev machines.
func hermesAgentRoot() string {
	if p := strings.TrimSpace(os.Getenv("HERMES_AGENT_PATH")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hermes", "hermes-agent")
}

// Config holds everything needed to spawn one bridge subprocess.
type Config struct {
	SessionID       string
	AgentID         string
	Role            string
	Profile         string
	Workdir         string
	SocketPath      string // daemon Unix socket path or http://host:port for TCP
	HTTPProxy       string // HTTP CONNECT proxy for clamshell (e.g. http://172.31.0.2:3128)
	RunDir          string // e.g. /workspace/.belayer/runs/{session}/{agent}
	Model           string // optional model override
	APIKey          string // LLM provider API key (injected when Hermes config is unavailable, e.g. clamshell)
	BaseURL         string // LLM provider base URL (e.g. https://opencode.ai/zen/go/v1)
	Provider        string // LLM provider name (e.g. "openai")
	Message         string // initial message/instructions for the agent
	SystemPrompt    string // optional system prompt injected via ephemeral_system_prompt
	HermesSessionID string // for crash recovery resume
	BelayerRoot     string   // directory containing hermes_bridge/ package (for PYTHONPATH)
	Ephemeral       bool     // true = exit on task completion, false = stay alive for more work
	BelayerTools    []string // role-specific belayer tools from agent.yaml
	TranscriptPath      string   // absolute path to per-agent JSONL; empty = capture disabled (standard log level)
	LogLevel            string   // "standard", "verbose", or "trace"; empty treated as "standard"
	SkipOpenRouterProbe bool     // when true, injects HERMES_SKIP_OPENROUTER_PROBE=1 to suppress the openrouter metadata fetch at bridge startup

	// WriteRoots is the set of absolute paths the agent is allowed to write to.
	// When ConfineWrites is true and this slice is non-empty, the bridge
	// subprocess is launched via belayer-landlock-exec which applies a
	// Landlock v2 ruleset: read-only globally, read+write for each root.
	// /tmp is always included by the daemon so compilers and package managers work.
	WriteRoots []string

	// ConfineWrites, when true and WriteRoots is non-empty, wraps the bridge
	// argv in belayer-landlock-exec to enforce kernel-level write confinement.
	// When false (the default), no wrapping is applied.
	ConfineWrites bool

	// Cmd overrides the default python3 -m hermes_bridge command.
	// If nil, pythonCmd is used. Useful for testing.
	Cmd []string
}

// ProcessHandle is the minimal contract bridge.NewProcess needs from a process
// started by an external driver (e.g. sandbox.Process). Wait must block until
// the process has fully exited — including any stdio pump goroutines — so the
// daemon can safely close log writers once Wait returns.
type ProcessHandle interface {
	Wait() error
	Kill() error
}

// Process wraps a running bridge subprocess.
type Process struct {
	cmd     *exec.Cmd
	handle  ProcessHandle // set by NewProcess; drives Wait/Kill when cmd is nil
	stdin   io.WriteCloser
	done    chan struct{} // closed when process exits
	exitErr error         // set before done is closed
	mu      sync.Mutex

	// scanner, when non-nil, tees bridge stdout and emits StdoutErrors for
	// known LLM/API failure patterns. Only set by Spawn (not NewProcess).
	scanner *stdoutScanner

	// firstEvent is closed the first time the bridge posts an event to the daemon.
	// Used by spawn callers to distinguish a live bridge from one that crashes
	// during startup (Gap 14).
	firstEvent     chan struct{}
	firstEventOnce sync.Once
}

// BuildCmd returns the argv slice to use when launching the bridge subprocess.
// If cfg.Cmd is non-empty it is used as the base command; otherwise the
// default python command (Hermes venv python3 -m hermes_bridge) is used.
// When cfg.ConfineWrites is true and cfg.WriteRoots is non-empty, the argv is
// prepended with "belayer-landlock-exec" so the kernel-enforced Landlock
// ruleset is applied before exec-replacing into the bridge process.
func BuildCmd(cfg Config) []string {
	var base []string
	if len(cfg.Cmd) > 0 {
		base = cfg.Cmd
	} else {
		base = defaultPythonCmd()
	}
	if cfg.ConfineWrites && len(cfg.WriteRoots) > 0 {
		return append([]string{"belayer-landlock-exec"}, base...)
	}
	return base
}

// BuildEnv builds the complete environment variable slice for the bridge
// subprocess. It inherits the parent process environment, then layers
// PYTHONPATH (hermes-agent path + BelayerRoot + existing PYTHONPATH) and all
// BELAYER_* variables derived from cfg.
func BuildEnv(cfg Config) []string {
	// Build environment: inherit parent env, then layer bridge-specific vars.
	env := os.Environ()

	// Set PYTHONPATH so the bridge can import hermes-agent modules and hermes_bridge.
	// Hermes's run_agent.py, hermes_state.py, tools/ etc. live at
	// $HERMES_AGENT_PATH (or ~/.hermes/hermes-agent by default on dev hosts).
	// The hermes_bridge package lives at BelayerRoot (the belayer repo root).
	// Build from non-empty components so we never emit a leading/trailing/empty
	// segment (an empty segment Python would interpret as cwd).
	var parts []string
	if cfg.BelayerRoot != "" {
		parts = append(parts, cfg.BelayerRoot)
	}
	if hermesAgent := hermesAgentRoot(); hermesAgent != "" {
		parts = append(parts, hermesAgent)
	}
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		parts = append(parts, existing)
	}
	if len(parts) > 0 {
		env = appendEnv(env, "PYTHONPATH", strings.Join(parts, string(os.PathListSeparator)))
	}

	env = appendEnv(env, "BELAYER_SESSION_ID", cfg.SessionID)
	env = appendEnv(env, "BELAYER_AGENT_ID", cfg.AgentID)
	env = appendEnv(env, "BELAYER_SOCKET", cfg.SocketPath)
	if cfg.HTTPProxy != "" {
		env = appendEnv(env, "BELAYER_HTTP_PROXY", cfg.HTTPProxy)
		// Inject standard proxy env vars so Python's httpx/requests/urllib route
		// LLM API calls through the sandbox egress broker. The docker exec env-file
		// only contains what we pass — the container's startup env is not inherited.
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
			env = appendEnv(env, k, cfg.HTTPProxy)
		}
		env = appendEnv(env, "NO_PROXY", "127.0.0.1,localhost,::1")
		env = appendEnv(env, "no_proxy", "127.0.0.1,localhost,::1")
	}
	env = appendEnv(env, "BELAYER_RUN_DIR", cfg.RunDir)
	env = appendEnv(env, "BELAYER_ROLE", cfg.Role)
	env = appendEnv(env, "BELAYER_PROFILE", cfg.Profile)
	if cfg.Model != "" {
		env = appendEnv(env, "BELAYER_MODEL", cfg.Model)
	}
	if cfg.APIKey != "" {
		env = appendEnv(env, "BELAYER_API_KEY", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		env = appendEnv(env, "BELAYER_BASE_URL", cfg.BaseURL)
	}
	if cfg.Provider != "" {
		env = appendEnv(env, "BELAYER_PROVIDER", cfg.Provider)
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
	if len(cfg.BelayerTools) > 0 {
		env = appendEnv(env, "BELAYER_TOOLS", strings.Join(cfg.BelayerTools, ","))
	}
	if cfg.TranscriptPath != "" {
		env = appendEnv(env, "BELAYER_TRANSCRIPT_PATH", cfg.TranscriptPath)
	}
	level := cfg.LogLevel
	if level == "" {
		level = "standard"
	}
	env = appendEnv(env, "BELAYER_LOG_LEVEL", level)
	if cfg.SkipOpenRouterProbe {
		env = appendEnv(env, "HERMES_SKIP_OPENROUTER_PROBE", "1")
	}
	if cfg.ConfineWrites && len(cfg.WriteRoots) > 0 {
		env = appendEnv(env, "BELAYER_WRITE_ROOTS", strings.Join(cfg.WriteRoots, ":"))
	}
	return env
}

// NewProcess creates a Process that wraps an already-started ProcessHandle.
// This is used when process execution is handled by an external sandbox driver
// rather than by Spawn(). The handle's Wait is expected to synchronize with
// stdio pumps so the caller can close log writers after Done fires.
func NewProcess(handle ProcessHandle, stdin io.WriteCloser) *Process {
	p := &Process{
		handle:     handle,
		stdin:      stdin,
		done:       make(chan struct{}),
		firstEvent: make(chan struct{}),
	}
	go func() {
		waitErr := handle.Wait()
		p.mu.Lock()
		p.exitErr = waitErr
		p.mu.Unlock()
		close(p.done)
	}()
	return p
}

// Spawn starts a hermes-bridge subprocess with the given config.
// Stdout and stderr from the subprocess are tee'd to log files in RunDir.
// Returns a Process handle for monitoring and communication.
func Spawn(cfg Config) (*Process, error) {
	if err := os.MkdirAll(cfg.RunDir, 0o700); err != nil {
		return nil, fmt.Errorf("bridge: create run dir: %w", err)
	}

	argv := BuildCmd(cfg)

	//nolint:gosec // argv is controlled by internal callers, not user input
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.Workdir

	cmd.Env = BuildEnv(cfg)

	// Pipe stdin so the daemon can send interrupt/stop commands.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge: create stdin pipe: %w", err)
	}

	// Rotate and open per-spawn stdout/stderr logs under RunDir. Rotation keeps
	// the previous 3 spawns' output so operators can diff runs after a crash.
	stdoutLog, err := bridgelog.RotateAndOpen(filepath.Join(cfg.RunDir, "bridge-stdout.log"), 3)
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("bridge: open stdout log: %w", err)
	}
	stderrLog, err := bridgelog.RotateAndOpen(filepath.Join(cfg.RunDir, "bridge-stderr.log"), 3)
	if err != nil {
		stdinPipe.Close()
		stdoutLog.Close()
		return nil, fmt.Errorf("bridge: open stderr log: %w", err)
	}

	// Stderr goes directly to both os.Stderr and the log file.
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrLog)

	// Stdout is piped through the scanner (which tees into the log file and
	// watches for API/network error markers) instead of being assigned directly
	// to cmd.Stdout. The scanner pump goroutine is started after cmd.Start().
	stdoutPipeR, stdoutPipeW := io.Pipe()
	cmd.Stdout = stdoutPipeW

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		stdoutLog.Close()
		stderrLog.Close()
		stdoutPipeR.Close()
		stdoutPipeW.Close()
		return nil, fmt.Errorf("bridge: start subprocess: %w", err)
	}

	sc := newStdoutScanner()
	p := &Process{
		cmd:        cmd,
		stdin:      stdinPipe,
		done:       make(chan struct{}),
		firstEvent: make(chan struct{}),
		scanner:    sc,
	}

	// pumpDone signals that the scanner pump goroutine has finished draining
	// stdout. We wait for it before closing done so callers that read log files
	// after <-proc.Done() see the complete output.
	pumpDone := make(chan struct{})

	// Start the scanner pump: reads from the pipe, tees to os.Stdout + log file,
	// and scans each line for error markers. Closes pumpDone when the pipe EOF
	// is reached (write-end closed by the goroutine below after cmd.Wait).
	go func() {
		sc.pump(stdoutPipeR, io.MultiWriter(os.Stdout, stdoutLog))
		stdoutPipeR.Close()
		close(sc.errors)
		close(pumpDone)
	}()

	go func() {
		// cmd.Wait blocks until the subprocess exits AND all OS-level I/O is
		// drained (including the stderr pipe). For stdout we use our own pipe;
		// closing stdoutPipeW causes the scanner goroutine to see EOF.
		waitErr := cmd.Wait()
		stdoutPipeW.Close()
		// Wait for the pump goroutine to finish writing to stdoutLog before
		// closing it, so callers that read the file after Done() see full output.
		<-pumpDone
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
		// Graceful wait timed out — force kill via whichever backend owns
		// the process.
		var killErr error
		switch {
		case p.handle != nil:
			killErr = p.handle.Kill()
		case p.cmd != nil && p.cmd.Process != nil:
			killErr = p.cmd.Process.Kill()
		}
		if killErr != nil {
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

// MarkLive signals that the bridge has posted its first event to the daemon.
// It is idempotent; subsequent calls are no-ops. The daemon calls this from
// handleLogEvent the first time any bridge:* event arrives for this process,
// so spawn callers can distinguish a healthy startup from an immediate crash.
func (p *Process) MarkLive() {
	p.firstEventOnce.Do(func() { close(p.firstEvent) })
}

// FirstEvent returns a channel that is closed after the first bridge event is
// received by the daemon. Use in a select alongside Done() and time.After to
// detect startup failures before watchBridgeExit fires.
func (p *Process) FirstEvent() <-chan struct{} { return p.firstEvent }

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
