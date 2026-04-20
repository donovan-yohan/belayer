package daemon

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
)

// fakeProcessHandle implements bridge.ProcessHandle for tests.
// exitCh is closed when the process should be considered exited.
type fakeProcessHandle struct {
	exitCh  chan struct{}
	waitErr error
}

func (f *fakeProcessHandle) Wait() error {
	<-f.exitCh
	return f.waitErr
}

func (f *fakeProcessHandle) Kill() error {
	select {
	case <-f.exitCh:
	default:
		close(f.exitCh)
	}
	return nil
}

// fakeStdinPipe is a no-op io.WriteCloser used in place of a real stdin pipe.
type fakeStdinPipe struct{}

func (fakeStdinPipe) Write(p []byte) (int, error) { return len(p), nil }
func (fakeStdinPipe) Close() error                { return nil }

// newImmediateProc returns a *bridge.Process that has already exited with the
// given error. Useful for testing the "bridge exited during spawn" path.
func newImmediateProc(err error) *bridge.Process {
	h := &fakeProcessHandle{exitCh: make(chan struct{}), waitErr: err}
	close(h.exitCh)
	return bridge.NewProcess(h, fakeStdinPipe{})
}

// newLiveProc returns a *bridge.Process backed by a handle that stays alive
// until the returned cancel func is called.
func newLiveProc() (*bridge.Process, func()) {
	h := &fakeProcessHandle{exitCh: make(chan struct{})}
	proc := bridge.NewProcess(h, fakeStdinPipe{})
	return proc, func() {
		select {
		case <-h.exitCh:
		default:
			close(h.exitCh)
		}
	}
}

// Ensure bridge.Process methods are accessible (compile-time check).
var _ = (*bridge.Process).MarkLive
var _ = (*bridge.Process).FirstEvent
var _ = func() io.WriteCloser { return fakeStdinPipe{} }

// --- Test: Bridge exits immediately → spawn returns error ---

// TestSpawnAgent_BridgeCrashDuringStartup verifies that when the bridge
// subprocess exits before posting any event, spawnAgentInternal returns an
// error containing "exited during spawn".
func TestSpawnAgent_BridgeCrashDuringStartup(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "crash-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	// Build a runDir in a temp dir so TailLines can find the fake stderr.
	tempBase := t.TempDir()
	runDir := filepath.Join(tempBase, ".belayer", "runs", sess.ID, "worker")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stderrPath := filepath.Join(runDir, "bridge-stderr.log")
	if err := os.WriteFile(stderrPath, []byte("ModuleNotFoundError: No module named 'hermes_bridge'\n"), 0o600); err != nil {
		t.Fatalf("WriteFile stderr: %v", err)
	}

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		return newImmediateProc(fmt.Errorf("exit status 1")), nil
	}

	_, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sess.ID,
		Name:      "worker",
		Role:      "implementer",
		Profile:   "default",
		Workdir:   tempBase,
	})
	if err == nil {
		t.Fatal("expected error from spawnAgentInternal, got nil")
	}
	if !strings.Contains(err.Error(), "exited during spawn") {
		t.Errorf("expected error to contain 'exited during spawn', got: %v", err)
	}
	// stderr tail content should appear in the error message
	if !strings.Contains(err.Error(), "hermes_bridge") {
		t.Errorf("expected stderr tail in error message, got: %v", err)
	}
}

// --- Test: Bridge heartbeats within 100ms → spawn returns success ---

// TestSpawnAgent_BridgeHeartbeatsQuickly verifies that when the bridge posts
// an event within the 500ms window, spawnAgentInternal returns successfully
// and the agent run is in "running" status.
func TestSpawnAgent_BridgeHeartbeatsQuickly(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "heartbeat-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	proc, cancel := newLiveProc()
	defer cancel()

	var spawnedProc *bridge.Process
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		spawnedProc = proc
		return proc, nil
	}

	// Simulate the bridge posting a heartbeat event after 50ms.
	// In production handleLogEvent calls MarkLive; here we call it directly.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if spawnedProc != nil {
			spawnedProc.MarkLive()
		}
	}()

	stored, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sess.ID,
		Name:      "fast-agent",
		Role:      "implementer",
		Profile:   "default",
	})
	if err != nil {
		t.Fatalf("spawnAgentInternal: unexpected error: %v", err)
	}
	if stored.Status != "running" {
		t.Errorf("expected status=running, got %q", stored.Status)
	}
}

// --- Test: Bridge takes >500ms → spawn returns success (timeout path) ---

// TestSpawnAgent_SlowStartAssumedOK verifies that when neither an event nor a
// process exit occurs within 500ms, spawnAgentInternal returns successfully
// and the agent run is "running" (the watchBridgeExit goroutine catches later crashes).
func TestSpawnAgent_SlowStartAssumedOK(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "slow-start-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	proc, cancel := newLiveProc()
	defer cancel()

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		return proc, nil
	}

	start := time.Now()
	stored, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sess.ID,
		Name:      "slow-agent",
		Role:      "implementer",
		Profile:   "default",
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("spawnAgentInternal: unexpected error: %v", err)
	}
	if stored.Status != "running" {
		t.Errorf("expected status=running, got %q", stored.Status)
	}
	// The timeout path should take roughly 500ms.
	if elapsed < 450*time.Millisecond {
		t.Errorf("spawn returned in %v, expected ~500ms for slow-start timeout path", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("spawn took too long: %v", elapsed)
	}
}

// --- Test: HTTP endpoint propagates startup crash to caller ---

// TestHandleSpawnAgent_BridgeCrashDuringStartup verifies that the HTTP spawn
// endpoint returns a non-2xx status when the bridge crashes on startup, so the
// supervisor tool result surfaces the failure message.
func TestHandleSpawnAgent_BridgeCrashDuringStartup(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "http-crash-test"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		return newImmediateProc(fmt.Errorf("exit status 1")), nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "http-worker",
		Role:    "implementer",
		Profile: "default",
	})

	if rr.Code == http.StatusCreated {
		t.Fatalf("expected non-201 response when bridge crashes on startup, got 201: %s", rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "exited during spawn") {
		t.Errorf("expected 'exited during spawn' in response body, got: %s", body)
	}
}
