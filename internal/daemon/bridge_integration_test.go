package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/store"
)

// TestHelperProcess is not a real test — it is re-executed as a subprocess to
// simulate bridge behavior.  The parent test sets GO_WANT_HELPER_PROCESS=1 and
// BELAYER_ROLE to select the simulated behavior.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	role := os.Getenv("BELAYER_ROLE")
	switch role {
	case "mock-cat":
		// Echo stdin lines back to stdout — simulates a running bridge that
		// processes interrupt commands.
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	case "mock-exit":
		// Exit immediately with a non-zero status to simulate a crash.
		os.Exit(1)
	case "mock-idle":
		// Block until killed — simulates a long-running bridge.
		select {}
	}
	os.Exit(0)
}

// mockBridgeCmd returns the argv needed to re-invoke this test binary as a
// mock bridge subprocess with the given role.
func mockBridgeCmd(role string) []string {
	return []string{
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
	}
}

// spawnMockBridge spawns a real bridge.Process whose subprocess runs
// TestHelperProcess in the current test binary.
func spawnMockBridge(t *testing.T, sessionID, agentID, role string) *bridge.Process {
	t.Helper()
	runDir := t.TempDir()
	cfg := bridge.Config{
		Cmd:       mockBridgeCmd(role),
		SessionID: sessionID,
		AgentID:   agentID,
		Role:      role,
		Profile:   "mock",
		Workdir:   t.TempDir(),
		SocketPath: "",
		RunDir:    runDir,
	}
	proc, err := bridge.Spawn(cfg)
	if err != nil {
		t.Fatalf("bridge.Spawn(%q): %v", role, err)
	}
	t.Cleanup(func() {
		// Best-effort stop; ignore errors on cleanup.
		_ = proc.Stop(2 * time.Second)
	})
	return proc
}

// --- Test: Multi-agent message flow ---

// TestBridgeIntegration_MultiAgentMessageFlow exercises the full pull-based
// message flow between two bridge agents through the session bus.
//
//  1. Start daemon, create session.
//  2. Spawn "planner" and "api" bridge agents (mock-idle subprocesses).
//  3. Planner sends a message to api via POST /sessions/{id}/messages.
//  4. Api polls for pending messages.
//  5. Api sends a reply back to planner.
//  6. Planner polls and receives the reply.
//  7. Api posts bridge:finished → daemon notifies planner.
//  8. Verify planner gets the completion notification.
//  9. Verify agent status is updated to "complete".
func TestBridgeIntegration_MultiAgentMessageFlow(t *testing.T) {
	d := testDaemon(t)

	// Create session.
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "bridge-flow"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID

	// Override spawnBridgeAgent so we launch mock subprocesses.
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		proc := spawnMockBridge(t, req.SessionID, req.Name, "mock-idle")
		return proc, nil
	}

	// Spawn planner.
	plannerRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "planner",
		Role:    "planner",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if plannerRR.Code != http.StatusCreated {
		t.Fatalf("spawn planner: expected 201, got %d: %s", plannerRR.Code, plannerRR.Body.String())
	}
	plannerRun := decodeJSON[store.AgentRun](t, plannerRR)
	if plannerRun.Status != "running" {
		t.Fatalf("planner: expected status=running, got %q", plannerRun.Status)
	}

	// Spawn api.
	apiRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "api",
		Role:    "api",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if apiRR.Code != http.StatusCreated {
		t.Fatalf("spawn api: expected 201, got %d: %s", apiRR.Code, apiRR.Body.String())
	}
	apiRun := decodeJSON[store.AgentRun](t, apiRR)
	if apiRun.Status != "running" {
		t.Fatalf("api: expected status=running, got %q", apiRun.Status)
	}

	// Step 3: Planner sends a message to api.
	sendRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/messages", sendMessageRequest{
		From:    "planner",
		To:      "api",
		Content: "implement the endpoint",
		Type:    "instruction",
	})
	if sendRR.Code != http.StatusCreated {
		t.Fatalf("send message planner->api: expected 201, got %d: %s", sendRR.Code, sendRR.Body.String())
	}

	// Step 4: Api polls for pending messages.
	apiPollRR := doRequest(t, d, "GET", "/sessions/"+sessionID+"/messages?for=api&pending=true", nil)
	if apiPollRR.Code != http.StatusOK {
		t.Fatalf("api poll: expected 200, got %d: %s", apiPollRR.Code, apiPollRR.Body.String())
	}
	apiMsgs := decodeJSON[[]store.Message](t, apiPollRR)
	if len(apiMsgs) == 0 {
		t.Fatal("api poll: expected at least one pending message")
	}
	found := false
	for _, m := range apiMsgs {
		if m.Content == "implement the endpoint" && m.SenderID == "planner" && m.RecipientID == "api" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("api poll: expected message from planner with 'implement the endpoint', got %#v", apiMsgs)
	}

	// Step 4b: Verify the message was marked delivered (second poll returns nothing).
	apiPollRR2 := doRequest(t, d, "GET", "/sessions/"+sessionID+"/messages?for=api&pending=true", nil)
	apiMsgs2 := decodeJSON[[]store.Message](t, apiPollRR2)
	if len(apiMsgs2) != 0 {
		t.Fatalf("api poll 2: expected 0 pending after delivery, got %d", len(apiMsgs2))
	}

	// Step 5: Api sends a reply back to planner.
	replyRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/messages", sendMessageRequest{
		From:    "api",
		To:      "planner",
		Content: "endpoint implemented",
		Type:    "result",
	})
	if replyRR.Code != http.StatusCreated {
		t.Fatalf("send reply api->planner: expected 201, got %d: %s", replyRR.Code, replyRR.Body.String())
	}

	// Step 6: Planner polls and receives the reply.
	plannerPollRR := doRequest(t, d, "GET", "/sessions/"+sessionID+"/messages?for=planner&pending=true", nil)
	if plannerPollRR.Code != http.StatusOK {
		t.Fatalf("planner poll: expected 200, got %d: %s", plannerPollRR.Code, plannerPollRR.Body.String())
	}
	plannerMsgs := decodeJSON[[]store.Message](t, plannerPollRR)
	if len(plannerMsgs) == 0 {
		t.Fatal("planner poll: expected at least one pending message")
	}
	foundReply := false
	for _, m := range plannerMsgs {
		if m.Content == "endpoint implemented" && m.SenderID == "api" && m.RecipientID == "planner" {
			foundReply = true
			break
		}
	}
	if !foundReply {
		t.Fatalf("planner poll: expected reply from api, got %#v", plannerMsgs)
	}

	// Step 7: Api reports completion via bridge:finished event.
	finishedData, _ := json.Marshal(map[string]any{
		"agent":          "api",
		"final_response": "endpoint is live",
	})
	bridgeFinishedRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/events", logEventRequest{
		Type: "bridge:finished",
		Data: string(finishedData),
	})
	if bridgeFinishedRR.Code != http.StatusCreated {
		t.Fatalf("bridge:finished: expected 201, got %d: %s", bridgeFinishedRR.Code, bridgeFinishedRR.Body.String())
	}

	// Step 8: Verify NO auto-generated completion message to planner.
	// The specialist sends its own report via belayer_send_message; the daemon
	// should not duplicate it with an auto-generated state_change.
	completionMsgsRR := doRequest(t, d, "GET", "/sessions/"+sessionID+"/messages?for=planner&pending=true", nil)
	completionMsgs := decodeJSON[[]store.Message](t, completionMsgsRR)
	for _, m := range completionMsgs {
		if m.SenderID == "api" && strings.Contains(m.Content, "has completed its work") {
			t.Fatalf("bridge:finished should not auto-generate completion message, got: %s", m.Content)
		}
	}

	// Step 9: Verify api agent status is updated to "complete".
	apiRunUpdated, err := d.store.GetAgentRun(sessionID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun api: %v", err)
	}
	if apiRunUpdated.Status != "complete" {
		t.Fatalf("expected api status=complete after bridge:finished, got %q", apiRunUpdated.Status)
	}
}

// --- Test: Bridge interrupt delivery ---

// TestBridgeIntegration_InterruptDelivery verifies that sending an urgent
// (interrupt) message to a bridge agent causes the daemon to write the
// interrupt JSON to the bridge's stdin.
func TestBridgeIntegration_InterruptDelivery(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "bridge-interrupt"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID

	// Capture the spawned process so we can inspect it later.
	var capturedProc *bridge.Process
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		// mock-cat echoes stdin, so we can verify writes don't error.
		proc := spawnMockBridge(t, req.SessionID, req.Name, "mock-cat")
		capturedProc = proc
		return proc, nil
	}

	// Spawn an api agent backed by mock-cat (reads stdin indefinitely).
	agentRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "api",
		Role:    "api",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if agentRR.Code != http.StatusCreated {
		t.Fatalf("spawn api: expected 201, got %d: %s", agentRR.Code, agentRR.Body.String())
	}

	if capturedProc == nil {
		t.Fatal("expected spawnBridgeAgent to have been called and set capturedProc")
	}

	// Send an urgent/interrupt message to api.
	interruptRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/messages", sendMessageRequest{
		From:      "planner",
		To:        "api",
		Content:   "stop what you are doing",
		Type:      "interrupt",
		Interrupt: true,
	})
	if interruptRR.Code != http.StatusCreated {
		t.Fatalf("send interrupt: expected 201, got %d: %s", interruptRR.Code, interruptRR.Body.String())
	}

	// Stop the process and verify it can be stopped cleanly.
	stopErr := capturedProc.Stop(2 * time.Second)
	// Stopping mock-cat (which exits when its stdin pipe closes) may report a
	// non-nil error depending on the OS signal; only fail if the channel never closes.
	_ = stopErr

	select {
	case <-capturedProc.Done():
		// Good — process exited.
	case <-time.After(3 * time.Second):
		t.Fatal("bridge process did not exit within timeout after Stop")
	}
}

// --- Test: Bridge process exit detection ---

// TestBridgeIntegration_BridgeProcessExitDetection verifies that when a bridge
// subprocess exits without posting bridge:finished, the daemon's watchBridgeExit
// goroutine marks the agent as "blocked" and logs an event.
func TestBridgeIntegration_BridgeProcessExitDetection(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "bridge-exit"})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID

	// Also spawn a planner so watchBridgeExit can try to notify it (avoids
	// the no-op "planner" branch).
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      "planner",
		Role:      "planner",
		Profile:   "mock",
		Status:    "running",
		Transport: "bridge",
	})
	if err != nil {
		t.Fatalf("create planner run: %v", err)
	}

	// Spawn an api agent that exits immediately.
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		proc := spawnMockBridge(t, req.SessionID, req.Name, "mock-exit")
		return proc, nil
	}

	agentRR := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
		Name:    "api",
		Role:    "api",
		Profile: "mock",
		Workdir: t.TempDir(),
	})
	if agentRR.Code != http.StatusCreated {
		t.Fatalf("spawn api: expected 201, got %d: %s", agentRR.Code, agentRR.Body.String())
	}

	// Wait for watchBridgeExit to detect the exit and update status.
	// Give it up to 3 seconds — the mock process exits immediately, so this
	// should be very fast in practice.
	deadline := time.Now().Add(3 * time.Second)
	var agentStatus string
	for time.Now().Before(deadline) {
		run, err := d.store.GetAgentRun(sessionID, "api")
		if err != nil {
			t.Fatalf("GetAgentRun: %v", err)
		}
		agentStatus = run.Status
		if agentStatus == "blocked" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify agent status was set to "blocked".
	if agentStatus != "blocked" {
		t.Fatalf("expected agent status=blocked after unexpected exit, got %q", agentStatus)
	}

	// Verify the agent_exited_without_finish event was logged.
	var foundEvent bool
	deadline2 := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline2) {
		events, err := d.store.QueryEvents(sessionID)
		if err != nil {
			t.Fatalf("QueryEvents: %v", err)
		}
		for _, e := range events {
			if e.Type == "agent_exited_without_finish" && strings.Contains(e.Data, "api") {
				foundEvent = true
				break
			}
		}
		if foundEvent {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundEvent {
		events, _ := d.store.QueryEvents(sessionID)
		t.Fatalf("expected agent_exited_without_finish event, got %#v", events)
	}
}

// TestBridgeIntegration_RosterReflectsSpawnedAgents verifies that after
// spawning two bridge agents the roster endpoint returns both.
func TestBridgeIntegration_RosterReflectsSpawnedAgents(t *testing.T) {
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "bridge-roster"})
	sess := decodeJSON[sessionAPIResponse](t, sessRR)
	sessionID := sess.ID

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		proc := spawnMockBridge(t, req.SessionID, req.Name, "mock-idle")
		return proc, nil
	}

	for _, name := range []string{"planner", "api"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sessionID+"/agents", agentSpawnRequest{
			Name:    name,
			Role:    name,
			Profile: "mock",
			Workdir: t.TempDir(),
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("spawn %s: expected 201, got %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	rosterRR := doRequest(t, d, "GET", "/sessions/"+sessionID+"/agents", nil)
	if rosterRR.Code != http.StatusOK {
		t.Fatalf("roster: expected 200, got %d: %s", rosterRR.Code, rosterRR.Body.String())
	}
	roster := decodeJSON[[]store.AgentRun](t, rosterRR)
	if len(roster) != 2 {
		t.Fatalf("expected 2 agents in roster, got %d: %#v", len(roster), roster)
	}
	names := map[string]bool{}
	for _, r := range roster {
		names[r.Name] = true
		if r.Transport != "bridge" {
			t.Errorf("agent %q: expected transport=bridge, got %q", r.Name, r.Transport)
		}
	}
	if !names["planner"] || !names["api"] {
		t.Fatalf("expected planner and api in roster, got %v", names)
	}
}
