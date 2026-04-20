package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// --- Tests for checkSessionStalled ---

// TestCheckSessionStalledTransitionsWhenAllAgentsDone verifies that when all
// agents are in a terminal state, the session transitions to "stalled".
func TestCheckSessionStalledTransitionsWhenAllAgentsDone(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Mark both agents as complete.
	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "complete")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "complete")

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "stalled" {
		t.Fatalf("expected session status=stalled, got %q", sess.Status)
	}

	// Verify session_stalled event was logged.
	events, _ := d.store.QueryEvents(sessionID)
	found := false
	for _, e := range events {
		if e.Type == "session_stalled" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected session_stalled event to be logged")
	}
}

// TestCheckSessionStalledDoesNotTransitionWhenAgentRunning verifies that the
// session stays "running" when at least one agent is still active.
func TestCheckSessionStalledDoesNotTransitionWhenAgentRunning(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Only one agent is complete; supervisor still running.
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "complete")

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "running" {
		t.Fatalf("expected session status=running (agent still active), got %q", sess.Status)
	}
}

// TestCheckSessionStalledSkipsNonRunningSession verifies that sessions already
// in a terminal state are not re-transitioned.
func TestCheckSessionStalledSkipsNonRunningSession(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")
	_ = d.store.UpdateSessionStatus(sessionID, "complete")
	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "complete")

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "complete" {
		t.Fatalf("expected session to remain complete, got %q", sess.Status)
	}
}

// TestCheckSessionStalledRespectsPendingVerification verifies that agents in
// "pending_verification" are treated as active (session should not stall).
func TestCheckSessionStalledRespectsPendingVerification(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "pm")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	_ = d.store.UpdateAgentRunStatus(sessionID, "supervisor", "pending_verification")
	_ = d.store.UpdateAgentRunStatus(sessionID, "pm", "complete")

	d.checkSessionStalled(sessionID)

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "running" {
		t.Fatalf("expected session to remain running (supervisor pending_verification), got %q", sess.Status)
	}
}

// --- Tests for checkSupervisorExitedEarly ---

// TestCheckSupervisorExitedEarlyWarnsWhenSpecialistRunning verifies that a
// warning event is logged when the supervisor exits while a specialist is still
// running.
func TestCheckSupervisorExitedEarlyWarnsWhenSpecialistRunning(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")

	// Worker is still running when supervisor exits.
	d.checkSupervisorExitedEarly(sessionID)

	events, _ := d.store.QueryEvents(sessionID)
	found := false
	for _, e := range events {
		if e.Type == "warning:supervisor_exited_early" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning:supervisor_exited_early event")
	}
}

// TestCheckSupervisorExitedEarlyNoWarningWhenAllDone verifies that no warning
// is logged when all specialists have already completed.
func TestCheckSupervisorExitedEarlyNoWarningWhenAllDone(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "worker")
	_ = d.store.UpdateAgentRunStatus(sessionID, "worker", "complete")

	d.checkSupervisorExitedEarly(sessionID)

	events, _ := d.store.QueryEvents(sessionID)
	for _, e := range events {
		if e.Type == "warning:supervisor_exited_early" {
			t.Fatal("did not expect warning:supervisor_exited_early when all specialists are done")
		}
	}
}

// --- Tests for processAgentStatusEvent ---

// TestProcessAgentStatusIncompleteUpdatesAgent verifies that an
// agent_status:incomplete event updates the agent's status and logs an
// agent_escalated event.
func TestProcessAgentStatusIncompleteUpdatesAgent(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	data, _ := json.Marshal(map[string]any{
		"agent":  "worker",
		"status": "incomplete",
		"detail": "stuck in loop",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", string(data))

	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "incomplete" {
		t.Fatalf("expected agent status=incomplete, got %q", run.Status)
	}

	events, _ := d.store.QueryEvents(sessionID)
	found := false
	for _, e := range events {
		if e.Type == "agent_escalated" && strings.Contains(e.Data, "worker") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected agent_escalated event for worker")
	}
}

// TestProcessAgentStatusIncompleteSupervisorEscalatesSession verifies that when
// the supervisor reports incomplete, the session transitions to needs_human_review.
func TestProcessAgentStatusIncompleteSupervisorEscalatesSession(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "idle timeout",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", string(data))

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "needs_human_review" {
		t.Fatalf("expected session status=needs_human_review, got %q", sess.Status)
	}
}

// TestProcessAgentStatusIncompleteSpecialistNotifiesSupervisor verifies that
// when a specialist (non-supervisor) reports incomplete, the daemon sends an
// urgent broker message to the supervisor. Without this nudge the supervisor
// would sleep on its idle timer and escalate the whole run without ever
// attempting to respawn the dead peer or take corrective action.
func TestProcessAgentStatusIncompleteSpecialistNotifiesSupervisor(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "web-1", "supervisor")

	data, _ := json.Marshal(map[string]any{
		"agent":  "web-1",
		"status": "incomplete",
		"detail": "ran out of retries after seccomp kill",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", string(data))

	// Specialist status must be updated.
	run, err := d.store.GetAgentRun(sessionID, "web-1")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "incomplete" {
		t.Fatalf("expected agent status=incomplete, got %q", run.Status)
	}

	// Supervisor must have an urgent pending message from the peer.
	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.SenderID != "web-1" || m.RecipientID != "supervisor" {
			continue
		}
		if !m.Urgent {
			continue
		}
		if !strings.Contains(m.Content, "incomplete") {
			continue
		}
		if !strings.Contains(m.Content, "seccomp") {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("expected urgent incomplete-notice from web-1 to supervisor containing detail, got %#v", msgs)
	}

	// Session itself must not be escalated — only supervisor reporting incomplete
	// should tear the run down.
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status == "needs_human_review" {
		t.Fatalf("specialist-incomplete must not escalate session; got %q", sess.Status)
	}
}

// TestProcessAgentStatusIncompleteSupervisorSendsNoSelfMessage verifies that
// when the supervisor itself reports incomplete, no self-directed broker
// message is generated (the session-escalation path handles it).
func TestProcessAgentStatusIncompleteSupervisorSendsNoSelfMessage(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "idle timeout",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", string(data))

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	for _, m := range msgs {
		if m.SenderID == "supervisor" && m.RecipientID == "supervisor" {
			t.Fatalf("supervisor reporting incomplete must not generate self-message: %#v", m)
		}
	}
}

// TestProcessAgentStatusMalformedJSONDoesNotPanic verifies that malformed event
// data does not panic and is handled gracefully.
func TestProcessAgentStatusMalformedJSONDoesNotPanic(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker")

	// Should not panic — malformed JSON is logged and returned early.
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", "{invalid json")
}

// TestProcessAgentStatusMissingAgentFieldDoesNotPanic verifies that an event
// without the agent field is handled gracefully.
func TestProcessAgentStatusMissingAgentFieldDoesNotPanic(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker")

	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", `{"detail":"no agent"}`)
}

// --- Tests for handleBridgeFinished status overwrite guard ---

// TestBridgeFinishedDoesNotOverwriteIncomplete verifies that when an agent
// has already been marked incomplete, bridge:finished does not overwrite
// the status to "complete".
func TestBridgeFinishedDoesNotOverwriteIncomplete(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	// Simulate the idle timeout sequence: agent_status:incomplete then bridge:finished.
	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "idle timeout",
	})
	rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/events", logEventRequest{
		Type: "agent_status:incomplete",
		Data: string(data),
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("agent_status:incomplete: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Now post bridge:finished — should NOT overwrite incomplete.
	rr = postBridgeEvent(t, d, sessionID, "bridge:finished", map[string]any{
		"agent":  "supervisor",
		"reason": "idle_timeout",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("bridge:finished: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	run, err := d.store.GetAgentRun(sessionID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "incomplete" {
		t.Fatalf("expected agent status=incomplete (not overwritten by bridge:finished), got %q", run.Status)
	}
}
