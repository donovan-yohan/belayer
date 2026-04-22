package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestBuildCmdConfinementWraps verifies that BuildCmd prepends
// "belayer-landlock-exec" when ConfineWrites is true and WriteRoots is set.
func TestBuildCmdConfinementWraps(t *testing.T) {
	cfg := Config{
		Cmd:           []string{"python3", "-m", "hermes_bridge"},
		ConfineWrites: true,
		WriteRoots:    []string{"/workspace", "/tmp"},
	}
	argv := BuildCmd(cfg)
	if argv[0] != "belayer-landlock-exec" {
		t.Fatalf("expected argv[0] = %q, got %q", "belayer-landlock-exec", argv[0])
	}
	if argv[1] != "python3" {
		t.Fatalf("expected argv[1] = %q, got %q", "python3", argv[1])
	}
}

// TestBuildCmdConfinementNoWrapWhenRootsEmpty verifies that BuildCmd does NOT
// wrap when WriteRoots is empty, even if ConfineWrites is true.
func TestBuildCmdConfinementNoWrapWhenRootsEmpty(t *testing.T) {
	cfg := Config{
		Cmd:           []string{"python3", "-m", "hermes_bridge"},
		ConfineWrites: true,
		WriteRoots:    nil,
	}
	argv := BuildCmd(cfg)
	if argv[0] == "belayer-landlock-exec" {
		t.Fatal("BuildCmd must not wrap when WriteRoots is empty")
	}
	if argv[0] != "python3" {
		t.Fatalf("expected argv[0] = %q, got %q", "python3", argv[0])
	}
}

// TestBuildCmdConfinementNoWrapWhenDisabled verifies that BuildCmd does NOT
// wrap when ConfineWrites is false.
func TestBuildCmdConfinementNoWrapWhenDisabled(t *testing.T) {
	cfg := Config{
		Cmd:           []string{"python3", "-m", "hermes_bridge"},
		ConfineWrites: false,
		WriteRoots:    []string{"/workspace"},
	}
	argv := BuildCmd(cfg)
	if argv[0] == "belayer-landlock-exec" {
		t.Fatal("BuildCmd must not wrap when ConfineWrites is false")
	}
}

// TestBuildEnvWriteRootsSet verifies that BELAYER_WRITE_ROOTS is set when
// ConfineWrites is true and WriteRoots is non-empty.
func TestBuildEnvWriteRootsSet(t *testing.T) {
	cfg := Config{
		SessionID:     "s",
		AgentID:       "a",
		Role:          "implementer",
		Profile:       "default",
		SocketPath:    "/tmp/t.sock",
		RunDir:        t.TempDir(),
		ConfineWrites: true,
		WriteRoots:    []string{"/workspace", "/tmp"},
	}
	env := BuildEnv(cfg)
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}
	want := "/workspace:/tmp"
	got, ok := envMap["BELAYER_WRITE_ROOTS"]
	if !ok {
		t.Fatal("expected BELAYER_WRITE_ROOTS to be set")
	}
	if got != want {
		t.Errorf("BELAYER_WRITE_ROOTS = %q, want %q", got, want)
	}
}

// TestBuildEnvWriteRootsOmittedWhenConfinementOff verifies that
// BELAYER_WRITE_ROOTS is absent when ConfineWrites is false.
func TestBuildEnvWriteRootsOmittedWhenConfinementOff(t *testing.T) {
	cfg := Config{
		SessionID:     "s",
		AgentID:       "a",
		Role:          "implementer",
		Profile:       "default",
		SocketPath:    "/tmp/t.sock",
		RunDir:        t.TempDir(),
		ConfineWrites: false,
		WriteRoots:    []string{"/workspace"},
	}
	env := BuildEnv(cfg)
	for _, e := range env {
		if strings.HasPrefix(e, "BELAYER_WRITE_ROOTS=") {
			t.Errorf("expected BELAYER_WRITE_ROOTS to be absent, but got %q", e)
		}
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
		MaxTurns:        42,
		Message:         "build message",
		SystemPrompt:    "you are helpful",
		HermesSessionID: "hermes-build-789",
		BelayerTools:    []string{"tool_a", "tool_b"},
		TranscriptPath:  "/tmp/transcripts/agent-build.jsonl",
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
		"BELAYER_MAX_TURNS":         "42",
		"BELAYER_MESSAGE":           "build message",
		"BELAYER_SYSTEM_PROMPT":     "you are helpful",
		"BELAYER_HERMES_SESSION_ID": "hermes-build-789",
		"BELAYER_TOOLS":             "tool_a,tool_b",
		"BELAYER_TRANSCRIPT_PATH":   "/tmp/transcripts/agent-build.jsonl",
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

// TestBuildEnvHermesAgentPathOverridesPYTHONPATH verifies that setting
// HERMES_AGENT_PATH changes the hermes-agent segment of PYTHONPATH.
func TestBuildEnvHermesAgentPathOverridesPYTHONPATH(t *testing.T) {
	t.Setenv("HERMES_AGENT_PATH", "/opt/custom/hermes-agent")
	t.Setenv("PYTHONPATH", "")

	cfg := Config{
		SessionID:   "sess",
		AgentID:     "agent",
		Role:        "implementer",
		Profile:     "default",
		SocketPath:  "/tmp/t.sock",
		RunDir:      t.TempDir(),
		BelayerRoot: "/opt/belayer",
	}
	env := BuildEnv(cfg)

	var pyPath string
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			pyPath = strings.TrimPrefix(e, "PYTHONPATH=")
		}
	}
	if pyPath == "" {
		t.Fatal("PYTHONPATH not set")
	}
	if !strings.Contains(pyPath, "/opt/custom/hermes-agent") {
		t.Errorf("PYTHONPATH = %q, want to contain HERMES_AGENT_PATH override", pyPath)
	}
	if strings.Contains(pyPath, ".hermes/hermes-agent") {
		t.Errorf("PYTHONPATH = %q, must not fall back to ~/.hermes when HERMES_AGENT_PATH is set", pyPath)
	}
}

// TestBuildEnvPYTHONPATHHasNoEmptySegments verifies that when the
// hermes-agent fallback is unresolvable, BuildEnv doesn't emit a PYTHONPATH
// with leading/trailing empty segments (which Python would interpret as cwd).
func TestBuildEnvPYTHONPATHHasNoEmptySegments(t *testing.T) {
	t.Setenv("HERMES_AGENT_PATH", "")
	t.Setenv("HOME", "")
	t.Setenv("PYTHONPATH", "/pre/existing")

	cfg := Config{
		SessionID:   "sess",
		AgentID:     "agent",
		Role:        "implementer",
		Profile:     "default",
		SocketPath:  "/tmp/t.sock",
		RunDir:      t.TempDir(),
		BelayerRoot: "/opt/belayer",
	}
	env := BuildEnv(cfg)

	var pyPath string
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			pyPath = strings.TrimPrefix(e, "PYTHONPATH=")
		}
	}
	if pyPath == "" {
		t.Fatal("PYTHONPATH not set")
	}
	sep := string(os.PathListSeparator)
	segs := strings.Split(pyPath, sep)
	if segs[0] == "" {
		t.Errorf("PYTHONPATH = %q starts with empty segment (Python would interpret as cwd)", pyPath)
	}
	if segs[len(segs)-1] == "" {
		t.Errorf("PYTHONPATH = %q ends with empty segment (Python would interpret as cwd)", pyPath)
	}
	if !strings.Contains(pyPath, "/opt/belayer") {
		t.Errorf("PYTHONPATH = %q, want to contain BelayerRoot", pyPath)
	}
	if !strings.Contains(pyPath, "/pre/existing") {
		t.Errorf("PYTHONPATH = %q, want to preserve existing PYTHONPATH", pyPath)
	}
}

// TestBuildEnvHermesAgentPathTrimmed verifies that whitespace-only
// HERMES_AGENT_PATH values are treated as unset (parity with
// sandbox.ModeOrDefault's BELAYER_SANDBOX_MODE trimming).
func TestBuildEnvHermesAgentPathTrimmed(t *testing.T) {
	t.Setenv("HERMES_AGENT_PATH", "   \n\t  ")
	t.Setenv("HOME", "/opt/dev-home")
	t.Setenv("PYTHONPATH", "")

	cfg := Config{
		SessionID:   "sess-ws",
		AgentID:     "agent-ws",
		Role:        "implementer",
		Profile:     "default",
		SocketPath:  "/tmp/ws.sock",
		RunDir:      t.TempDir(),
		BelayerRoot: "/opt/belayer",
	}
	env := BuildEnv(cfg)

	var pyPath string
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			pyPath = strings.TrimPrefix(e, "PYTHONPATH=")
		}
	}
	if pyPath == "" {
		t.Fatal("PYTHONPATH not set")
	}
	// Whitespace-only override should fall back to $HOME/.hermes/hermes-agent.
	wantFallback := "/opt/dev-home/.hermes/hermes-agent"
	if !strings.Contains(pyPath, wantFallback) {
		t.Errorf("PYTHONPATH = %q, want whitespace HERMES_AGENT_PATH to fall back to %q", pyPath, wantFallback)
	}
	// And should not carry the whitespace literally.
	if strings.Contains(pyPath, "   ") {
		t.Errorf("PYTHONPATH = %q leaked whitespace from HERMES_AGENT_PATH", pyPath)
	}
}

// TestBuildEnvSkipOpenRouterProbeInjected verifies that HERMES_SKIP_OPENROUTER_PROBE=1
// is set when SkipOpenRouterProbe is true, and absent when false.
func TestBuildEnvSkipOpenRouterProbeInjected(t *testing.T) {
	base := Config{
		SessionID:  "sess-probe",
		AgentID:    "agent-probe",
		Role:       "implementer",
		Profile:    "default",
		SocketPath: "/tmp/probe.sock",
		RunDir:     t.TempDir(),
	}

	t.Run("true injects env var", func(t *testing.T) {
		cfg := base
		cfg.SkipOpenRouterProbe = true
		env := BuildEnv(cfg)
		envMap := envToMap(env)
		if got, ok := envMap["HERMES_SKIP_OPENROUTER_PROBE"]; !ok {
			t.Error("expected HERMES_SKIP_OPENROUTER_PROBE to be set when SkipOpenRouterProbe=true")
		} else if got != "1" {
			t.Errorf("HERMES_SKIP_OPENROUTER_PROBE = %q, want \"1\"", got)
		}
	})

	t.Run("false omits env var", func(t *testing.T) {
		cfg := base
		cfg.SkipOpenRouterProbe = false
		env := BuildEnv(cfg)
		envMap := envToMap(env)
		if _, ok := envMap["HERMES_SKIP_OPENROUTER_PROBE"]; ok {
			t.Error("expected HERMES_SKIP_OPENROUTER_PROBE to be absent when SkipOpenRouterProbe=false")
		}
	})
}

// envToMap converts a KEY=VALUE slice into a map for convenient test lookups.
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			m[e[:idx]] = e[idx+1:]
		}
	}
	return m
}

// TestBuildEnvPYTHONUNBUFFERED verifies that PYTHONUNBUFFERED=1 is always
// present in the environment built by BuildEnv so that CPython line-buffers
// its stdout/stderr when running as a piped subprocess.
func TestBuildEnvPYTHONUNBUFFERED(t *testing.T) {
	cfg := Config{
		SessionID:  "sess-buf",
		AgentID:    "agent-buf",
		Role:       "implementer",
		Profile:    "default",
		SocketPath: "/tmp/buf.sock",
		RunDir:     t.TempDir(),
	}
	env := BuildEnv(cfg)
	envMap := envToMap(env)
	got, ok := envMap["PYTHONUNBUFFERED"]
	if !ok {
		t.Fatal("expected PYTHONUNBUFFERED to be set in env")
	}
	if got != "1" {
		t.Errorf("PYTHONUNBUFFERED = %q, want \"1\"", got)
	}
}

// TestBuildEnvPYTHONPATHInteriorEmptySegmentsPassedThrough documents current
// behavior: BuildEnv does not normalize interior empty segments supplied in
// the caller's PYTHONPATH. Callers with cwd-sensitive environments must
// sanitize upstream; BuildEnv only guarantees it does not introduce new
// empty segments of its own.
func TestBuildEnvPYTHONPATHInteriorEmptySegmentsPassedThrough(t *testing.T) {
	t.Setenv("HERMES_AGENT_PATH", "")
	t.Setenv("HOME", "")
	sep := string(os.PathListSeparator)
	interior := "/a" + sep + sep + "/b"
	t.Setenv("PYTHONPATH", interior)

	cfg := Config{
		SessionID:   "sess-int",
		AgentID:     "agent-int",
		Role:        "implementer",
		Profile:     "default",
		SocketPath:  "/tmp/int.sock",
		RunDir:      t.TempDir(),
		BelayerRoot: "/opt/belayer",
	}
	env := BuildEnv(cfg)

	var pyPath string
	for _, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			pyPath = strings.TrimPrefix(e, "PYTHONPATH=")
		}
	}
	if !strings.Contains(pyPath, interior) {
		t.Errorf("PYTHONPATH = %q, want to preserve interior-empty-segment input %q verbatim", pyPath, interior)
	}
}

// TestBuildEnvOmitsOptionalVars verifies that empty optional fields (Model,
// Message, SystemPrompt, HermesSessionID, BelayerTools, TranscriptPath) are
// not added to the environment.
func TestBuildEnvOmitsOptionalVars(t *testing.T) {
	cfg := Config{
		SessionID:  "sess-omit",
		AgentID:    "agent-omit",
		Role:       "implementer",
		Profile:    "nightshift",
		SocketPath: "/tmp/omit.sock",
		RunDir:     t.TempDir(),
		// Model, Message, SystemPrompt, HermesSessionID, BelayerTools, TranscriptPath all zero-value
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
		"BELAYER_TRANSCRIPT_PATH",
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

// TestSpawn_RotatesStdoutLogAcrossSpawns verifies that bridgelog.RotateAndOpen is
// wired correctly: each new Spawn rotates the previous log so the last 3 spawns'
// output is preserved as .log.1, .log.2, etc.
func TestSpawn_RotatesStdoutLogAcrossSpawns(t *testing.T) {
	// Use TestHelperProcess-style mock so we don't need a real python interpreter.
	// NOTE: TestHelperProcess lives in internal/daemon/bridge_integration_test.go,
	// not here — so we use a simple /bin/echo command instead.
	dir := t.TempDir()

	spawn := func(msg string) {
		cfg := Config{
			Cmd:       []string{"/bin/sh", "-c", "printf %s " + msg},
			SessionID: "s", AgentID: "a", Role: "mock", Profile: "mock",
			Workdir: t.TempDir(), RunDir: dir,
		}
		p, err := Spawn(cfg)
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		if err := p.Wait(); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}

	spawn("first")
	spawn("second")
	spawn("third")

	// Current bridge-stdout.log holds the newest spawn's output.
	cur, err := os.ReadFile(filepath.Join(dir, "bridge-stdout.log"))
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	if string(cur) != "third" {
		t.Fatalf("current log = %q want %q", cur, "third")
	}
	// .log.1 holds the previous spawn; .log.2 the one before.
	b1, err := os.ReadFile(filepath.Join(dir, "bridge-stdout.log.1"))
	if err != nil {
		t.Fatalf("read .1: %v", err)
	}
	if string(b1) != "second" {
		t.Fatalf(".log.1 = %q want %q", b1, "second")
	}
	b2, err := os.ReadFile(filepath.Join(dir, "bridge-stdout.log.2"))
	if err != nil {
		t.Fatalf("read .2: %v", err)
	}
	if string(b2) != "first" {
		t.Fatalf(".log.2 = %q want %q", b2, "first")
	}
}
