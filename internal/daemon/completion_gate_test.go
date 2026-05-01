package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/store"
)

// TestCompletionRequested_AutoSpawnsPM verifies that posting a
// bridge:completion_requested event causes the daemon to auto-spawn a "pm"
// agent run with the supervisor's summary embedded in the spawn message.
//
// This is the contract that ties belayer_request_completion (tool, fired from
// the supervisor's bridge) to the adversarial PM verification gate. A silent
// regression here would skip the completion gate entirely.
func TestCompletionRequested_AutoSpawnsPM(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")

	var mu sync.Mutex
	var spawnedNames []string
	var spawnedMessages []string
	spawned := make(chan struct{}, 4)

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		spawnedNames = append(spawnedNames, req.Name)
		spawnedMessages = append(spawnedMessages, req.Message)
		mu.Unlock()
		proc, _ := newLiveProc()
		go func() {
			time.Sleep(10 * time.Millisecond)
			proc.MarkLive()
		}()
		select {
		case spawned <- struct{}{}:
		default:
		}
		return proc, nil
	}

	rr := postBridgeEvent(t, d, sessionID, "bridge:completion_requested", map[string]any{
		"agent":         "supervisor",
		"summary":       "extended the api, added tests, all green",
		"spec_artifact": "docs/spec.md",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	select {
	case <-spawned:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for PM auto-spawn after completion_requested")
	}

	// Poll until PM reaches running status so the background goroutine finishes
	// its store writes before test cleanup closes the DB. Without this wait the
	// goroutine races with t.Cleanup and surfaces a spurious "database is closed"
	// log line.
	var pm store.AgentRun
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pm, err = d.store.GetAgentRun(sessionID, "pm")
		if err == nil && pm.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("expected pm agent run to exist after completion_requested: %v", err)
	}
	if pm.Status != "running" {
		t.Fatalf("pm status = %q, want %q", pm.Status, "running")
	}
	if pm.Role != "pm" {
		t.Errorf("pm role = %q, want %q", pm.Role, "pm")
	}
	if pm.Kind != "side" {
		t.Errorf("pm kind = %q, want %q (PM is a side agent)", pm.Kind, "side")
	}

	mu.Lock()
	defer mu.Unlock()
	sawPM := false
	for i, name := range spawnedNames {
		if name == "pm" {
			sawPM = true
			if !strings.Contains(spawnedMessages[i], "extended the api") {
				t.Errorf("PM spawn message did not include supervisor summary, got: %q", spawnedMessages[i])
			}
			if !strings.Contains(spawnedMessages[i], "docs/spec.md") {
				t.Errorf("PM spawn message did not include spec_artifact, got: %q", spawnedMessages[i])
			}
		}
	}
	if !sawPM {
		t.Fatalf("spawnBridgeAgent never called with name=pm; saw: %v", spawnedNames)
	}
}

func TestCompletionRequested_UsesConfiguredAcceptanceGateTalent(t *testing.T) {
	d := testDaemon(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".belayer"), 0o755); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	config := `gates:
  - name: acceptance
    stage: session
    authority: blocking
    trigger: completion_requested
    assigned_talent:
      - acceptance-editor
    requires:
      - spec-or-task
    conditions:
      - "The delivered work satisfies the task"
    verdicts:
      - pass
      - fail
      - blocked
`
	if err := os.WriteFile(filepath.Join(workspace, ".belayer", "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	sessionID, err := d.store.CreateSession(store.Session{Name: "configured-gate", WorkspaceDir: workspace, Status: "running"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      "supervisor",
		Role:      "supervisor",
		Profile:   "default",
		Status:    "running",
		Transport: "tmux",
	}); err != nil {
		t.Fatalf("create supervisor run: %v", err)
	}

	var mu sync.Mutex
	var spawnedNames []string
	var spawnedMessages []string
	spawned := make(chan struct{}, 4)
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		spawnedNames = append(spawnedNames, req.Name)
		spawnedMessages = append(spawnedMessages, req.Message)
		mu.Unlock()
		proc, _ := newLiveProc()
		go func() {
			time.Sleep(10 * time.Millisecond)
			proc.MarkLive()
		}()
		select {
		case spawned <- struct{}{}:
		default:
		}
		return proc, nil
	}

	rr := postBridgeEvent(t, d, sessionID, "bridge:completion_requested", map[string]any{
		"agent":   "supervisor",
		"summary": "implemented the configured gate flow",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	select {
	case <-spawned:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for acceptance gate spawn after completion_requested")
	}

	var acceptanceRun store.AgentRun
	var acceptanceErr error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		acceptanceRun, acceptanceErr = d.store.GetAgentRun(sessionID, "acceptance-editor")
		if acceptanceErr == nil && acceptanceRun.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if acceptanceErr != nil {
		t.Fatalf("expected acceptance gate agent run to exist after completion_requested: %v", acceptanceErr)
	}
	if acceptanceRun.Status != "running" {
		t.Fatalf("acceptance-editor status = %q, want %q", acceptanceRun.Status, "running")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(spawnedNames) != 1 {
		t.Fatalf("expected one spawned acceptance gate talent, got %v", spawnedNames)
	}
	if spawnedNames[0] != "acceptance-editor" {
		t.Fatalf("spawned %q, want configured acceptance talent %q", spawnedNames[0], "acceptance-editor")
	}
	if !strings.Contains(spawnedMessages[0], "The delivered work satisfies the task") {
		t.Fatalf("spawn message did not include configured gate condition: %q", spawnedMessages[0])
	}
}
