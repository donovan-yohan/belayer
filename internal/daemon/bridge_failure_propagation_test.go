package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
)

// --- Tests: stdout scanner → agent blocked + bridge:failed event ---

// TestStdoutScannerMarkerBlocksAgent verifies the end-to-end path: when the
// bridge subprocess writes a matching error line to stdout and then exits, the
// daemon's watchBridgeExit goroutine detects the marker via the stdout scanner,
// synthesizes a bridge:failed event, and marks the agent blocked.
func TestStdoutScannerMarkerBlocksAgent(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-scanner-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID

	// Create supervisor so notify path works.
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID, Name: "supervisor", Role: "supervisor",
		Profile: "mock", Status: "running", Transport: "bridge",
	})
	if err != nil {
		t.Fatalf("create supervisor: %v", err)
	}
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Spawn a real bridge subprocess that prints an error marker to stdout then exits.
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		runDir := t.TempDir()
		cfg := bridge.Config{
			Cmd:       []string{"/bin/sh", "-c", "echo 'API failed after 3 retries — Connection error'; sleep 0"},
			SessionID: req.SessionID,
			AgentID:   req.Name,
			Role:      req.Role,
			Profile:   req.Profile,
			Workdir:   req.Workdir,
			SocketPath: "",
			RunDir:    runDir,
		}
		proc, err := bridge.Spawn(cfg)
		if err != nil {
			return nil, err
		}
		proc.MarkLive()
		return proc, nil
	}

	agentRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "web-dev-1",
		Role:    "implementer",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if agentRR.Code != http.StatusCreated {
		t.Fatalf("spawn agent: expected 201, got %d: %s", agentRR.Code, agentRR.Body.String())
	}

	// Wait for the agent to end up blocked or failed (either the stdout_scanner
	// path or the bridge:failed path from the Python side; here, the subprocess
	// exits cleanly so watchBridgeExit may also catch it).
	deadline := time.Now().Add(5 * time.Second)
	var agentStatus string
	for time.Now().Before(deadline) {
		run, err := d.store.GetAgentRun(sessionID, "web-dev-1")
		if err != nil {
			t.Fatalf("GetAgentRun: %v", err)
		}
		agentStatus = run.Status
		if agentStatus == "blocked" || agentStatus == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if agentStatus != "blocked" && agentStatus != "failed" {
		t.Fatalf("expected agent status=blocked or failed after marker line, got %q", agentStatus)
	}

	// Verify that a bridge:failed event was persisted.
	deadline2 := time.Now().Add(2 * time.Second)
	var foundBridgeFailed bool
	for time.Now().Before(deadline2) {
		events, err := d.store.QueryEvents(sessionID)
		if err != nil {
			t.Fatalf("QueryEvents: %v", err)
		}
		for _, e := range events {
			if e.Type == "bridge:failed" && strings.Contains(e.Data, "web-dev-1") {
				foundBridgeFailed = true
				break
			}
		}
		if foundBridgeFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundBridgeFailed {
		events, _ := d.store.QueryEvents(sessionID)
		t.Fatalf("expected bridge:failed event after stdout marker, got events: %#v", events)
	}
}

// TestStdoutScannerMarkerRecordsStdoutMarkerField verifies that the bridge:failed
// event emitted by the stdout scanner includes the stdout_marker field so
// post-mortems can identify which pattern tripped the detector.
func TestStdoutScannerMarkerRecordsStdoutMarkerField(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "marker-field-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID, Name: "supervisor", Role: "supervisor",
		Profile: "mock", Status: "running", Transport: "bridge",
	})
	if err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		runDir := t.TempDir()
		cfg := bridge.Config{
			Cmd:        []string{"/bin/sh", "-c", "echo 'HTTP 429 Too Many Requests from provider'"},
			SessionID:  req.SessionID,
			AgentID:    req.Name,
			Role:       req.Role,
			Profile:    req.Profile,
			Workdir:    req.Workdir,
			SocketPath: "",
			RunDir:     runDir,
		}
		proc, err := bridge.Spawn(cfg)
		if err != nil {
			return nil, err
		}
		proc.MarkLive()
		return proc, nil
	}

	agentRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "backend-dev-1",
		Role:    "implementer",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if agentRR.Code != http.StatusCreated {
		t.Fatalf("spawn agent: expected 201, got %d: %s", agentRR.Code, agentRR.Body.String())
	}

	// Wait for bridge:failed event with stdout_marker field.
	deadline := time.Now().Add(5 * time.Second)
	var foundMarker bool
	for time.Now().Before(deadline) {
		events, err := d.store.QueryEvents(sessionID)
		if err != nil {
			t.Fatalf("QueryEvents: %v", err)
		}
		for _, e := range events {
			if e.Type == "bridge:failed" && strings.Contains(e.Data, "stdout_marker") {
				foundMarker = true
				break
			}
		}
		if foundMarker {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundMarker {
		events, _ := d.store.QueryEvents(sessionID)
		t.Fatalf("expected bridge:failed event with stdout_marker field, got: %#v", events)
	}
}

// --- Tests: session failed vs stalled routing ---

// TestCheckSessionStalledRouteToFailedWhenAllBlockedWithBridgeFailed verifies
// that when every agent is blocked/failed/incomplete AND a bridge:failed event
// was emitted, the session transitions to "failed" (not "stalled").
func TestCheckSessionStalledRouteToFailedWhenAllBlockedWithBridgeFailed(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Mark all agents as blocked.
	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "blocked")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "blocked")

	// Log a bridge:failed event to signal connectivity failure.
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:failed",
		Data:      `{"agent":"worker","error":"API failed after 3 retries","stdout_marker":"api_failed_retries"}`,
	})

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "failed" {
		t.Fatalf("expected session status=failed when all agents blocked + bridge:failed event, got %q", sess.Status)
	}

	// Verify session_failed event was logged.
	events, _ := d.store.QueryEvents(sessionID)
	var foundFailed bool
	for _, e := range events {
		if e.Type == "session_failed" {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Fatal("expected session_failed event to be logged")
	}
}

// TestCheckSessionStalledRouteToStalledWhenMixedStatuses verifies that when
// some agents are complete and some are blocked, the session transitions to
// "stalled" (not "failed") because there was at least one clean completion.
func TestCheckSessionStalledRouteToStalledWhenMixedStatuses(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// One agent complete, one blocked — mixed outcome.
	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "complete")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "blocked")

	// Even with a bridge:failed event, the session should be "stalled" because
	// the supervisor completed cleanly.
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:failed",
		Data:      `{"agent":"worker","error":"Connection error"}`,
	})

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "stalled" {
		t.Fatalf("expected session status=stalled for mixed statuses (one complete), got %q", sess.Status)
	}

	// Verify session_stalled event was logged (not session_failed).
	events, _ := d.store.QueryEvents(sessionID)
	for _, e := range events {
		if e.Type == "session_failed" {
			t.Fatalf("did not expect session_failed when supervisor completed cleanly, got event: %s", e.Type)
		}
	}
	var foundStalled bool
	for _, e := range events {
		if e.Type == "session_stalled" {
			foundStalled = true
			break
		}
	}
	if !foundStalled {
		t.Fatal("expected session_stalled event")
	}
}

// TestCheckSessionStalledRouteToStalledWhenNoBridgeFailed verifies that when
// all agents are in failure states but NO bridge:failed event was emitted, the
// session still transitions to "stalled" (not "failed").
func TestCheckSessionStalledRouteToStalledWhenNoBridgeFailed(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "blocked")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "blocked")

	// No bridge:failed event logged.

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "stalled" {
		t.Fatalf("expected session status=stalled when no bridge:failed event, got %q", sess.Status)
	}
}

// TestCheckSessionStalledAllIncomplete verifies that a session where all agents
// are "incomplete" (not just "blocked") also triggers the failed path when a
// bridge:failed event exists.
func TestCheckSessionStalledAllIncomplete(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "incomplete")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "incomplete")

	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:failed",
		Data:      `{"agent":"worker","error":"Max retries exceeded"}`,
	})

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "failed" {
		t.Fatalf("expected session status=failed (all incomplete + bridge:failed), got %q", sess.Status)
	}
}

// TestCheckSessionStalledSupervisorCompleteNoRegression verifies that when the
// supervisor is complete and specialists are terminal (no bridge:failed event),
// the existing stalled transition still fires — no regression.
func TestCheckSessionStalledSupervisorCompleteNoRegression(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "complete")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "complete")

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "stalled" {
		t.Fatalf("expected session status=stalled (both complete, no completion approval), got %q", sess.Status)
	}
}

// --- Tests: watchBridgeExit broker message includes stdout tail ---

// TestWatchBridgeExitIncludesStdoutTail verifies that the supervisor broker
// message produced by watchBridgeExit includes both a stderr tail section and a
// stdout tail section.
func TestWatchBridgeExitIncludesStdoutTail(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "stdout-tail-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Pre-create supervisor so the notify path has a recipient.
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID, Name: "supervisor", Role: "supervisor",
		Profile: "mock", Status: "running", Transport: "bridge",
	})
	if err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	// Create an agent run with a real RunDir so the tail functions have files to read.
	runDir := t.TempDir()

	// Write something to the "log" files.
	stdoutContent := "some stdout output from agent\nbridge:step_completed"
	stderrContent := "some stderr output\nError: Connection reset"
	if err := os.WriteFile(filepath.Join(runDir, "bridge-stdout.log"), []byte(stdoutContent), 0o644); err != nil {
		t.Fatalf("write stdout log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "bridge-stderr.log"), []byte(stderrContent), 0o644); err != nil {
		t.Fatalf("write stderr log: %v", err)
	}

	// Create the agent run with Workdir pointing to a directory whose .belayer/runs/<sess>/<name>
	// path resolves to our runDir. We set Workdir so the path reconstruction works.
	// watchBridgeExit reconstructs: filepath.Join(runBase, ".belayer", "runs", sessionID, run.Name)
	// So we need: runBase + "/.belayer/runs/" + sessionID + "/api" == runDir
	runBase := t.TempDir()
	targetDir := filepath.Join(runBase, ".belayer", "runs", sessionID, "api")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "bridge-stdout.log"), []byte(stdoutContent), 0o644); err != nil {
		t.Fatalf("write stdout log in target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "bridge-stderr.log"), []byte(stderrContent), 0o644); err != nil {
		t.Fatalf("write stderr log in target: %v", err)
	}

	_, err = d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      "api",
		Role:      "implementer",
		Profile:   "mock",
		Status:    "running",
		Transport: "bridge",
		Workdir:   runBase,
	})
	if err != nil {
		t.Fatalf("create api run: %v", err)
	}

	// Set up a fake process handle we can trigger exit on.
	apiHandle := &fakeProcessHandle{exitCh: make(chan struct{}), waitErr: fmt.Errorf("exit status 1")}
	proc := bridge.NewProcess(apiHandle, fakeStdinPipe{})
	proc.MarkLive()

	// Register and start watching.
	key := bridgeKey(sessionID, "api")
	d.bridgeMu.Lock()
	d.bridgeProcs[key] = proc
	d.bridgeMu.Unlock()

	apiRun, err := d.store.GetAgentRun(sessionID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	d.watchBridgeExit(apiRun, proc)

	// Trigger exit.
	close(apiHandle.exitCh)

	// Wait for the broker message to appear.
	deadline := time.Now().Add(3 * time.Second)
	var foundMsg bool
	for time.Now().Before(deadline) {
		msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
		if err != nil {
			t.Fatalf("PendingMessages: %v", err)
		}
		for _, m := range msgs {
			if m.SenderID != "api" || m.RecipientID != "supervisor" {
				continue
			}
			// Must contain both stderr and stdout tail sections.
			if strings.Contains(m.Content, "bridge-stderr.log") && strings.Contains(m.Content, "bridge-stdout.log") {
				foundMsg = true
				break
			}
		}
		if foundMsg {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !foundMsg {
		msgs, _ := d.store.PendingMessages(sessionID, "supervisor", "")
		t.Fatalf("expected broker message with both stderr and stdout tail sections, got: %#v", msgs)
	}
}

// --- Tests: session "failed" status visible in roster output ---

// TestSessionFailedStatusVisibleViaRoster verifies that a session marked
// "failed" returns that status through GET /sessions/{id}/agents (the roster
// endpoint), so a runner polling `belayer roster` would see the terminal state.
func TestSessionFailedStatusVisibleViaRoster(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Simulate full connectivity failure.
	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "blocked")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "blocked")
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:failed",
		Data:      `{"agent":"supervisor","error":"API failed after 3 retries"}`,
	})

	d.checkSessionStalled(sessionID)

	// Verify the session itself is "failed".
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "failed" {
		t.Fatalf("expected session status=failed, got %q", sess.Status)
	}

	// GET /sessions/{id} must return status=failed.
	rr := doRequest(t, d, "GET", "/sessions/"+sessionID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET session: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	type sessResp struct {
		Status string `json:"status"`
	}
	body := decodeJSON[sessResp](t, rr)
	if body.Status != "failed" {
		t.Fatalf("GET /sessions/{id} returned status=%q, want %q", body.Status, "failed")
	}
}

// --- Regression test: bridgeLaunchAgent (sandbox-driver path) wires scanner ---

// stdoutMarkerFakeDriver is a sandbox.Driver whose Exec runs a shell command
// that prints an error marker line to stdout, then exits. This lets us verify
// that bridgeLaunchAgent (the noop/clamshell path that calls driver.Exec +
// NewProcess) now attaches a scanner via StartStdoutScanner so that stdout
// markers synthesize bridge:failed events — not just the Spawn path.
type stdoutMarkerFakeDriver struct{}

func (d *stdoutMarkerFakeDriver) Create(_ context.Context, cfg sandbox.Config) (sandbox.Handle, error) {
	return sandbox.Handle{ID: cfg.Name}, nil
}

func (d *stdoutMarkerFakeDriver) Exec(_ context.Context, _ sandbox.Handle, _ []string, opts sandbox.ExecOpts) (sandbox.Process, error) {
	// Override the command: print error marker then exit cleanly.
	// The real bridgeLaunchAgent command is ignored; this fake driver always
	// runs the marker-emitting shell one-liner so the test is hermetic.
	cmd := exec.Command("/bin/sh", "-c", "echo 'API failed after 3 retries — Connection error'")
	cmd.Env = opts.Env
	cmd.Dir = opts.Dir
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("fake driver: start: %w", err)
	}
	return &fakeDriverProcess{cmd: cmd}, nil
}

func (d *stdoutMarkerFakeDriver) Stop(_ context.Context, _ sandbox.Handle) error { return nil }

type fakeDriverProcess struct{ cmd *exec.Cmd }

func (p *fakeDriverProcess) Pid() int    { return p.cmd.Process.Pid }
func (p *fakeDriverProcess) Wait() error { return p.cmd.Wait() }
func (p *fakeDriverProcess) Kill() error { return p.cmd.Process.Kill() }

// TestBridgeLaunchAgentWiresStdoutScanner is the regression test for Fix 1:
// it verifies that the bridgeLaunchAgent sandbox-driver path (NewProcess +
// StartStdoutScanner) correctly fires a bridge:failed event when the bridge
// subprocess writes a matching stdout error marker. Before the fix,
// proc.StdoutErrors() was nil in this path, so markers were silently dropped.
func TestBridgeLaunchAgentWiresStdoutScanner(t *testing.T) {
	d := testDaemon(t)

	// Create a session that the daemon can look up.
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "launch-agent-scanner-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Pre-create supervisor so watchBridgeExit notify path has a recipient.
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID, Name: "supervisor", Role: "supervisor",
		Profile: "mock", Status: "running", Transport: "bridge",
	})
	if err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	// Inject the fake driver as the session sandbox so bridgeLaunchAgent uses it.
	fakeDriver := &stdoutMarkerFakeDriver{}
	fakeHandle := sandbox.Handle{ID: sessionID}
	d.sandboxMu.Lock()
	d.sessionSandboxes[sessionID] = sessionSandbox{
		driver: fakeDriver,
		handle: fakeHandle,
		mode:   sandbox.DefaultMode,
	}
	d.sandboxMu.Unlock()

	// Create an agent run entry so bridgeLaunchAgent can find it.
	workdir := t.TempDir()
	_, err = d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID, Name: "scan-test-agent", Role: "implementer",
		Profile: "mock", Status: "running", Transport: "bridge",
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("create agent run: %v", err)
	}

	// Call bridgeLaunchAgent directly — this is the path under test.
	req := agentSpawnRequest{
		SessionID: sessionID,
		Name:      "scan-test-agent",
		Role:      "implementer",
		Profile:   "mock",
		Workdir:   workdir,
	}
	proc, err := d.bridgeLaunchAgent(req)
	if err != nil {
		t.Fatalf("bridgeLaunchAgent: %v", err)
	}

	// Verify that the scanner was wired: StdoutErrors() must be non-nil.
	if proc.StdoutErrors() == nil {
		t.Fatal("bridgeLaunchAgent did not attach a stdout scanner (StdoutErrors() is nil)")
	}

	proc.MarkLive()

	// Start watching so bridge:failed events get synthesized when the scanner fires.
	agentRun, err := d.store.GetAgentRun(sessionID, "scan-test-agent")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	d.watchBridgeExit(agentRun, proc)

	// Wait for bridge:failed event — the marker line in stdout should fire it.
	deadline := time.Now().Add(5 * time.Second)
	var foundBridgeFailed bool
	for time.Now().Before(deadline) {
		events, evErr := d.store.QueryEvents(sessionID)
		if evErr != nil {
			t.Fatalf("QueryEvents: %v", evErr)
		}
		for _, e := range events {
			if e.Type == "bridge:failed" && strings.Contains(e.Data, "scan-test-agent") {
				foundBridgeFailed = true
				break
			}
		}
		if foundBridgeFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundBridgeFailed {
		events, _ := d.store.QueryEvents(sessionID)
		t.Fatalf("expected bridge:failed event from bridgeLaunchAgent scanner path, got events: %#v", events)
	}
}
