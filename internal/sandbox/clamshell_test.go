//go:build clamshell

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// fakeRunner records calls and returns scripted outputs. Tests queue scripted
// Run responses in order; Start is captured but never actually launches a
// process (tests that care assert on the recorded argv).
type fakeRunner struct {
	runs       []runCall
	starts     []startCall
	runScript  []runResponse
	startProc  Process
	startError error
}

type runCall struct {
	name string
	args []string
}

type startCall struct {
	name string
	args []string
	opts ExecOpts
}

type runResponse struct {
	stdout []byte
	stderr []byte
	err    error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.runs = append(f.runs, runCall{name: name, args: append([]string(nil), args...)})
	if len(f.runScript) == 0 {
		return nil, nil, nil
	}
	resp := f.runScript[0]
	f.runScript = f.runScript[1:]
	return resp.stdout, resp.stderr, resp.err
}

func (f *fakeRunner) Start(_ context.Context, name string, args []string, opts ExecOpts) (Process, error) {
	f.starts = append(f.starts, startCall{
		name: name,
		args: append([]string(nil), args...),
		opts: opts,
	})
	if f.startError != nil {
		return nil, f.startError
	}
	return f.startProc, nil
}

// newTestClamshell wires up a Clamshell with a fake runner and canned responses.
func newTestClamshell(runScript ...runResponse) (*Clamshell, *fakeRunner) {
	f := &fakeRunner{runScript: runScript}
	return &Clamshell{cli: "clamshell", docker: "docker", runner: f}, f
}

func TestClamshellCreateInvokesExpectedCLI(t *testing.T) {
	c, f := newTestClamshell(
		runResponse{}, // gateway start → OK
		runResponse{}, // sandbox create → OK
		runResponse{stdout: []byte(`{"container":"sbx-abc123"}`)}, // connect → container
	)

	handle, err := c.Create(context.Background(), Config{
		Name:      "sess-1",
		Workspace: "/home/user/workspace",
		Endpoints: []TCPEndpoint{
			{Name: "api", Host: "localhost", Port: 4000},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if p := handle.Meta["policyFile"]; p != "" {
			os.Remove(p)
		}
	})

	if len(f.runs) != 3 {
		t.Fatalf("expected 3 clamshell CLI calls, got %d: %+v", len(f.runs), f.runs)
	}

	// Call 0: gateway start
	if got := f.runs[0]; got.name != "clamshell" || !equalSlice(got.args, []string{"gateway", "start"}) {
		t.Errorf("gateway call = %v %v, want clamshell gateway start", got.name, got.args)
	}

	// Call 1: sandbox create
	create := f.runs[1]
	if create.name != "clamshell" {
		t.Errorf("create binary = %q, want clamshell", create.name)
	}
	if len(create.args) != 8 ||
		create.args[0] != "sandbox" || create.args[1] != "create" ||
		create.args[2] != "--name" || create.args[3] != "sess-1" ||
		create.args[4] != "--policy" ||
		create.args[6] != "--workspace" ||
		create.args[7] != "/home/user/workspace" {
		t.Errorf("sandbox create args = %v", create.args)
	}

	// Call 2: --json sandbox connect
	connect := f.runs[2]
	if !equalSlice(connect.args, []string{"--json", "sandbox", "connect", "sess-1"}) {
		t.Errorf("connect args = %v", connect.args)
	}

	if handle.Meta["container"] != "sbx-abc123" {
		t.Errorf("handle container = %q, want sbx-abc123", handle.Meta["container"])
	}
	if handle.Meta["policyFile"] == "" {
		t.Error("handle missing policyFile metadata")
	}
}

func TestClamshellCreateMergesEndpointsIntoPolicy(t *testing.T) {
	// Write a base policy with an existing endpoint.
	baseDir := t.TempDir()
	basePath := filepath.Join(baseDir, "policy.yaml")
	basePolicy := `
allow:
  - api.anthropic.com
tcp_endpoints:
  - {name: postgres, host: localhost, port: 5432}
`
	if err := os.WriteFile(basePath, []byte(basePolicy), 0o600); err != nil {
		t.Fatalf("write base policy: %v", err)
	}

	c, f := newTestClamshell(
		runResponse{},
		runResponse{},
		runResponse{stdout: []byte(`{"container":"sbx-xyz"}`)},
	)

	handle, err := c.Create(context.Background(), Config{
		Name:      "sess-merge",
		Workspace: "/tmp/ws",
		Policy:    basePath,
		Endpoints: []TCPEndpoint{
			{Name: "api", Host: "localhost", Port: 4000},
			{Name: "web", Host: "localhost", Port: 3000},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	policyPath := handle.Meta["policyFile"]
	t.Cleanup(func() { os.Remove(policyPath) })

	// The sandbox create call must point at the temp policy, not the original.
	createCall := f.runs[1]
	gotPolicyArg := ""
	for i, a := range createCall.args {
		if a == "--policy" && i+1 < len(createCall.args) {
			gotPolicyArg = createCall.args[i+1]
		}
	}
	if gotPolicyArg != policyPath {
		t.Errorf("sandbox create policy arg = %q, want %q", gotPolicyArg, policyPath)
	}

	// And that temp file must contain the merged endpoints.
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("read merged policy: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse merged policy: %v", err)
	}
	endpoints, _ := doc["tcp_endpoints"].([]any)
	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoints after merge, got %d: %+v", len(endpoints), endpoints)
	}
	// The base's postgres entry should still be there.
	names := map[string]bool{}
	for _, e := range endpoints {
		m, _ := e.(map[string]any)
		if n, ok := m["name"].(string); ok {
			names[n] = true
		}
	}
	for _, want := range []string{"postgres", "api", "web"} {
		if !names[want] {
			t.Errorf("merged policy missing endpoint %q; have %v", want, names)
		}
	}
}

func TestClamshellCreateGatewayFailureReturnsError(t *testing.T) {
	c, _ := newTestClamshell(
		runResponse{stderr: []byte("boom"), err: fmt.Errorf("exit 1")},
	)
	_, err := c.Create(context.Background(), Config{Name: "sess-fail"})
	if err == nil {
		t.Fatal("expected gateway failure error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway start") {
		t.Errorf("error %q does not mention gateway start", err.Error())
	}
}

func TestClamshellCreateMissingContainerFieldErrors(t *testing.T) {
	c, _ := newTestClamshell(
		runResponse{},
		runResponse{},
		runResponse{stdout: []byte(`{"container":""}`)},
	)
	_, err := c.Create(context.Background(), Config{Name: "sess-missing"})
	if err == nil {
		t.Fatal("expected missing-container error, got nil")
	}
	if !strings.Contains(err.Error(), "container field") {
		t.Errorf("error %q does not mention container field", err.Error())
	}
}

func TestClamshellExecBuildsDockerArgv(t *testing.T) {
	c, f := newTestClamshell()
	f.startProc = &fakeProcess{}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("hi")

	proc, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{"container": "sbx-abc"}},
		[]string{"python3", "-m", "hermes_bridge"},
		ExecOpts{
			Env:    []string{"ANTHROPIC_API_KEY=secret", "PYTHONPATH=/belayer"},
			Dir:    "/workspace/arielcharts",
			Stdin:  stdin,
			Stdout: &stdout,
			Stderr: &stderr,
		},
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(f.starts) != 1 {
		t.Fatalf("expected 1 docker start, got %d", len(f.starts))
	}
	call := f.starts[0]
	if call.name != "docker" {
		t.Errorf("runner.Start name = %q, want docker", call.name)
	}

	// Expected argv shape: exec -u sandbox -i --env-file <tmp> sbx-abc sh -lc <cmd>
	// The env values live inside <tmp> (mode 0600) rather than in the argv, so
	// `ps auxe` on the host can't read secrets out of the process table.
	if len(call.args) != 10 {
		t.Fatalf("argv length = %d, want 10: %v", len(call.args), call.args)
	}
	prefix := []string{"exec", "-u", "sandbox", "-i", "--env-file"}
	for i, w := range prefix {
		if call.args[i] != w {
			t.Errorf("argv[%d] = %q, want %q (full: %v)", i, call.args[i], w, call.args)
		}
	}
	envFilePath := call.args[5]
	if envFilePath == "" {
		t.Fatal("argv missing --env-file path")
	}
	if call.args[6] != "sbx-abc" {
		t.Errorf("argv[6] = %q, want container name sbx-abc", call.args[6])
	}
	if call.args[7] != "sh" || call.args[8] != "-lc" {
		t.Errorf("argv tail = %v, want sh -lc <cmd>", call.args[7:])
	}

	// env-file contents: exactly the provided KEY=VAL entries, one per line, and
	// mode must be 0600 so other users on the host can't read secrets.
	info, statErr := os.Stat(envFilePath)
	if statErr != nil {
		t.Fatalf("stat env file: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("env-file perm = %o, want 0600", perm)
	}
	envBody, readErr := os.ReadFile(envFilePath)
	if readErr != nil {
		t.Fatalf("read env file: %v", readErr)
	}
	wantBody := "ANTHROPIC_API_KEY=secret\nPYTHONPATH=/belayer\n"
	if string(envBody) != wantBody {
		t.Errorf("env-file body = %q, want %q", string(envBody), wantBody)
	}
	// Secrets must not appear in argv at all.
	for _, a := range call.args {
		if strings.Contains(a, "ANTHROPIC_API_KEY") || strings.Contains(a, "secret") {
			t.Errorf("secret leaked into argv: %q (full: %v)", a, call.args)
		}
	}

	// The final arg should be the shell-joined command with a cd prefix.
	shellCmd := call.args[len(call.args)-1]
	if !strings.Contains(shellCmd, "cd '/workspace/arielcharts'") {
		t.Errorf("shell cmd does not cd into Dir: %q", shellCmd)
	}
	if !strings.Contains(shellCmd, "'python3' '-m' 'hermes_bridge'") {
		t.Errorf("shell cmd does not shell-quote argv: %q", shellCmd)
	}

	// Stdio must be passed through — the daemon depends on stdin for interrupts
	// and stdout for log capture.
	if call.opts.Stdin != stdin {
		t.Error("Exec did not forward Stdin to docker process")
	}
	if call.opts.Stdout != &stdout {
		t.Error("Exec did not forward Stdout to docker process")
	}
	if call.opts.Stderr != &stderr {
		t.Error("Exec did not forward Stderr to docker process")
	}
	// Env lives in the env-file; don't also set it on the outer process.
	if call.opts.Env != nil {
		t.Errorf("Exec set opts.Env on docker process = %v, want nil", call.opts.Env)
	}

	// Waiting on the returned Process must unlink the env-file — otherwise we
	// accumulate /tmp/belayer-env-*.env files across every agent spawn.
	if err := proc.Wait(); err != nil {
		t.Errorf("proc.Wait() = %v, want nil", err)
	}
	if _, err := os.Stat(envFilePath); !os.IsNotExist(err) {
		t.Errorf("env-file %q still exists after Wait: err=%v", envFilePath, err)
		os.Remove(envFilePath)
	}
}

func TestClamshellExecCleansEnvFileOnStartError(t *testing.T) {
	// If runner.Start fails after we've written the env-file, Exec must still
	// remove it — the caller won't receive a Process to Wait on.
	c, f := newTestClamshell()
	f.startError = fmt.Errorf("boom")

	// Capture the path by intercepting the recorded start args. We can't know
	// the path in advance, but we can inspect /tmp after the failure and make
	// sure the specific tempfile referenced in argv is gone.
	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{"container": "sbx"}},
		[]string{"echo"},
		ExecOpts{Env: []string{"K=V"}},
	)
	if err == nil {
		t.Fatal("expected start error, got nil")
	}
	if len(f.starts) != 1 {
		t.Fatalf("expected 1 start attempt, got %d", len(f.starts))
	}
	envFilePath := f.starts[0].args[5]
	if envFilePath == "" {
		t.Fatal("argv missing --env-file path")
	}
	if _, statErr := os.Stat(envFilePath); !os.IsNotExist(statErr) {
		t.Errorf("env-file %q still exists after Start error: err=%v", envFilePath, statErr)
		os.Remove(envFilePath)
	}
}

func TestClamshellExecNoEnvSkipsEnvFile(t *testing.T) {
	// With no env vars there's nothing to hide, so Exec should skip writing an
	// env-file and keep the argv shorter. Verifies we don't leak empty tempfiles.
	c, f := newTestClamshell()
	f.startProc = &fakeProcess{}

	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{"container": "sbx"}},
		[]string{"echo", "hi"},
		ExecOpts{},
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	call := f.starts[0]
	for _, a := range call.args {
		if a == "--env-file" {
			t.Errorf("argv contains --env-file despite empty Env: %v", call.args)
		}
	}
}

func TestClamshellExecTranslatesHostWorkdirToContainerPath(t *testing.T) {
	// The daemon passes ExecOpts.Dir as a host-side path (e.g. the absolute
	// workspace path). Inside the clamshell container the workspace is
	// mounted at /workspace, so host paths produce a broken `cd`. Exec must
	// rewrite paths that live under the handle's hostWorkspace.
	c, f := newTestClamshell()
	f.startProc = &fakeProcess{}

	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{
			"container":     "sbx",
			"hostWorkspace": "/home/user/workspace",
		}},
		[]string{"echo", "hi"},
		ExecOpts{Dir: "/home/user/workspace/repo/src"},
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	shellCmd := f.starts[0].args[len(f.starts[0].args)-1]
	if !strings.Contains(shellCmd, "cd '/workspace/repo/src'") {
		t.Errorf("shell cmd did not translate host workdir: %q", shellCmd)
	}
	if strings.Contains(shellCmd, "/home/user/workspace") {
		t.Errorf("host workspace path leaked into container: %q", shellCmd)
	}
}

func TestClamshellExecWorkdirAtWorkspaceRoot(t *testing.T) {
	c, f := newTestClamshell()
	f.startProc = &fakeProcess{}

	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{
			"container":     "sbx",
			"hostWorkspace": "/home/user/workspace",
		}},
		[]string{"echo", "hi"},
		ExecOpts{Dir: "/home/user/workspace"},
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	shellCmd := f.starts[0].args[len(f.starts[0].args)-1]
	if !strings.Contains(shellCmd, "cd '/workspace'") {
		t.Errorf("workspace root did not translate to /workspace: %q", shellCmd)
	}
}

func TestClamshellExecContainerPathPassesThrough(t *testing.T) {
	// Callers that already compute container-side paths (or the daemon once
	// it gains mount-aware planning) must still work. Paths outside the host
	// workspace are preserved verbatim.
	c, f := newTestClamshell()
	f.startProc = &fakeProcess{}

	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{
			"container":     "sbx",
			"hostWorkspace": "/home/user/workspace",
		}},
		[]string{"echo", "hi"},
		ExecOpts{Dir: "/workspace/nested"},
	)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	shellCmd := f.starts[0].args[len(f.starts[0].args)-1]
	if !strings.Contains(shellCmd, "cd '/workspace/nested'") {
		t.Errorf("container path not preserved: %q", shellCmd)
	}
}

func TestClamshellExecEmptyCmdErrors(t *testing.T) {
	c, _ := newTestClamshell()
	_, err := c.Exec(context.Background(),
		Handle{ID: "x", Meta: map[string]string{"container": "c"}},
		[]string{},
		ExecOpts{},
	)
	if err == nil {
		t.Fatal("expected error for empty cmd, got nil")
	}
}

func TestClamshellExecMissingContainerErrors(t *testing.T) {
	c, _ := newTestClamshell()
	_, err := c.Exec(context.Background(),
		Handle{ID: "sess-1", Meta: map[string]string{}},
		[]string{"echo", "hi"},
		ExecOpts{},
	)
	if err == nil {
		t.Fatal("expected error for missing container metadata, got nil")
	}
	if !strings.Contains(err.Error(), "container") {
		t.Errorf("error %q does not mention container", err.Error())
	}
}

func TestClamshellStopCallsCLIAndRemovesPolicyFile(t *testing.T) {
	// Create a real temp file so we can verify Stop removes it.
	tmp, err := os.CreateTemp("", "belayer-test-policy-*.yaml")
	if err != nil {
		t.Fatalf("create temp policy: %v", err)
	}
	tmp.Close()
	policyPath := tmp.Name()

	c, f := newTestClamshell(runResponse{}) // sandbox stop → OK
	err = c.Stop(context.Background(), Handle{
		ID:   "sess-1",
		Meta: map[string]string{"policyFile": policyPath},
	})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if len(f.runs) != 1 {
		t.Fatalf("expected 1 CLI call, got %d", len(f.runs))
	}
	if !equalSlice(f.runs[0].args, []string{"sandbox", "stop", "sess-1"}) {
		t.Errorf("stop args = %v", f.runs[0].args)
	}
	if _, err := os.Stat(policyPath); !os.IsNotExist(err) {
		t.Errorf("policy file still exists after Stop: err=%v", err)
		os.Remove(policyPath)
	}
}

func TestClamshellStopPropagatesCLIError(t *testing.T) {
	c, _ := newTestClamshell(
		runResponse{stderr: []byte("not found"), err: fmt.Errorf("exit 2")},
	)
	err := c.Stop(context.Background(), Handle{ID: "sess-x"})
	if err == nil {
		t.Fatal("expected Stop error, got nil")
	}
}

// fakeProcess is a Process whose Wait/Kill return immediately. Tests that
// exercise cleanup hooks (e.g. envFileProcess) use it so they can call Wait
// without spinning a real subprocess.
type fakeProcess struct {
	waitCount int
	killCount int
}

func (p *fakeProcess) Pid() int     { return 0 }
func (p *fakeProcess) Wait() error  { p.waitCount++; return nil }
func (p *fakeProcess) Kill() error  { p.killCount++; return nil }

func TestPreparePolicyRejectsNonListTCPEndpoints(t *testing.T) {
	// If a base policy already has a scalar under tcp_endpoints, silently
	// overwriting it would drop user config on the floor. Exec must refuse
	// to proceed so the user sees the mistake instead of a confusing missing
	// allowlist at runtime.
	baseDir := t.TempDir()
	basePath := filepath.Join(baseDir, "bad-policy.yaml")
	badPolicy := `tcp_endpoints: "not-a-list"
`
	if err := os.WriteFile(basePath, []byte(badPolicy), 0o600); err != nil {
		t.Fatalf("write base policy: %v", err)
	}

	c, _ := newTestClamshell(runResponse{}) // gateway start
	_, err := c.Create(context.Background(), Config{
		Name:      "sess-bad",
		Workspace: "/tmp/ws",
		Policy:    basePath,
		Endpoints: []TCPEndpoint{{Name: "api", Host: "localhost", Port: 4000}},
	})
	if err == nil {
		t.Fatal("expected error for non-list tcp_endpoints, got nil")
	}
	if !strings.Contains(err.Error(), "non-list tcp_endpoints") {
		t.Errorf("error %q does not mention non-list tcp_endpoints", err.Error())
	}
}

func TestClamshellCreateStopsSandboxOnPostCreateFailure(t *testing.T) {
	// If `clamshell sandbox create` succeeds but `sandbox connect` fails,
	// the sandbox is left running. Create must invoke `sandbox stop <name>`
	// before returning so we don't leak sandboxes on every discovery error.
	c, f := newTestClamshell(
		runResponse{},                                         // gateway start → OK
		runResponse{},                                         // sandbox create → OK
		runResponse{stderr: []byte("denied"), err: fmt.Errorf("exit 1")}, // connect → fail
		runResponse{},                                         // sandbox stop cleanup → OK
	)

	_, err := c.Create(context.Background(), Config{Name: "sess-leak", Workspace: "/tmp/ws"})
	if err == nil {
		t.Fatal("expected connect failure error, got nil")
	}

	if len(f.runs) != 4 {
		t.Fatalf("expected 4 CLI calls (gateway, create, connect, stop), got %d: %+v", len(f.runs), f.runs)
	}
	stopCall := f.runs[3]
	if !equalSlice(stopCall.args, []string{"sandbox", "stop", "sess-leak"}) {
		t.Errorf("cleanup call = %v, want sandbox stop sess-leak", stopCall.args)
	}
}

func TestClamshellCreateCleanupErrorIsWrapped(t *testing.T) {
	// If the cleanup stop itself fails, the original cause must still be the
	// primary error (the user cares why Create failed, not about the cleanup
	// attempt), but the cleanup failure should be visible.
	c, _ := newTestClamshell(
		runResponse{},
		runResponse{},
		runResponse{stdout: []byte(`{"container":""}`)}, // triggers missing-container path
		runResponse{err: fmt.Errorf("stop-failed")},
	)

	_, err := c.Create(context.Background(), Config{Name: "sess-both-fail", Workspace: "/tmp/ws"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "container field") {
		t.Errorf("error %q does not mention original cause", err.Error())
	}
	if !strings.Contains(err.Error(), "cleanup stop failed") {
		t.Errorf("error %q does not mention cleanup failure", err.Error())
	}
}

func TestClamshellCreateUpsertsProviders(t *testing.T) {
	// Providers configured on cfg.Providers must be registered with the
	// gateway (delete-then-create for idempotence) before sandbox create, and
	// each provider must be attached to the sandbox via --provider. Provider
	// names are namespaced with the session id to avoid collisions across
	// concurrent sessions against a shared clamshell gateway.
	t.Setenv("OPENCODE_GO_API_KEY", "sk-real-opencode")
	t.Setenv("ANTHROPIC_API_KEY", "sk-real-anthropic")

	c, f := newTestClamshell(
		runResponse{}, // gateway start
		runResponse{}, // provider delete sess-prov-opencode (best-effort)
		runResponse{}, // provider create sess-prov-opencode
		runResponse{}, // provider delete sess-prov-anthropic (best-effort)
		runResponse{}, // provider create sess-prov-anthropic
		runResponse{}, // sandbox create
		runResponse{stdout: []byte(`{"container":"sbx-abc"}`)}, // connect
	)

	handle, err := c.Create(context.Background(), Config{
		Name:      "sess-prov",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{
			{
				Name:      "opencode",
				Type:      "apikey",
				SecretEnv: "OPENCODE_GO_API_KEY",
				Project:   []string{"OPENCODE_GO_API_KEY"},
				Endpoints: []string{"opencode.ai"},
			},
			{
				Name:          "anthropic",
				Type:          "apikey",
				SecretEnv:     "ANTHROPIC_API_KEY",
				Project:       []string{"ANTHROPIC_API_KEY"},
				Endpoints:     []string{"api.anthropic.com"},
				AuthHeader:    "x-api-key",
				AuthScheme:    "",
				AuthSchemeSet: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if p := handle.Meta["policyFile"]; p != "" {
			os.Remove(p)
		}
	})

	if len(f.runs) != 7 {
		t.Fatalf("expected 7 clamshell calls, got %d: %+v", len(f.runs), f.runs)
	}

	// Call 0: gateway start (must come BEFORE provider calls because
	// provider mutations require a live gateway).
	if !equalSlice(f.runs[0].args, []string{"gateway", "start"}) {
		t.Errorf("runs[0] args = %v, want gateway start", f.runs[0].args)
	}
	// Call 1: provider delete sess-prov-opencode (namespaced idempotence)
	if !equalSlice(f.runs[1].args, []string{"provider", "delete", "--name", "sess-prov-opencode"}) {
		t.Errorf("runs[1] args = %v, want provider delete --name sess-prov-opencode", f.runs[1].args)
	}
	// Call 2: provider create sess-prov-opencode — Bearer defaults, no auth flags
	wantOpencode := []string{
		"provider", "create",
		"--type", "apikey",
		"--name", "sess-prov-opencode",
		"--from-existing", "OPENCODE_GO_API_KEY",
		"--project", "OPENCODE_GO_API_KEY",
		"--endpoints", "opencode.ai",
	}
	if !equalSlice(f.runs[2].args, wantOpencode) {
		t.Errorf("runs[2] args = %v, want %v", f.runs[2].args, wantOpencode)
	}
	// When AuthSchemeSet is false the --auth-scheme flag must NOT be forwarded —
	// we defer to clamshell's default rather than override with an empty string.
	for _, a := range f.runs[2].args {
		if a == "--auth-scheme" {
			t.Errorf("runs[2] forwards --auth-scheme despite AuthSchemeSet=false: %v", f.runs[2].args)
		}
	}
	// Call 3: provider delete sess-prov-anthropic
	if !equalSlice(f.runs[3].args, []string{"provider", "delete", "--name", "sess-prov-anthropic"}) {
		t.Errorf("runs[3] args = %v, want provider delete --name sess-prov-anthropic", f.runs[3].args)
	}
	// Call 4: provider create sess-prov-anthropic — explicit x-api-key with empty scheme
	wantAnthropic := []string{
		"provider", "create",
		"--type", "apikey",
		"--name", "sess-prov-anthropic",
		"--from-existing", "ANTHROPIC_API_KEY",
		"--project", "ANTHROPIC_API_KEY",
		"--endpoints", "api.anthropic.com",
		"--auth-header", "x-api-key",
		"--auth-scheme", "",
	}
	if !equalSlice(f.runs[4].args, wantAnthropic) {
		t.Errorf("runs[4] args = %v, want %v", f.runs[4].args, wantAnthropic)
	}

	// Call 5: sandbox create — must carry the namespaced --provider flags.
	createArgs := f.runs[5].args
	joined := strings.Join(createArgs, " ")
	if !strings.Contains(joined, "--provider apikey=sess-prov-opencode") {
		t.Errorf("sandbox create missing --provider apikey=sess-prov-opencode: %v", createArgs)
	}
	if !strings.Contains(joined, "--provider apikey=sess-prov-anthropic") {
		t.Errorf("sandbox create missing --provider apikey=sess-prov-anthropic: %v", createArgs)
	}
	// By construction the real secret value is never passed in argv — only
	// env var NAMES cross the clamshell CLI boundary, and clamshell reads the
	// value from its inherited env via --from-existing. This assertion catches
	// the regression where someone substitutes os.Getenv(p.SecretEnv).
	for _, call := range f.runs {
		for _, arg := range call.args {
			if strings.Contains(arg, "sk-real") {
				t.Errorf("secret value reached argv: %q (call: %v)", arg, call.args)
			}
		}
	}
}

func TestClamshellCreateNoProvidersSkipsUpsert(t *testing.T) {
	// With no providers configured, Create must not call `provider delete`
	// or `provider create` at all, and sandbox create must not carry any
	// --provider flags. This guards the default (provider-less) path.
	c, f := newTestClamshell(
		runResponse{},                                           // gateway start
		runResponse{},                                           // sandbox create
		runResponse{stdout: []byte(`{"container":"sbx-abc"}`)},  // connect
	)

	handle, err := c.Create(context.Background(), Config{
		Name:      "sess-none",
		Workspace: "/tmp/ws",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if p := handle.Meta["policyFile"]; p != "" {
			os.Remove(p)
		}
	})

	if len(f.runs) != 3 {
		t.Fatalf("expected 3 calls (no provider commands), got %d: %+v", len(f.runs), f.runs)
	}
	for _, call := range f.runs {
		for _, a := range call.args {
			if a == "--provider" {
				t.Errorf("sandbox create carries --provider flag with no providers: %v", call.args)
			}
		}
	}
}

func TestClamshellCreateMissingSecretEnvFailsBeforeAnySideEffect(t *testing.T) {
	// If the referenced host env var is empty, Create must fail before ANY
	// side-effect: no gateway start (so we don't spin up a gateway for a
	// config we're about to reject), no provider mutation.
	t.Setenv("OPENCODE_GO_API_KEY", "")

	c, f := newTestClamshell()
	_, err := c.Create(context.Background(), Config{
		Name:      "sess-missing",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{{
			Name:      "opencode",
			Type:      "apikey",
			SecretEnv: "OPENCODE_GO_API_KEY",
			Project:   []string{"OPENCODE_GO_API_KEY"},
			Endpoints: []string{"opencode.ai"},
		}},
	})
	if err == nil {
		t.Fatal("expected error for missing secret env, got nil")
	}
	if !strings.Contains(err.Error(), "OPENCODE_GO_API_KEY") {
		t.Errorf("error %q does not mention missing env var", err.Error())
	}
	if len(f.runs) != 0 {
		t.Errorf("expected zero clamshell calls, got %d: %+v", len(f.runs), f.runs)
	}
}

func TestClamshellCreateProviderCreateFailureStopsCreate(t *testing.T) {
	// A provider-create failure must surface the clamshell error and skip the
	// sandbox create call entirely — otherwise we'd create a sandbox that
	// references a nonexistent provider.
	t.Setenv("OPENCODE_GO_API_KEY", "sk-real")
	c, f := newTestClamshell(
		runResponse{}, // gateway start
		runResponse{}, // provider delete (best-effort)
		runResponse{stderr: []byte("duplicate field"), err: fmt.Errorf("exit 1")}, // provider create fails
	)
	_, err := c.Create(context.Background(), Config{
		Name:      "sess-pc-fail",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{{
			Name:      "opencode",
			Type:      "apikey",
			SecretEnv: "OPENCODE_GO_API_KEY",
			Project:   []string{"OPENCODE_GO_API_KEY"},
			Endpoints: []string{"opencode.ai"},
		}},
	})
	if err == nil {
		t.Fatal("expected provider create error, got nil")
	}
	if !strings.Contains(err.Error(), "provider \"opencode\" create") {
		t.Errorf("error %q does not identify the failing provider", err.Error())
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Errorf("error %q does not include clamshell stderr", err.Error())
	}
	// gateway + delete + create = 3 runs; sandbox create must not have been called.
	if len(f.runs) != 3 {
		t.Fatalf("expected 3 runs (no sandbox create), got %d: %+v", len(f.runs), f.runs)
	}
}

func TestClamshellCreateProviderValidateFailureRejectsEarly(t *testing.T) {
	// An invalid ProviderConfig (missing SecretEnv) must fail before touching
	// the clamshell CLI at all — no gateway start, no provider commands.
	c, f := newTestClamshell()
	_, err := c.Create(context.Background(), Config{
		Name:      "sess-bad",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{{
			Name:      "bad",
			Type:      "apikey",
			SecretEnv: "", // invalid
			Project:   []string{"K"},
			Endpoints: []string{"e.example"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "secret_env is required") {
		t.Errorf("error %q does not mention missing secret_env", err.Error())
	}
	if len(f.runs) != 0 {
		t.Errorf("expected zero clamshell calls, got %d: %+v", len(f.runs), f.runs)
	}
}

func TestClamshellCreateRollsBackProvidersOnSandboxCreateFailure(t *testing.T) {
	// If `sandbox create` fails AFTER providers are registered, we must roll
	// back each provider via `provider delete` — otherwise the gateway holds a
	// live reference to the real secret for a sandbox that will never exist.
	t.Setenv("OPENCODE_GO_API_KEY", "sk-real-op")
	t.Setenv("ANTHROPIC_API_KEY", "sk-real-an")

	c, f := newTestClamshell(
		runResponse{}, // gateway start
		runResponse{}, // provider delete sess-rb-opencode (best-effort)
		runResponse{}, // provider create sess-rb-opencode (succeeds)
		runResponse{}, // provider delete sess-rb-anthropic (best-effort)
		runResponse{}, // provider create sess-rb-anthropic (succeeds)
		runResponse{stderr: []byte("policy invalid"), err: fmt.Errorf("exit 1")}, // sandbox create fails
		runResponse{}, // rollback: provider delete sess-rb-opencode
		runResponse{}, // rollback: provider delete sess-rb-anthropic
	)

	_, err := c.Create(context.Background(), Config{
		Name:      "sess-rb",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{
			{Name: "opencode", Type: "apikey", SecretEnv: "OPENCODE_GO_API_KEY", Project: []string{"OPENCODE_GO_API_KEY"}, Endpoints: []string{"opencode.ai"}},
			{Name: "anthropic", Type: "apikey", SecretEnv: "ANTHROPIC_API_KEY", Project: []string{"ANTHROPIC_API_KEY"}, Endpoints: []string{"api.anthropic.com"}},
		},
	})
	if err == nil {
		t.Fatal("expected sandbox create error, got nil")
	}
	if len(f.runs) != 8 {
		t.Fatalf("expected 8 runs (5 setup + 1 failed create + 2 rollback), got %d: %+v", len(f.runs), f.runs)
	}
	// The last two calls must be rollback deletes, in creation order.
	if !equalSlice(f.runs[6].args, []string{"provider", "delete", "--name", "sess-rb-opencode"}) {
		t.Errorf("runs[6] rollback args = %v, want provider delete --name sess-rb-opencode", f.runs[6].args)
	}
	if !equalSlice(f.runs[7].args, []string{"provider", "delete", "--name", "sess-rb-anthropic"}) {
		t.Errorf("runs[7] rollback args = %v, want provider delete --name sess-rb-anthropic", f.runs[7].args)
	}
}

func TestClamshellCreateRollsBackOnPartialUpsertFailure(t *testing.T) {
	// Second provider's create fails. The first one was registered
	// successfully; Create must leave it behind for the caller's explicit
	// cleanup path rather than silently orphaning it. Because there's no
	// rollback in upsertProviders itself (the caller owns rollback once we
	// return a non-nil error), the test verifies we DON'T issue sandbox
	// create — the leak is bounded to the caller's visibility.
	t.Setenv("OPENCODE_GO_API_KEY", "sk-real-op")
	t.Setenv("ANTHROPIC_API_KEY", "sk-real-an")

	c, f := newTestClamshell(
		runResponse{}, // gateway start
		runResponse{}, // delete sess-p-opencode
		runResponse{}, // create sess-p-opencode succeeds
		runResponse{}, // delete sess-p-anthropic
		runResponse{stderr: []byte("bad"), err: fmt.Errorf("exit 1")}, // create sess-p-anthropic fails
	)

	_, err := c.Create(context.Background(), Config{
		Name:      "sess-p",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{
			{Name: "opencode", Type: "apikey", SecretEnv: "OPENCODE_GO_API_KEY", Project: []string{"OPENCODE_GO_API_KEY"}, Endpoints: []string{"opencode.ai"}},
			{Name: "anthropic", Type: "apikey", SecretEnv: "ANTHROPIC_API_KEY", Project: []string{"ANTHROPIC_API_KEY"}, Endpoints: []string{"api.anthropic.com"}},
		},
	})
	if err == nil {
		t.Fatal("expected upsert error, got nil")
	}
	// Exactly the 5 upsert-phase calls — no sandbox create, no automatic
	// rollback (Create's caller-side rollback kicks in after sandbox create
	// starts; mid-upsert failures surface cleanly to the caller).
	if len(f.runs) != 5 {
		t.Fatalf("expected 5 runs, got %d: %+v", len(f.runs), f.runs)
	}
	for _, call := range f.runs {
		if len(call.args) > 0 && call.args[0] == "sandbox" {
			t.Errorf("sandbox create called despite upsert failure: %v", call.args)
		}
	}
}

func TestClamshellCreateScrubsSecretFromProviderCreateStderr(t *testing.T) {
	// If clamshell's stderr ever echoes the secret value back (unlikely today
	// but cheap defense-in-depth), the error we forward into daemon logs must
	// redact it. The alternative is an operator grepping bridge logs for a
	// failure and finding the API key staring back at them.
	const secret = "sk-this-is-the-real-opencode-key"
	t.Setenv("OPENCODE_GO_API_KEY", secret)

	leakyStderr := []byte("provider create failed: rejected key value '" + secret + "'")
	c, _ := newTestClamshell(
		runResponse{}, // gateway start
		runResponse{}, // provider delete
		runResponse{stderr: leakyStderr, err: fmt.Errorf("exit 1")}, // create fails with leaky stderr
	)

	_, err := c.Create(context.Background(), Config{
		Name:      "sess-leak",
		Workspace: "/tmp/ws",
		Providers: []ProviderConfig{{
			Name: "opencode", Type: "apikey", SecretEnv: "OPENCODE_GO_API_KEY",
			Project: []string{"OPENCODE_GO_API_KEY"}, Endpoints: []string{"opencode.ai"},
		}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("secret value leaked into error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "<REDACTED>") {
		t.Errorf("expected <REDACTED> marker in error: %q", err.Error())
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestClamshellInitRegistersRealDriver proves the tagged build replaces the
// stub. With -tags clamshell set, Default.Get("clamshell") should return a
// *Clamshell (not the !clamshell stub) and Create should require real CLI
// availability rather than returning the "built without -tags clamshell"
// error. We don't actually invoke Create here — just assert the type.
func TestClamshellInitRegistersRealDriver(t *testing.T) {
	d, err := Default.Get("clamshell")
	if err != nil {
		t.Fatalf("Default.Get(\"clamshell\"): %v", err)
	}
	if _, ok := d.(*Clamshell); !ok {
		t.Errorf("Default.Get(\"clamshell\") = %T, want *Clamshell", d)
	}
}
