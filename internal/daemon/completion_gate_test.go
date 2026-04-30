package daemon

import (
	"net/http"
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
