//go:build clamshell

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func init() {
	Register("clamshell", New())
}

// Clamshell drives the external `clamshell` CLI (plus `docker exec` for agent
// spawns). It is compiled in only when -tags clamshell is set; the stub in
// clamshell_stub.go stands in on default builds so sandbox.mode: clamshell
// surfaces a clear error instead of a "not registered" one.
type Clamshell struct {
	// cli is the name (or path) of the clamshell binary. Default "clamshell".
	cli string
	// docker is the name (or path) of the docker binary. Default "docker".
	docker string
	// runner abstracts process execution so tests can mock the CLI and
	// docker exec calls without needing a real Lima VM.
	runner commandRunner
}

// New builds a Clamshell with production defaults (resolves `clamshell` and
// `docker` via PATH).
func New() *Clamshell {
	return &Clamshell{
		cli:    "clamshell",
		docker: "docker",
		runner: osRunner{},
	}
}

// commandRunner abstracts process execution for tests. Production wires this
// to osRunner; tests inject a fake.
type commandRunner interface {
	// Run executes name with args to completion and returns captured
	// stdout/stderr along with any exit error.
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
	// Start launches name with args, wiring stdio via opts, and returns a
	// Process the caller can Wait/Kill.
	Start(ctx context.Context, name string, args []string, opts ExecOpts) (Process, error)
}

// osRunner is the production commandRunner. It delegates to os/exec.
type osRunner struct{}

func (osRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	//nolint:gosec // name/args are controlled by Clamshell, not user input
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (osRunner) Start(ctx context.Context, name string, args []string, opts ExecOpts) (Process, error) {
	//nolint:gosec // name/args are controlled by Clamshell, not user input
	c := exec.CommandContext(ctx, name, args...)
	c.Env = opts.Env
	c.Dir = opts.Dir
	c.Stdin = opts.Stdin
	c.Stdout = opts.Stdout
	c.Stderr = opts.Stderr
	if err := c.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: c}, nil
}

// osProcess wraps *exec.Cmd. We use cmd.Wait (not cmd.Process.Wait) so that
// any stdio pump goroutines spawned by exec have finished before Wait returns.
type osProcess struct{ cmd *exec.Cmd }

func (p *osProcess) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *osProcess) Wait() error { return p.cmd.Wait() }

func (p *osProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Create provisions a clamshell sandbox. It ensures the clamshell gateway is
// up, merges runtime endpoints into the supplied policy, creates the sandbox,
// and then discovers the backing Docker container so Exec can target it.
func (c *Clamshell) Create(ctx context.Context, cfg Config) (Handle, error) {
	// 1. Ensure the gateway is running. `clamshell gateway start` is expected
	//    to be idempotent; surfacing errors here catches misconfigured workers.
	if _, stderr, err := c.runner.Run(ctx, c.cli, "gateway", "start"); err != nil {
		return Handle{}, fmt.Errorf("sandbox/clamshell: gateway start: %w (stderr: %s)", err, stderr)
	}

	// 2. Inject runtime endpoints into the policy's tcp_endpoints section and
	//    write the merged copy to a temp file. The temp file lives for the
	//    session — Stop cleans it up via handle.Meta["policyFile"].
	policyPath, err := c.preparePolicy(cfg.Policy, cfg.Endpoints)
	if err != nil {
		return Handle{}, err
	}

	// 3. Create the sandbox.
	createArgs := []string{"sandbox", "create",
		"--name", cfg.Name,
		"--policy", policyPath,
		"--workspace", cfg.Workspace,
	}
	if _, stderr, err := c.runner.Run(ctx, c.cli, createArgs...); err != nil {
		_ = os.Remove(policyPath)
		return Handle{}, fmt.Errorf("sandbox/clamshell: create: %w (stderr: %s)", err, stderr)
	}

	// After this point the sandbox exists in clamshell. Any failure must tear
	// it down, otherwise we leak a running sandbox on every bad discovery.
	cleanupAfterCreate := func(cause error) error {
		if _, stopStderr, stopErr := c.runner.Run(ctx, c.cli, "sandbox", "stop", cfg.Name); stopErr != nil {
			cause = fmt.Errorf("%w; additionally, cleanup stop failed: %v (stderr: %s)", cause, stopErr, stopStderr)
		}
		_ = os.Remove(policyPath)
		return cause
	}

	// 4. Discover the container name so Exec can docker-exec into it. The
	//    --json form emits a structured response clamshell guarantees stable.
	connectOut, stderr, err := c.runner.Run(ctx, c.cli, "--json", "sandbox", "connect", cfg.Name)
	if err != nil {
		return Handle{}, cleanupAfterCreate(fmt.Errorf("sandbox/clamshell: connect: %w (stderr: %s)", err, stderr))
	}
	var connect struct {
		Container string   `json:"container"`
		Argv      []string `json:"argv"`
	}
	if err := json.Unmarshal(connectOut, &connect); err != nil {
		return Handle{}, cleanupAfterCreate(fmt.Errorf("sandbox/clamshell: parse connect output: %w (stdout: %s)", err, connectOut))
	}
	// Newer clamshell versions may return argv=[docker exec -it <container> /bin/bash]
	// instead of a direct container field; extract the container name from argv[3].
	if connect.Container == "" && len(connect.Argv) >= 4 {
		connect.Container = connect.Argv[3]
	}
	if connect.Container == "" {
		return Handle{}, cleanupAfterCreate(fmt.Errorf("sandbox/clamshell: connect output missing container field: %s", connectOut))
	}

	return Handle{
		ID: cfg.Name,
		Meta: map[string]string{
			"container":     connect.Container,
			"policyFile":    policyPath,
			"hostWorkspace": cfg.Workspace,
			// clamshell always mounts the sandbox home at {runtime_dir}/home.
			// runtime_dir defaults to /run/agent per clamshell policy convention.
			"containerHome": "/run/agent/home",
		},
	}, nil
}

// sandboxWorkspace is where Clamshell mounts the host workspace inside the
// container. The clamshell CLI guarantees this path; Exec rewrites host-side
// Dir values that fall under it to the container-side equivalent.
const sandboxWorkspace = "/workspace"

// translateDir rewrites a host-path Dir to the container-side path when it
// lives under the sandbox's host workspace. Paths that are empty, already
// container-side, or outside the workspace are returned unchanged so callers
// that pass raw container paths (or leave Dir empty) still work.
func translateDir(dir, hostWorkspace string) string {
	if dir == "" || hostWorkspace == "" {
		return dir
	}
	if !filepath.IsAbs(dir) {
		return dir
	}
	rel, err := filepath.Rel(hostWorkspace, dir)
	if err != nil {
		return dir
	}
	if rel == "." {
		return sandboxWorkspace
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dir
	}
	return sandboxWorkspace + "/" + filepath.ToSlash(rel)
}

// Exec launches cmd inside the sandbox container via `docker exec`.
// Environment variables are passed through a mode-0600 env-file referenced by
// `--env-file` rather than folded into the argv. This keeps secrets out of
// the host process table (`ps auxe` would otherwise expose them). The command
// runs under `sh -lc` so shell conveniences (pipes, globs, login profile) work
// as they would in a normal shell session.
func (c *Clamshell) Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (Process, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("sandbox/clamshell: exec requires at least one argument")
	}
	container, ok := h.Meta["container"]
	if !ok || container == "" {
		return nil, fmt.Errorf("sandbox/clamshell: exec handle %q missing container metadata", h.ID)
	}

	args := []string{"exec", "-u", "sandbox", "-i"}
	envFile := ""
	env := opts.Env
	// Override HOME with the container home dir so the bridge doesn't try to
	// write to the host user's home path (which is read-only inside the container).
	if containerHome := h.Meta["containerHome"]; containerHome != "" {
		replaced := false
		for i, e := range env {
			if strings.HasPrefix(e, "HOME=") {
				env[i] = "HOME=" + containerHome
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, "HOME="+containerHome)
		}
	}
	if len(env) > 0 {
		path, err := writeEnvFile(env)
		if err != nil {
			return nil, err
		}
		envFile = path
		args = append(args, "--env-file", envFile)
	}
	args = append(args, container)

	shellCmd := shellJoin(cmd)
	if dir := translateDir(opts.Dir, h.Meta["hostWorkspace"]); dir != "" {
		shellCmd = fmt.Sprintf("cd %s && %s", shellQuote(dir), shellCmd)
	}
	args = append(args, "sh", "-lc", shellCmd)

	// Env was written to envFile; don't also set it on the docker process
	// (docker exec won't forward host env to the container anyway).
	proc, err := c.runner.Start(ctx, c.docker, args, ExecOpts{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
	if err != nil {
		if envFile != "" {
			_ = os.Remove(envFile)
		}
		return nil, err
	}
	if envFile != "" {
		return &envFileProcess{Process: proc, path: envFile}, nil
	}
	return proc, nil
}

// writeEnvFile materializes env ("KEY=VALUE" entries) into a 0600 tempfile
// suitable for `docker exec --env-file`. Callers must remove the returned
// path once the referring process has exited.
//
// Docker's --env-file does not support multiline values. Any newlines in a
// value are encoded as the two-character sequence \n so the file stays valid.
// Readers (e.g. hermes_bridge) must decode \n back to real newlines.
func writeEnvFile(env []string) (string, error) {
	tmp, err := os.CreateTemp("", "belayer-env-*.env")
	if err != nil {
		return "", fmt.Errorf("sandbox/clamshell: temp env: %w", err)
	}
	// CreateTemp already uses 0600 on Unix, but be explicit so hardened umask
	// environments (or future filesystems) don't widen it.
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sandbox/clamshell: chmod env: %w", err)
	}
	var lines []string
	for _, entry := range env {
		idx := strings.IndexByte(entry, '=')
		if idx < 0 {
			lines = append(lines, entry)
			continue
		}
		key := entry[:idx]
		val := strings.ReplaceAll(entry[idx+1:], "\n", `\n`)
		lines = append(lines, key+"="+val)
	}
	body := strings.Join(lines, "\n")
	if len(lines) > 0 {
		body += "\n"
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sandbox/clamshell: write env: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sandbox/clamshell: close env: %w", err)
	}
	return tmp.Name(), nil
}

// envFileProcess wraps a Process to unlink the temporary env file once the
// underlying process has exited (via Wait) or been killed. Cleanup runs at
// most once.
type envFileProcess struct {
	Process
	path    string
	cleaned bool
}

func (p *envFileProcess) cleanup() {
	if p.cleaned {
		return
	}
	p.cleaned = true
	_ = os.Remove(p.path)
}

func (p *envFileProcess) Wait() error {
	err := p.Process.Wait()
	p.cleanup()
	return err
}

func (p *envFileProcess) Kill() error {
	err := p.Process.Kill()
	// Kill doesn't block on exit; cleanup happens in Wait. But if the caller
	// Kills without ever Waiting, they still leak — that's on them.
	return err
}

// Stop tears down the clamshell sandbox and removes any temp policy file
// written during Create.
func (c *Clamshell) Stop(ctx context.Context, h Handle) error {
	if policyFile := h.Meta["policyFile"]; policyFile != "" {
		_ = os.Remove(policyFile)
	}
	if _, stderr, err := c.runner.Run(ctx, c.cli, "sandbox", "stop", h.ID); err != nil {
		return fmt.Errorf("sandbox/clamshell: stop: %w (stderr: %s)", err, stderr)
	}
	return nil
}

// preparePolicy reads basePath (if set), appends endpoints to its
// tcp_endpoints section, and writes the merged YAML to a temp file. Returns
// the temp path. Callers are responsible for removing the file when they no
// longer need it (Stop handles this via handle.Meta["policyFile"]).
func (c *Clamshell) preparePolicy(basePath string, endpoints []TCPEndpoint) (string, error) {
	doc := map[string]any{}
	if basePath != "" {
		raw, err := os.ReadFile(basePath)
		if err != nil {
			return "", fmt.Errorf("sandbox/clamshell: read policy %s: %w", basePath, err)
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return "", fmt.Errorf("sandbox/clamshell: parse policy %s: %w", basePath, err)
		}
	}
	if len(endpoints) > 0 {
		var existing []any
		if raw, present := doc["tcp_endpoints"]; present && raw != nil {
			list, ok := raw.([]any)
			if !ok {
				return "", fmt.Errorf("sandbox/clamshell: policy %s has non-list tcp_endpoints (%T)", basePath, raw)
			}
			existing = list
		}
		for _, ep := range endpoints {
			existing = append(existing, map[string]any{
				"name": ep.Name,
				"host": ep.Host,
				"port": ep.Port,
			})
		}
		doc["tcp_endpoints"] = existing
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("sandbox/clamshell: marshal policy: %w", err)
	}
	tmp, err := os.CreateTemp("", "belayer-policy-*.yaml")
	if err != nil {
		return "", fmt.Errorf("sandbox/clamshell: temp policy: %w", err)
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sandbox/clamshell: write temp policy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("sandbox/clamshell: close temp policy: %w", err)
	}
	return tmp.Name(), nil
}

// shellJoin renders argv as a single sh-safe string. We single-quote every
// element so spaces, quotes, and shell metacharacters in agent argv don't
// break out of the `sh -lc` wrapper.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps s in single quotes, escaping any embedded single quote by
// closing the quoted region, inserting a literal quote, and reopening.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
