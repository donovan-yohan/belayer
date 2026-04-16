package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// setupSessionWithAgents creates a session and registers the given agent names
// with running status so bridge event side effects have something to update.
func setupSessionWithAgents(t *testing.T, d *Daemon, agentNames ...string) string {
	t.Helper()
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "bridge-test"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	for _, name := range agentNames {
		_, err := d.store.CreateAgentRun(store.AgentRun{
			SessionID: sess.ID,
			Name:      name,
			Role:      name,
			Profile:   "default",
			Status:    "running",
			Transport: "tmux",
		})
		if err != nil {
			t.Fatalf("create agent run %q: %v", name, err)
		}
	}
	return sess.ID
}

func postBridgeEvent(t *testing.T, d *Daemon, sessionID, eventType string, data map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	return doRequest(t, d, "POST", "/sessions/"+sessionID+"/events", logEventRequest{
		Type: eventType,
		Data: string(dataJSON),
	})
}

// TestBridgeFinishedUpdatesAgentStatusToComplete verifies that posting a
// bridge:finished event marks the agent run as complete in the store.
func TestBridgeFinishedUpdatesAgentStatusToComplete(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	rr := postBridgeEvent(t, d, sessionID, "bridge:finished", map[string]any{
		"agent":          "worker",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Fatalf("expected status=complete, got %q", run.Status)
	}
}

// TestBridgeFailedUpdatesAgentStatusToBlocked verifies that posting a
// bridge:failed event marks the agent run as blocked in the store.
func TestBridgeFailedUpdatesAgentStatusToBlocked(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	rr := postBridgeEvent(t, d, sessionID, "bridge:failed", map[string]any{
		"agent": "worker",
		"error": "something went wrong",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "blocked" {
		t.Fatalf("expected status=blocked, got %q", run.Status)
	}
}

// TestBridgeFinishedForNonPlannerUpdatesStatusOnly verifies that a
// bridge:finished event from a non-supervisor agent updates its status to
// "complete" but does NOT auto-generate a message to the supervisor.
// The specialist is expected to send its own report via belayer_send_message.
func TestBridgeFinishedForNonPlannerUpdatesStatusOnly(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	postBridgeEvent(t, d, sessionID, "bridge:finished", map[string]any{
		"agent":          "worker",
		"final_response": "task done",
	})

	// Status should be updated to complete.
	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Fatalf("expected status=complete, got %q", run.Status)
	}

	// No auto-generated message to the supervisor (avoids duplicate noise).
	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no auto-generated messages for supervisor, got %d: %#v", len(msgs), msgs)
	}
}

// TestBridgeFailedForNonPlannerSendsUrgentMessageToPlanner verifies that a
// bridge:failed event from a non-supervisor agent results in an urgent message
// persisted to the messages table addressed to the supervisor.
func TestBridgeFailedForNonPlannerSendsUrgentMessageToPlanner(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	postBridgeEvent(t, d, sessionID, "bridge:failed", map[string]any{
		"agent": "worker",
		"error": "fatal error",
	})

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one pending message for supervisor after bridge:failed")
	}
	found := false
	for _, m := range msgs {
		if m.SenderID == "worker" && m.RecipientID == "supervisor" && m.Urgent {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected urgent message from worker to supervisor, got %#v", msgs)
	}
}

// TestBridgeFinishedForPlannerDoesNotSendMessage verifies that when the supervisor
// itself posts bridge:finished, no message is sent to the supervisor.
func TestBridgeFinishedForPlannerDoesNotSendMessage(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")

	postBridgeEvent(t, d, sessionID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "session complete",
	})

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no pending messages for supervisor when supervisor itself finishes, got %d", len(msgs))
	}

	// Status should still be updated.
	run, err := d.store.GetAgentRun(sessionID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Fatalf("expected status=complete, got %q", run.Status)
	}
}

// TestUnknownBridgeEventDoesNotError verifies that unrecognized bridge:* event
// types are handled gracefully (log-only) and do not produce errors.
func TestUnknownBridgeEventDoesNotError(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker")

	for _, eventType := range []string{
		"bridge:started",
		"bridge:step_completed",
		"bridge:heartbeat",
		"bridge:tool_started",
		"bridge:tool_completed",
		"bridge:status_change",
		"bridge:unknown_future_type",
	} {
		rr := postBridgeEvent(t, d, sessionID, eventType, map[string]any{
			"agent": "worker",
		})
		if rr.Code != http.StatusCreated {
			t.Errorf("event %q: expected 201, got %d: %s", eventType, rr.Code, rr.Body.String())
		}
	}

	// Worker status should be unchanged (still running) — no side effects.
	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("expected status=running after log-only events, got %q", run.Status)
	}
}

// TestBridgeEventWithoutAgentFieldDoesNotPanic verifies that bridge events
// missing the agent field are silently ignored without panicking.
func TestBridgeEventWithoutAgentFieldDoesNotPanic(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker")

	// No "agent" key in data — processBridgeEvent should return early.
	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/events", logEventRequest{
		Type: "bridge:finished",
		Data: `{"final_response":"done"}`,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestBridgeClarificationNeededSendsMessageToPlanner verifies that a
// bridge:clarification_needed event results in a message persisted to the
// messages table for the supervisor.
func TestBridgeClarificationNeededSendsMessageToPlanner(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	postBridgeEvent(t, d, sessionID, "bridge:clarification_needed", map[string]any{
		"agent":    "worker",
		"question": "which endpoint should I use?",
	})

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one pending message for supervisor after bridge:clarification_needed")
	}
	found := false
	for _, m := range msgs {
		if m.SenderID == "worker" && m.RecipientID == "supervisor" && m.Type == "input-needed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected input-needed message from worker to supervisor, got %#v", msgs)
	}
}
