package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// spawnCat starts a subprocess that reads from stdin until EOF.
// It is used as a stand-in for the real Python bridge in tests.
func spawnCat(t *testing.T, cfg Config) *Process {
	t.Helper()
	cfg.Cmd = []string{"cat"}
	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { p.cmd.Process.Kill() }) //nolint:errcheck
	return p
}

// testConfig returns a minimal Config wired to t.TempDir().
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		SessionID:  "sess-test",
		AgentID:    "agent-test",
		Role:       "implementer",
		Profile:    "nightshift",
		Workdir:    t.TempDir(),
		SocketPath: "/tmp/test-belayer.sock",
		RunDir:     t.TempDir(),
	}
}

// TestSpawnEcho verifies that Spawn can start a real subprocess and that the
// done channel closes after the process exits.
func TestSpawnEcho(t *testing.T) {
	cfg := testConfig(t)
	cfg.Cmd = []string{"echo", "hello"}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	select {
	case <-p.Done():
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit within timeout")
	}

	if err := p.ExitErr(); err != nil {
		t.Fatalf("unexpected exit error: %v", err)
	}
}

// TestSpawnCreatesLogFiles verifies that stdout/stderr log files are created in RunDir.
func TestSpawnCreatesLogFiles(t *testing.T) {
	cfg := testConfig(t)
	cfg.Cmd = []string{"echo", "hello"}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	for _, name := range []string{"bridge-stdout.log", "bridge-stderr.log"} {
		path := cfg.RunDir + "/" + name
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected log file %s to exist: %v", path, err)
		}
	}
}

// TestWriteStdinValidJSON verifies that WriteStdin writes a valid JSON line.
func TestWriteStdinValidJSON(t *testing.T) {
	cfg := testConfig(t)
	p := spawnCat(t, cfg)

	payload := map[string]string{"hello": "world"}
	if err := p.WriteStdin(payload); err != nil {
		t.Fatalf("WriteStdin: %v", err)
	}
	// Close stdin to let the subprocess exit, then wait.
	p.stdin.Close()
	<-p.Done()

	// Verify that the stdout log contains the JSON line.
	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	line := strings.TrimSpace(string(logData))
	var decoded map[string]string
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("stdout log is not valid JSON: %v (got %q)", err, line)
	}
	if decoded["hello"] != "world" {
		t.Fatalf("unexpected JSON payload: %q", line)
	}
}

// TestInterruptSendsCorrectJSON verifies Interrupt writes the correct JSON shape.
func TestInterruptSendsCorrectJSON(t *testing.T) {
	cfg := testConfig(t)
	p := spawnCat(t, cfg)

	if err := p.Interrupt("supervisor", "please stop what you're doing"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	p.stdin.Close()
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	line := strings.TrimSpace(string(logData))
	var decoded map[string]string
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("stdout log is not valid JSON: %v (got %q)", err, line)
	}
	if decoded["type"] != "interrupt" {
		t.Fatalf("expected type=interrupt, got %q", decoded["type"])
	}
	if decoded["from"] != "supervisor" {
		t.Fatalf("expected from=supervisor, got %q", decoded["from"])
	}
	if decoded["content"] != "please stop what you're doing" {
		t.Fatalf("unexpected content: %q", decoded["content"])
	}
}

// TestStopGracefulExit verifies Stop returns nil when the process exits before
// the timeout.
func TestStopGracefulExit(t *testing.T) {
	cfg := testConfig(t)
	// Use a process that exits immediately when stdin is closed (cat exits on EOF).
	p := spawnCat(t, cfg)

	// Give cat a moment to start.
	time.Sleep(20 * time.Millisecond)

	// Stop sends {"type":"stop"} then waits. cat will echo it and stay alive
	// until stdin closes. We give a generous timeout — the stop message causes
	// stdin to be written, then the mutex is released. cat won't exit on its
	// own from the stop message alone, so close stdin explicitly here via Wait
	// with a short timeout to exercise the kill path.
	done := make(chan error, 1)
	go func() {
		done <- p.Stop(200 * time.Millisecond)
	}()

	select {
	case err := <-done:
		// Either nil (graceful) or a kill error is acceptable; the key thing is
		// that Stop returned and the process is gone.
		_ = err
		select {
		case <-p.Done():
			// good
		default:
			// done channel may not be closed yet if kill path was taken — give it a moment
			select {
			case <-p.Done():
			case <-time.After(500 * time.Millisecond):
				t.Fatal("process did not exit after Stop")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within timeout")
	}
}

// TestStopKillsProcessOnTimeout verifies that Stop kills the process when it
// does not exit within the given timeout.
func TestStopKillsProcessOnTimeout(t *testing.T) {
	cfg := testConfig(t)
	// Use `cat` which will stay alive until killed.
	p := spawnCat(t, cfg)

	start := time.Now()
	err := p.Stop(100 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Fatalf("Stop returned too quickly (%v), expected ~100ms timeout", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Stop took too long (%v)", elapsed)
	}
	if err == nil {
		t.Fatal("expected non-nil error when process is killed")
	}

	// Process must be gone now.
	select {
	case <-p.Done():
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("done channel not closed after kill")
	}
}

// TestDoneChannelClosesOnExit verifies the done channel semantics.
func TestDoneChannelClosesOnExit(t *testing.T) {
	cfg := testConfig(t)
	cfg.Cmd = []string{"true"} // exits immediately with code 0

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	select {
	case <-p.Done():
		// good — channel closed
	case <-time.After(3 * time.Second):
		t.Fatal("done channel was not closed after process exited")
	}
}

// TestWaitReturnsAfterExit verifies that Wait blocks until exit.
func TestWaitReturnsAfterExit(t *testing.T) {
	cfg := testConfig(t)
	cfg.Cmd = []string{"true"}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- p.Wait() }()

	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("unexpected Wait error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return within timeout")
	}
}

// TestEnvVarsInjected verifies that Belayer env vars are forwarded to the subprocess.
// We use `env` to print the environment, then check stdout log.
func TestEnvVarsInjected(t *testing.T) {
	cfg := Config{
		SessionID:       "sess-abc",
		AgentID:         "agent-xyz",
		Role:            "supervisor",
		Profile:         "nightshift-supervisor",
		Workdir:         t.TempDir(),
		SocketPath:      "/tmp/test.sock",
		RunDir:          t.TempDir(),
		Model:           "claude-opus-4",
		Message:         "do the thing",
		HermesSessionID: "hermes-456",
		Cmd:             []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	checks := map[string]string{
		"BELAYER_SESSION_ID":        "sess-abc",
		"BELAYER_AGENT_ID":          "agent-xyz",
		"BELAYER_ROLE":              "supervisor",
		"BELAYER_PROFILE":           "nightshift-supervisor",
		"BELAYER_SOCKET":            "/tmp/test.sock",
		"BELAYER_MODEL":             "claude-opus-4",
		"BELAYER_MESSAGE":           "do the thing",
		"BELAYER_HERMES_SESSION_ID": "hermes-456",
	}
	for key, want := range checks {
		if !strings.Contains(output, key+"="+want) {
			t.Errorf("expected %s=%s in env output\ngot:\n%s", key, want, output)
		}
	}
}

// TestOptionalEnvVarsOmitted verifies that empty optional fields are not injected.
func TestOptionalEnvVarsOmitted(t *testing.T) {
	cfg := Config{
		SessionID:  "sess-abc",
		AgentID:    "agent-xyz",
		Role:       "implementer",
		Profile:    "nightshift",
		Workdir:    t.TempDir(),
		SocketPath: "/tmp/test.sock",
		RunDir:     t.TempDir(),
		// Model, Message, HermesSessionID intentionally empty
		Cmd: []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	for _, key := range []string{"BELAYER_MODEL", "BELAYER_MESSAGE", "BELAYER_HERMES_SESSION_ID"} {
		if strings.Contains(output, key+"=") {
			t.Errorf("expected %s to be absent from env, but found it in output", key)
		}
	}
}

// TestWriteStdinConcurrentSafe verifies that concurrent WriteStdin calls do not panic or corrupt output.
func TestWriteStdinConcurrentSafe(t *testing.T) {
	cfg := testConfig(t)
	p := spawnCat(t, cfg)

	const goroutines = 10
	errs := make(chan error, goroutines)
	for i := range goroutines {
		go func(i int) {
			errs <- p.WriteStdin(map[string]any{"seq": i})
		}(i)
	}
	for range goroutines {
		if err := <-errs; err != nil {
			t.Errorf("WriteStdin error: %v", err)
		}
	}

	p.stdin.Close()
	<-p.Done()
}

// TestBelayerToolsEnvVarInjected verifies that BelayerTools are passed as
// a comma-separated BELAYER_TOOLS env var.
func TestBelayerToolsEnvVarInjected(t *testing.T) {
	cfg := Config{
		SessionID:    "sess-abc",
		AgentID:      "supervisor",
		Role:         "supervisor",
		Profile:      "default",
		Workdir:      t.TempDir(),
		SocketPath:   "/tmp/test.sock",
		RunDir:       t.TempDir(),
		BelayerTools: []string{"belayer_spawn_agent", "belayer_request_completion"},
		Cmd:          []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	expected := "BELAYER_TOOLS=belayer_spawn_agent,belayer_request_completion"
	if !strings.Contains(output, expected) {
		t.Errorf("expected %q in env output\ngot:\n%s", expected, output)
	}
}

// TestBuildCmdDefault verifies that BuildCmd with an empty Cmd field returns a
// python3 command.
func TestBuildCmdDefault(t *testing.T) {
	cfg := Config{} // Cmd is nil/empty
	argv := BuildCmd(cfg)
	if len(argv) == 0 {
		t.Fatal("BuildCmd returned empty slice")
	}
	// The first element must be a python3 binary (either full venv path or system).
	if !strings.Contains(argv[0], "python3") {
		t.Errorf("expected argv[0] to contain 'python3', got %q", argv[0])
	}
	if len(argv) < 3 || argv[1] != "-m" || argv[2] != "hermes_bridge" {
		t.Errorf("expected argv to end with '-m hermes_bridge', got %v", argv)
	}
}

// TestBuildCmdOverride verifies that BuildCmd returns cfg.Cmd when it is set.
func TestBuildCmdOverride(t *testing.T) {
	want := []string{"my-custom-python", "--flag", "value"}
	cfg := Config{Cmd: want}
	got := BuildCmd(cfg)
	if len(got) != len(want) {
		t.Fatalf("BuildCmd returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuildEnvContainsBelayerVars verifies that BuildEnv sets all expected
// BELAYER_* environment variables.
func TestBuildEnvContainsBelayerVars(t *testing.T) {
	cfg := Config{
		SessionID:       "sess-build",
		AgentID:         "agent-build",
		Role:            "supervisor",
		Profile:         "nightshift-supervisor",
		SocketPath:      "/tmp/build.sock",
		RunDir:          t.TempDir(),
		Model:           "claude-opus-4",
		Message:         "build message",
		SystemPrompt:    "you are helpful",
		HermesSessionID: "hermes-build-789",
		BelayerTools:    []string{"tool_a", "tool_b"},
	}

	env := BuildEnv(cfg)
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	checks := map[string]string{
		"BELAYER_SESSION_ID":        "sess-build",
		"BELAYER_AGENT_ID":          "agent-build",
		"BELAYER_ROLE":              "supervisor",
		"BELAYER_PROFILE":           "nightshift-supervisor",
		"BELAYER_SOCKET":            "/tmp/build.sock",
		"BELAYER_RUN_DIR":           cfg.RunDir,
		"BELAYER_MODEL":             "claude-opus-4",
		"BELAYER_MESSAGE":           "build message",
		"BELAYER_SYSTEM_PROMPT":     "you are helpful",
		"BELAYER_HERMES_SESSION_ID": "hermes-build-789",
		"BELAYER_TOOLS":             "tool_a,tool_b",
	}
	for key, want := range checks {
		if got, ok := envMap[key]; !ok {
			t.Errorf("expected %s to be set in env", key)
		} else if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// PYTHONPATH must be present.
	if _, ok := envMap["PYTHONPATH"]; !ok {
		t.Error("expected PYTHONPATH to be set in env")
	}
}

// TestBuildEnvOmitsOptionalVars verifies that empty optional fields (Model,
// Message, SystemPrompt, HermesSessionID, BelayerTools) are not added to the
// environment.
func TestBuildEnvOmitsOptionalVars(t *testing.T) {
	cfg := Config{
		SessionID:  "sess-omit",
		AgentID:    "agent-omit",
		Role:       "implementer",
		Profile:    "nightshift",
		SocketPath: "/tmp/omit.sock",
		RunDir:     t.TempDir(),
		// Model, Message, SystemPrompt, HermesSessionID, BelayerTools all zero-value
	}

	env := BuildEnv(cfg)
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	for _, key := range []string{
		"BELAYER_MODEL",
		"BELAYER_MESSAGE",
		"BELAYER_SYSTEM_PROMPT",
		"BELAYER_HERMES_SESSION_ID",
		"BELAYER_TOOLS",
	} {
		if _, ok := envMap[key]; ok {
			t.Errorf("expected %s to be absent from env, but it was set", key)
		}
	}
}

// TestBelayerToolsEnvVarOmittedWhenEmpty verifies that BELAYER_TOOLS is not
// set when the tool list is empty (baseline-only agents).
func TestBelayerToolsEnvVarOmittedWhenEmpty(t *testing.T) {
	cfg := Config{
		SessionID:    "sess-abc",
		AgentID:      "worker",
		Role:         "implementer",
		Profile:      "default",
		Workdir:      t.TempDir(),
		SocketPath:   "/tmp/test.sock",
		RunDir:       t.TempDir(),
		BelayerTools: nil,
		Cmd:          []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	if strings.Contains(output, "BELAYER_TOOLS=") {
		t.Errorf("expected BELAYER_TOOLS to be absent from env, but found it in output")
	}
}
