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
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("hi")

	_, err := c.Exec(context.Background(),
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

	// Expected argv prefix: exec -u sandbox -i sbx-abc env ANTHROPIC_API_KEY=secret PYTHONPATH=/belayer sh -lc <cmd>
	want := []string{
		"exec", "-u", "sandbox", "-i", "sbx-abc",
		"env", "ANTHROPIC_API_KEY=secret", "PYTHONPATH=/belayer",
		"sh", "-lc",
	}
	if len(call.args) != len(want)+1 {
		t.Fatalf("argv length = %d, want %d: %v", len(call.args), len(want)+1, call.args)
	}
	for i, w := range want {
		if call.args[i] != w {
			t.Errorf("argv[%d] = %q, want %q (full: %v)", i, call.args[i], w, call.args)
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
	// Env is folded into argv via `env`; don't also set it on the outer process.
	if call.opts.Env != nil {
		t.Errorf("Exec set opts.Env on docker process = %v, want nil", call.opts.Env)
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
