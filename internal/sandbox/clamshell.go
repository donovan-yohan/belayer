//go:build clamshell

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

	// 4. Discover the container name so Exec can docker-exec into it. The
	//    --json form emits a structured response clamshell guarantees stable.
	connectOut, stderr, err := c.runner.Run(ctx, c.cli, "--json", "sandbox", "connect", cfg.Name)
	if err != nil {
		_ = os.Remove(policyPath)
		return Handle{}, fmt.Errorf("sandbox/clamshell: connect: %w (stderr: %s)", err, stderr)
	}
	var connect struct {
		Container string `json:"container"`
	}
	if err := json.Unmarshal(connectOut, &connect); err != nil {
		_ = os.Remove(policyPath)
		return Handle{}, fmt.Errorf("sandbox/clamshell: parse connect output: %w (stdout: %s)", err, connectOut)
	}
	if connect.Container == "" {
		_ = os.Remove(policyPath)
		return Handle{}, fmt.Errorf("sandbox/clamshell: connect output missing container field: %s", connectOut)
	}

	return Handle{
		ID: cfg.Name,
		Meta: map[string]string{
			"container":  connect.Container,
			"policyFile": policyPath,
		},
	}, nil
}

// Exec launches cmd inside the sandbox container via `docker exec`.
// Environment variables are materialized through `env KEY=VAL ...` so we don't
// depend on docker CLI's -e semantics, and the command runs under
// `sh -lc` so shell conveniences (pipes, globs, login profile) work as they
// would in a normal shell session.
func (c *Clamshell) Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (Process, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("sandbox/clamshell: exec requires at least one argument")
	}
	container, ok := h.Meta["container"]
	if !ok || container == "" {
		return nil, fmt.Errorf("sandbox/clamshell: exec handle %q missing container metadata", h.ID)
	}

	args := []string{"exec", "-u", "sandbox", "-i", container}
	if len(opts.Env) > 0 {
		args = append(args, "env")
		args = append(args, opts.Env...)
	}
	shellCmd := shellJoin(cmd)
	if opts.Dir != "" {
		shellCmd = fmt.Sprintf("cd %s && %s", shellQuote(opts.Dir), shellCmd)
	}
	args = append(args, "sh", "-lc", shellCmd)

	// Env was folded into the argv via `env`; don't also set it on the
	// docker process (docker exec won't forward host env to the container).
	return c.runner.Start(ctx, c.docker, args, ExecOpts{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
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
		existing, _ := doc["tcp_endpoints"].([]any)
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
