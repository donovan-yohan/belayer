package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestBridgeBudgetExhaustedSetsExitedAndNotifiesSupervisor(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker", "supervisor")

	rr := postBridgeEvent(t, d, sessionID, "bridge:budget_exhausted", map[string]any{
		"agent":        "worker",
		"turns_used":   12,
		"max_turns":    12,
		"last_message": "still trying to untangle the migration",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "exited" {
		t.Fatalf("expected status=exited, got %q", run.Status)
	}

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	var found bool
	for _, msg := range msgs {
		if msg.RecipientID == "supervisor" &&
			strings.Contains(msg.Content, "exhausted its turn budget") &&
			strings.Contains(msg.Content, "12/12") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected budget-exhausted mail to supervisor, got %#v", msgs)
	}

	listRR := doRequest(t, d, "GET", "/sessions/"+sessionID+"/agents", nil)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list agents: expected 200, got %d: %s", listRR.Code, listRR.Body.String())
	}
	var decoded []map[string]any
	if err := json.NewDecoder(listRR.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode list agents: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 agent rows, got %d", len(decoded))
	}
	var sawWorker bool
	for _, row := range decoded {
		if row["name"] == "worker" {
			sawWorker = true
			if row["outcome"] != "budget_exhausted" {
				t.Fatalf("worker outcome = %v, want budget_exhausted", row["outcome"])
			}
		}
	}
	if !sawWorker {
		t.Fatalf("worker row missing from agents list: %#v", decoded)
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

// TestBridgeToolStartedDestructiveDetection verifies that a bridge:tool_started
// event with a destructive terminal command increments the destructive_actions
// counter on the agent run and records the command snippet.
func TestBridgeToolStartedDestructiveDetection(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "backend-dev")

	// Non-destructive terminal call — should not increment counter.
	rr := postBridgeEvent(t, d, sessionID, "bridge:tool_started", map[string]any{
		"agent":         "backend-dev",
		"tool":          "terminal",
		"input_preview": "ls /workspace",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("non-destructive: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run, err := d.store.GetAgentRun(sessionID, "backend-dev")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.DestructiveActions != 0 {
		t.Fatalf("expected 0 destructive actions after non-destructive cmd, got %d", run.DestructiveActions)
	}

	// Destructive terminal call — rm -rf
	rr = postBridgeEvent(t, d, sessionID, "bridge:tool_started", map[string]any{
		"agent":         "backend-dev",
		"tool":          "terminal",
		"input_preview": "rm -rf /workspace/.belayer",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("destructive rm -rf: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run, err = d.store.GetAgentRun(sessionID, "backend-dev")
	if err != nil {
		t.Fatalf("GetAgentRun after destructive: %v", err)
	}
	if run.DestructiveActions != 1 {
		t.Fatalf("expected 1 destructive action after rm -rf, got %d", run.DestructiveActions)
	}
	if run.LastDestructiveCmd == "" {
		t.Fatal("expected LastDestructiveCmd to be set")
	}

	// Second destructive call — counter should increment to 2.
	rr = postBridgeEvent(t, d, sessionID, "bridge:tool_started", map[string]any{
		"agent":         "backend-dev",
		"tool":          "bash",
		"input_preview": "git reset --hard HEAD",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("destructive git reset: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run, err = d.store.GetAgentRun(sessionID, "backend-dev")
	if err != nil {
		t.Fatalf("GetAgentRun after second destructive: %v", err)
	}
	if run.DestructiveActions != 2 {
		t.Fatalf("expected 2 destructive actions, got %d", run.DestructiveActions)
	}

	// Non-terminal tool (Write) — destructive pattern in input_preview should be ignored.
	rr = postBridgeEvent(t, d, sessionID, "bridge:tool_started", map[string]any{
		"agent":         "backend-dev",
		"tool":          "write_file",
		"input_preview": "rm -rf /would-be-bad-if-executed",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("non-terminal tool: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run, err = d.store.GetAgentRun(sessionID, "backend-dev")
	if err != nil {
		t.Fatalf("GetAgentRun after non-terminal: %v", err)
	}
	if run.DestructiveActions != 2 {
		t.Fatalf("expected counter still 2 for non-terminal tool, got %d", run.DestructiveActions)
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
	_ = d.store.UpdateSessionStatus(sessionID, "running")

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

	// Session must remain "running" — supervisor is still active and should
	// decide whether to respawn. Any terminal transition (needs_human_review,
	// stalled, complete) here means we incorrectly acted on a specialist exit.
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "running" {
		t.Fatalf("specialist-incomplete must leave session running while supervisor is active; got %q", sess.Status)
	}
}

// TestProcessAgentStatusIncompleteSideUsesSystemSender verifies that a side
// agent cannot populate the supervisor inbox with a side-surface sender ID.
func TestProcessAgentStatusIncompleteSideUsesSystemSender(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor")
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      "pm",
		Role:      "pm",
		Kind:      "side",
		Profile:   "default",
		Status:    "running",
		Transport: "tmux",
	})
	if err != nil {
		t.Fatalf("CreateAgentRun side: %v", err)
	}
	_ = d.store.UpdateSessionStatus(sessionID, "running")

	data, _ := json.Marshal(map[string]any{
		"agent":  "pm",
		"status": "incomplete",
		"detail": "side surface",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:incomplete", string(data))

	msgs, err := d.store.PendingMessages(sessionID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.RecipientID == "supervisor" && m.Content != "" && strings.Contains(m.Content, "side surface") {
			if m.SenderID != "system" {
				t.Fatalf("expected side-surface notification to be system-sent, got sender=%q msg=%#v", m.SenderID, m)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected supervisor notification from side agent, got %#v", msgs)
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

// TestProcessAgentStatusDoneMarksIdleAndUpdatesRoster verifies that the daemon
// reconciles agent_status:done immediately so roster reads do not lag behind
// the bridge event stream.
func TestProcessAgentStatusDoneMarksIdleAndUpdatesRoster(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "worker")

	data, _ := json.Marshal(map[string]any{
		"agent":  "worker",
		"status": "done",
	})
	d.processAgentStatusEvent(sessionID, "agent_status:done", string(data))

	run, err := d.store.GetAgentRun(sessionID, "worker")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "idle" {
		t.Fatalf("expected agent_status:done to reconcile to idle, got %q", run.Status)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessionID+"/agents", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected roster fetch to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	agents := decodeJSON[[]store.AgentRun](t, rr)
	if len(agents) != 1 || agents[0].Status != "idle" {
		t.Fatalf("expected roster to reflect idle status immediately, got %#v", agents)
	}

	events, err := d.store.QueryEvents(sessionID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == "agent_status_done" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected agent_status_done event to be logged")
	}
}

// --- Tests for handleBridgeFinished status overwrite guard ---

// --- Tests for resolvePersistenceStrategy ---

// setupSessionWithWorkspace creates a session whose WorkspaceDir points at a
// fresh temp dir, so tests can seed a .belayer/config.yaml that the daemon's
// config readers will find.
func setupSessionWithWorkspace(t *testing.T, d *Daemon, configYAML string) string {
	t.Helper()
	ws := t.TempDir()
	if configYAML != "" {
		if err := os.MkdirAll(filepath.Join(ws, ".belayer"), 0o755); err != nil {
			t.Fatalf("mkdir .belayer: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ws, ".belayer", "config.yaml"), []byte(configYAML), 0o644); err != nil {
			t.Fatalf("write config.yaml: %v", err)
		}
	}
	sessID, err := d.store.CreateSession(store.Session{Name: "persistence-test", WorkspaceDir: ws})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sessID
}

// TestResolvePersistenceStrategy_Override verifies that a run_initiated event
// carrying persistence_strategy wins over any config file on disk.
func TestResolvePersistenceStrategy_Override(t *testing.T) {
	d := testDaemon(t)
	// Config file says one thing; the override should win.
	sessID := setupSessionWithWorkspace(t, d, "persistence_strategy:\n  - from-config\n")

	overrideData, _ := json.Marshal(map[string]any{
		"persistence_strategy": []string{"override-step-1", "override-step-2"},
	})
	if err := d.store.LogEvent(store.SessionEvent{
		SessionID: sessID,
		Type:      "run_initiated",
		Data:      string(overrideData),
	}); err != nil {
		t.Fatalf("log run_initiated: %v", err)
	}

	got, source := d.resolvePersistenceStrategy(sessID)
	if source != "override" {
		t.Fatalf("expected source=override, got %q", source)
	}
	if len(got) != 2 || got[0] != "override-step-1" || got[1] != "override-step-2" {
		t.Fatalf("expected override steps, got %v", got)
	}
}

// TestResolvePersistenceStrategy_Config verifies that the config file is used
// when no run_initiated override is present.
func TestResolvePersistenceStrategy_Config(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d,
		"persistence_strategy:\n  - commit-work\n  - push-branch\n")

	got, source := d.resolvePersistenceStrategy(sessID)
	if source != "config" {
		t.Fatalf("expected source=config, got %q", source)
	}
	if len(got) != 2 || got[0] != "commit-work" || got[1] != "push-branch" {
		t.Fatalf("expected config steps, got %v", got)
	}
}

// TestResolvePersistenceStrategy_None verifies that an absent config block
// produces source=none and an empty list (no false signal to the intercept).
func TestResolvePersistenceStrategy_None(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d, "# no persistence_strategy here\n")

	got, source := d.resolvePersistenceStrategy(sessID)
	if source != "none" {
		t.Fatalf("expected source=none, got %q", source)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %v", got)
	}
}

// TestResolvePersistenceStrategy_EmptyOverrideFallsThroughToConfig verifies
// that an override payload with an empty persistence_strategy array does NOT
// shadow the config file — mirrors the --persistence-strategy flag normalization
// in run.go, where a blank value falls through to the project config.
func TestResolvePersistenceStrategy_EmptyOverrideFallsThroughToConfig(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d,
		"persistence_strategy:\n  - config-step\n")

	overrideData, _ := json.Marshal(map[string]any{
		"persistence_strategy": []string{},
	})
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessID,
		Type:      "run_initiated",
		Data:      string(overrideData),
	})

	got, source := d.resolvePersistenceStrategy(sessID)
	if source != "config" {
		t.Fatalf("expected fall-through to config, got source=%q", source)
	}
	if len(got) != 1 || got[0] != "config-step" {
		t.Fatalf("expected config step, got %v", got)
	}
}

// --- Tests for the persistence intercept on agent_status:incomplete ---

// TestSupervisorIncompleteRepromptedWhenNoPersistenceArtifact verifies that
// when the supervisor reports incomplete and a persistence_strategy is
// configured but no persistence-notes artifact exists, the daemon reprompts
// the supervisor instead of escalating the session.
func TestSupervisorIncompleteRepromptedWhenNoPersistenceArtifact(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d,
		"persistence_strategy:\n  - commit-and-push\n  - register-persistence-notes\n")
	_ = d.store.UpdateSessionStatus(sessID, "running")
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID, Name: "supervisor", Role: "supervisor",
		Profile: "default", Status: "running", Transport: "bridge",
	}); err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "stuck on DB migration",
	})
	d.processAgentStatusEvent(sessID, "agent_status:incomplete", string(data))

	// Session must NOT escalate — the supervisor should be reprompted and
	// kept alive for another pass.
	sess, err := d.store.GetSession(sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status == "needs_human_review" {
		t.Fatalf("session escalated despite persistence reprompt being applicable")
	}

	// Supervisor row must be flipped back to running so downstream checks
	// don't treat it as terminal.
	run, err := d.store.GetAgentRun(sessID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("expected supervisor status=running after reprompt, got %q", run.Status)
	}

	// Reprompt event must be logged so post-mortems can see the gate fired.
	events, _ := d.store.QueryEvents(sessID)
	var foundReprompt bool
	for _, e := range events {
		if e.Type == "persistence_reprompt" {
			foundReprompt = true
			if !strings.Contains(e.Data, "commit-and-push") {
				t.Errorf("expected reprompt event to include strategy steps, got: %s", e.Data)
			}
		}
	}
	if !foundReprompt {
		t.Fatalf("expected persistence_reprompt event")
	}

	// Supervisor must receive an urgent message listing the strategy steps.
	msgs, err := d.store.PendingMessages(sessID, "supervisor", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	var foundMsg bool
	for _, m := range msgs {
		if m.RecipientID == "supervisor" && m.Urgent && strings.Contains(m.Content, "persistence_strategy") {
			foundMsg = true
		}
	}
	if !foundMsg {
		t.Fatalf("expected urgent reprompt message to supervisor, got %#v", msgs)
	}

	sessMeta, err := d.store.GetSession(sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	handoffPath := filepath.Join(sessMeta.WorkspaceDir, ".belayer", "runs", sessID, "handoff.md")
	if _, err := os.Stat(handoffPath); err != nil {
		t.Fatalf("expected handoff artifact at %s: %v", handoffPath, err)
	}
	artifacts, err := d.store.ListArtifacts(sessID)
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	var foundHandoff bool
	for _, artifact := range artifacts {
		if artifact.Kind == "handoff" && artifact.Path == ".belayer/runs/"+sessID+"/handoff.md" {
			foundHandoff = true
			break
		}
	}
	if !foundHandoff {
		t.Fatalf("expected handoff artifact registration, got %#v", artifacts)
	}
}

// TestSupervisorIncompleteAcceptedWhenPersistenceArtifactPresent verifies
// that an incomplete is accepted and the session escalates normally when the
// supervisor has already registered a persistence-notes artifact.
func TestSupervisorIncompleteAcceptedWhenPersistenceArtifactPresent(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d,
		"persistence_strategy:\n  - commit-and-push\n")
	_ = d.store.UpdateSessionStatus(sessID, "running")
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID, Name: "supervisor", Role: "supervisor",
		Profile: "default", Status: "running", Transport: "bridge",
	}); err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	// Supervisor already registered persistence-notes — the gate should
	// let this incomplete through.
	if _, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sessID,
		Kind:      "persistence-notes",
		Path:      "(inline)",
		Producer:  "supervisor",
		Summary:   "pushed branch incomplete/foo, opened draft PR #123",
	}); err != nil {
		t.Fatalf("create persistence-notes artifact: %v", err)
	}

	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "blocked on upstream API",
	})
	d.processAgentStatusEvent(sessID, "agent_status:incomplete", string(data))

	sess, err := d.store.GetSession(sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "needs_human_review" {
		t.Fatalf("expected session escalated to needs_human_review, got %q", sess.Status)
	}
}

// TestSupervisorIncompleteEscalatesWhenPersistenceStrategyEmpty verifies that
// the intercept is a no-op when no persistence_strategy is declared —
// preserves the existing escalation behavior for projects that opt out.
func TestSupervisorIncompleteEscalatesWhenPersistenceStrategyEmpty(t *testing.T) {
	d := testDaemon(t)
	// No config file, no override — persistence_strategy is empty.
	sessID := setupSessionWithWorkspace(t, d, "")
	_ = d.store.UpdateSessionStatus(sessID, "running")
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID, Name: "supervisor", Role: "supervisor",
		Profile: "default", Status: "running", Transport: "bridge",
	}); err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	data, _ := json.Marshal(map[string]any{
		"agent":  "supervisor",
		"status": "incomplete",
		"detail": "done bailing",
	})
	d.processAgentStatusEvent(sessID, "agent_status:incomplete", string(data))

	sess, err := d.store.GetSession(sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "needs_human_review" {
		t.Fatalf("expected session escalated to needs_human_review, got %q", sess.Status)
	}
}

// TestSupervisorIncompleteAcceptedAfterMaxReprompts verifies that after the
// reprompt limit is reached, the next incomplete escalates normally even
// without a persistence-notes artifact. Prevents a livelock when the
// supervisor genuinely cannot persist (no network, stuck credentials).
func TestSupervisorIncompleteAcceptedAfterMaxReprompts(t *testing.T) {
	d := testDaemon(t)
	sessID := setupSessionWithWorkspace(t, d,
		"persistence_strategy:\n  - commit-and-push\n")
	_ = d.store.UpdateSessionStatus(sessID, "running")
	if _, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID, Name: "supervisor", Role: "supervisor",
		Profile: "default", Status: "running", Transport: "bridge",
	}); err != nil {
		t.Fatalf("create supervisor: %v", err)
	}

	// First incomplete: should be intercepted (reprompt #1).
	data, _ := json.Marshal(map[string]any{
		"agent": "supervisor", "status": "incomplete", "detail": "first try",
	})
	d.processAgentStatusEvent(sessID, "agent_status:incomplete", string(data))

	sess, _ := d.store.GetSession(sessID)
	if sess.Status == "needs_human_review" {
		t.Fatalf("expected first incomplete to be intercepted, got escalation")
	}

	// Second incomplete: limit reached, should escalate normally.
	data2, _ := json.Marshal(map[string]any{
		"agent": "supervisor", "status": "incomplete", "detail": "second try",
	})
	d.processAgentStatusEvent(sessID, "agent_status:incomplete", string(data2))

	sess, _ = d.store.GetSession(sessID)
	if sess.Status != "needs_human_review" {
		t.Fatalf("expected second incomplete to escalate after reprompt limit, got %q", sess.Status)
	}

	// Bypass event must be logged so operators see the gate was skipped.
	events, _ := d.store.QueryEvents(sessID)
	var foundBypass bool
	for _, e := range events {
		if e.Type == "persistence_gate_bypassed" {
			foundBypass = true
		}
	}
	if !foundBypass {
		t.Fatalf("expected persistence_gate_bypassed event after reprompt limit")
	}
}

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

// TestRosterJSONShapeIsBridgeContract pins the wire shape of the
// /sessions/{id}/agents endpoint that hermes_bridge.__main__.active_peers
// reads. The bridge filters peers using lowercase JSON keys (`name`,
// `status`) and expects `status` to be a single lifecycle token (no
// `/<outcome>` suffix; the composite is CLI-display-only via
// rosterStatus()). If a future change capitalises the keys or fuses
// status+outcome into a composite string, the bridge's idle loop will
// silently classify every peer as terminal and trigger a false
// agent_status:incomplete after idle_timeout — see the case-mismatch
// regression that produced this test.
func TestRosterJSONShapeIsBridgeContract(t *testing.T) {
	d := testDaemon(t)
	sessionID := setupSessionWithAgents(t, d, "supervisor", "extend-api-dev")

	rr := doRequest(t, d, "GET", "/sessions/"+sessionID+"/agents", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("roster fetch: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Decode as a generic map so we observe the on-the-wire keys, not
	// Go's case-insensitive struct unmarshal.
	var rows []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 roster rows, got %d", len(rows))
	}

	for _, row := range rows {
		// 1. Lowercase keys must be present (bridge reads them directly).
		name, hasName := row["name"]
		if !hasName {
			t.Fatalf("roster row missing lowercase `name` key (bridge contract): %#v", row)
		}
		status, hasStatus := row["status"]
		if !hasStatus {
			t.Fatalf("roster row missing lowercase `status` key (bridge contract): %#v", row)
		}

		// 2. Capitalised keys must NOT appear (would mask presence of
		//    correct lowercase keys behind Go struct quirks).
		if _, ok := row["Name"]; ok {
			t.Fatalf("roster row exposes capitalised `Name` key — bridge expects lowercase only: %#v", row)
		}
		if _, ok := row["Status"]; ok {
			t.Fatalf("roster row exposes capitalised `Status` key — bridge expects lowercase only: %#v", row)
		}

		// 3. `status` must be a single lifecycle token, never a
		//    composite "<status>/<outcome>" string. The CLI builds
		//    that composite for display only (see rosterStatus in
		//    internal/cli/nightshift.go); the wire stays separate.
		statusStr, ok := status.(string)
		if !ok {
			t.Fatalf("`status` for %v is not a string: %T = %#v", name, status, status)
		}
		if strings.Contains(statusStr, "/") {
			t.Fatalf("`status` for %v is composite %q — must be a single lifecycle token; outcome belongs in the separate `outcome` field", name, statusStr)
		}

		// 4. `outcome` lives as its own field, not fused into `status`.
		if _, ok := row["outcome"]; !ok {
			t.Fatalf("roster row missing `outcome` field for %v (bridge contract — outcome must stay separate from status): %#v", name, row)
		}
	}
}
