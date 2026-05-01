package daemon

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/store"
)

// TestSpawnAgent_ResumesExistingHermesSessionID verifies that spawning an
// agent whose prior row already has a hermes_session_id reuses it in the
// subsequent spawnBridgeAgent call. This is the resume path: an agent exits,
// the supervisor re-spawns it, and Hermes picks up the same conversation.
//
// Reference: internal/daemon/agents.go spawnAgentInternal resume branch
// (the prior Hermes session ID copy into an empty resume request).
// A silent regression here would create a fresh Hermes session on every
// re-spawn, duplicating sessions and losing conversation history.
func TestSpawnAgent_ResumesExistingHermesSessionID(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d)

	// Seed an agent run that already completed a Hermes session.
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID:       sessionID,
		Name:            "web-dev",
		Role:            "web-dev",
		Kind:            "main",
		Profile:         "default",
		HermesSessionID: "hermes-abc-123",
		Status:          "exited",
		Transport:       "tmux",
	}); err != nil {
		t.Fatalf("seed agent run: %v", err)
	}

	var mu sync.Mutex
	var seen []string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		seen = append(seen, req.HermesSessionID)
		mu.Unlock()
		proc, _ := newLiveProc()
		go func() {
			time.Sleep(10 * time.Millisecond)
			proc.MarkLive()
		}()
		return proc, nil
	}

	_, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sessionID,
		Name:      "web-dev",
		Role:      "web-dev",
		Profile:   "default",
		Message:   "keep going",
	})
	if err != nil {
		t.Fatalf("spawnAgentInternal: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("spawnBridgeAgent call count = %d, want 1", len(seen))
	}
	if seen[0] != "hermes-abc-123" {
		t.Errorf("spawnBridgeAgent received HermesSessionID = %q, want %q (resume path broken)", seen[0], "hermes-abc-123")
	}
}

// TestSpawnAgent_FreshWhenNoPrior verifies the no-resume path: an agent with
// no prior run should receive an empty HermesSessionID so Hermes creates a
// new conversation. This is the counterpoint to resume — regressions that
// always fill in a stale session id would be caught here.
func TestSpawnAgent_FreshWhenNoPrior(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d)

	var mu sync.Mutex
	var seen []string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		seen = append(seen, req.HermesSessionID)
		mu.Unlock()
		proc, _ := newLiveProc()
		go func() {
			time.Sleep(10 * time.Millisecond)
			proc.MarkLive()
		}()
		return proc, nil
	}

	_, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sessionID,
		Name:      "fresh-agent",
		Role:      "implementer",
		Profile:   "default",
	})
	if err != nil {
		t.Fatalf("spawnAgentInternal: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("spawnBridgeAgent call count = %d, want 1", len(seen))
	}
	if seen[0] != "" {
		t.Errorf("spawnBridgeAgent received HermesSessionID = %q, want empty (no resume)", seen[0])
	}
}

func TestSpawnAgent_DoesNotOverwriteBridgeFinishedDuringStartup(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "fast-finish"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		postBridgeEvent(t, d, req.SessionID, "bridge:finished", map[string]any{
			"agent":          req.Name,
			"final_response": "finished before spawn bookkeeping caught up",
		})
		return nil, nil
	}

	run, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID: sess.ID,
		Name:      "qa",
		Role:      "qa",
		Kind:      "side",
		Profile:   "default",
		Workdir:   t.TempDir(),
		Message:   "quick check",
	})
	if err != nil {
		t.Fatalf("spawnAgentInternal: %v", err)
	}
	if run.Status != "complete" {
		t.Fatalf("spawnAgentInternal returned status %q, want complete", run.Status)
	}

	stored, err := d.store.GetAgentRun(sess.ID, "qa")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if stored.Status != "complete" {
		t.Fatalf("stored status = %q, want complete", stored.Status)
	}
	if stored.Outcome != "succeeded" {
		t.Fatalf("stored outcome = %q, want succeeded", stored.Outcome)
	}
}
