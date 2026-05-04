package daemon

// Phase 3.C session-end sweep tests — verify that climb-scoped fork profiles
// that were NOT torn down by a final_response event are swept when the session
// transitions to a terminal status.
//
// These tests use the same fixture helpers as agents_profile_teardown_test.go
// (setupForkProfile, profileExists, setupAgentRunWithProfile) and the
// standard doRequest / testDaemon / decodeJSON helpers from daemon_test.go.

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// addAgentRunToSession adds an additional agent_run row to an existing session
// without creating a new session. Returns nothing; failures are fatal.
func addAgentRunToSession(t *testing.T, d *Daemon, sessionID, agentName, profileName string) {
	t.Helper()
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessionID,
		Name:      agentName,
		Role:      agentName,
		Profile:   profileName,
		Status:    "running",
		Transport: "tmux",
		Outcome:   "active",
	})
	if err != nil {
		t.Fatalf("create agent run %q in session %q: %v", agentName, sessionID, err)
	}
}

// markSessionTerminal sends a PATCH /sessions/{id} with the given terminal
// status and asserts a 200 OK response.
func markSessionTerminal(t *testing.T, d *Daemon, sessionID, status string) {
	t.Helper()
	rr := doRequest(t, d, "PATCH", "/sessions/"+sessionID, updateSessionRequest{Status: status})
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH session %q status=%q: %d %s", sessionID, status, rr.Code, rr.Body.String())
	}
}

// TestSessionEnd_AllClimbScopedForksTornDown verifies that when a session
// transitions to a terminal state all climb-scoped fork profiles for agents
// in that session are removed from disk, even if no final_response was emitted.
func TestSessionEnd_AllClimbScopedForksTornDown(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	// Set up fork profiles before creating the daemon so HERMES_HOME is set.
	profiles := []string{
		"blyr-local-agent1",
		"blyr-local-agent2",
		"blyr-local-agent3",
	}
	for _, p := range profiles {
		setupForkProfile(t, profilesRoot, p, "climb")
	}

	d := testDaemon(t)

	// Create session with supervisor (required by checkSessionStalled logic).
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "sweep-all-climb"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
	addAgentRunToSession(t, d, sess.ID, "agent1", profiles[0])
	addAgentRunToSession(t, d, sess.ID, "agent2", profiles[1])
	addAgentRunToSession(t, d, sess.ID, "agent3", profiles[2])

	// Transition to terminal without triggering final_response.
	markSessionTerminal(t, d, sess.ID, "complete")

	for _, p := range profiles {
		if profileExists(t, profilesRoot, p) {
			t.Errorf("climb-scoped profile %q should have been swept but still exists", p)
		}
	}
}

// TestSessionEnd_CragAndTalentScopedForksPreserved verifies that crag- and
// talent-scoped profiles survive the session-end sweep while climb-scoped
// ones are torn down.
func TestSessionEnd_CragAndTalentScopedForksPreserved(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const (
		climbProfile  = "blyr-local-climber"
		cragProfile   = "blyr-local-cragger"
		talentProfile = "blyr-local-talented"
	)

	setupForkProfile(t, profilesRoot, climbProfile, "climb")
	setupForkProfile(t, profilesRoot, cragProfile, "crag")
	setupForkProfile(t, profilesRoot, talentProfile, "talent")

	d := testDaemon(t)

	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "sweep-mixed-scopes"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
	addAgentRunToSession(t, d, sess.ID, "climber", climbProfile)
	addAgentRunToSession(t, d, sess.ID, "cragger", cragProfile)
	addAgentRunToSession(t, d, sess.ID, "talented", talentProfile)

	markSessionTerminal(t, d, sess.ID, "complete")

	if profileExists(t, profilesRoot, climbProfile) {
		t.Errorf("climb-scoped profile %q should have been swept but still exists", climbProfile)
	}
	if !profileExists(t, profilesRoot, cragProfile) {
		t.Errorf("crag-scoped profile %q should be preserved but was removed", cragProfile)
	}
	if !profileExists(t, profilesRoot, talentProfile) {
		t.Errorf("talent-scoped profile %q should be preserved but was removed", talentProfile)
	}
}

// TestSessionEnd_DefaultProfileAgentUntouched verifies that agents using the
// "default" profile (i.e. non-fork profiles) are not affected by the sweep.
// No teardown is attempted; the session-end transition completes without error.
func TestSessionEnd_DefaultProfileAgentUntouched(t *testing.T) {
	// Set HERMES_HOME so ProfilesRoot() resolves safely.
	setupBaseBelayerProfile(t)

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", "default")

	// Should not panic or error even though there is no "default" fork dir.
	markSessionTerminal(t, d, sessID, "complete")

	// Session row must still be terminal.
	rr := doRequest(t, d, "GET", "/sessions/"+sessID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET session: %d %s", rr.Code, rr.Body.String())
	}
	got := decodeJSON[sessionAPIResponse](t, rr)
	if got.Status != "complete" {
		t.Errorf("expected status=complete, got %q", got.Status)
	}
}

// TestSessionEnd_AfterPartialFinalResponse verifies that when some agents
// already had their profiles torn down via final_response (Phase 3.B), the
// session-end sweep cleans up the remaining agents without error.
func TestSessionEnd_AfterPartialFinalResponse(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const (
		profileAgent1 = "blyr-local-already-done"
		profileAgent2 = "blyr-local-still-running"
	)

	setupForkProfile(t, profilesRoot, profileAgent1, "climb")
	setupForkProfile(t, profilesRoot, profileAgent2, "climb")

	d := testDaemon(t)

	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "partial-cleanup"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
	addAgentRunToSession(t, d, sess.ID, "agent1", profileAgent1)
	addAgentRunToSession(t, d, sess.ID, "agent2", profileAgent2)

	// Simulate Phase 3.B tearing down agent1 via final_response.
	rrEvent := postBridgeEvent(t, d, sess.ID, "bridge:finished", map[string]any{
		"agent":          "agent1",
		"final_response": "done early",
	})
	if rrEvent.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished for agent1: %d %s", rrEvent.Code, rrEvent.Body.String())
	}

	// agent1's profile should already be gone.
	if profileExists(t, profilesRoot, profileAgent1) {
		t.Fatalf("agent1 profile should have been removed by final_response, but still exists")
	}

	// agent2's profile is still present. Transition session to terminal.
	markSessionTerminal(t, d, sess.ID, "complete")

	// Both profiles must be absent — agent1 already gone, agent2 swept now.
	if profileExists(t, profilesRoot, profileAgent1) {
		t.Errorf("agent1 profile should remain absent after session sweep")
	}
	if profileExists(t, profilesRoot, profileAgent2) {
		t.Errorf("agent2 profile should have been swept at session end but still exists")
	}
}

// TestSessionEnd_SweepErrorOneAgentDoesNotBlockOthers verifies that when one
// agent's profile teardown encounters a filesystem error, the sweep continues
// and cleans up the remaining agents' profiles.
func TestSessionEnd_SweepErrorOneAgentDoesNotBlockOthers(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const (
		profileBad  = "blyr-local-bad-perms"
		profileGood = "blyr-local-good"
	)

	// Create both profiles with climb scope.
	badDir := setupForkProfile(t, profilesRoot, profileBad, "climb")
	setupForkProfile(t, profilesRoot, profileGood, "climb")

	// Make the bad profile directory read-only so os.RemoveAll fails on it.
	if err := os.Chmod(badDir, 0o555); err != nil {
		t.Fatalf("chmod bad profile dir: %v", err)
	}
	// Restore permissions so t.TempDir can clean up.
	t.Cleanup(func() {
		_ = os.Chmod(badDir, 0o755)
	})

	d := testDaemon(t)

	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "sweep-partial-error"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
	addAgentRunToSession(t, d, sess.ID, "bad-agent", profileBad)
	addAgentRunToSession(t, d, sess.ID, "good-agent", profileGood)

	// Transition to terminal — sweep fires; bad-agent's teardown will fail.
	markSessionTerminal(t, d, sess.ID, "complete")

	// good-agent's profile must be gone despite bad-agent's error.
	if profileExists(t, profilesRoot, profileGood) {
		t.Errorf("good-agent profile %q should have been swept but still exists", profileGood)
	}

	// Session must have reached terminal status (sweep error did not propagate).
	rrGet := doRequest(t, d, "GET", "/sessions/"+sess.ID, nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET session: %d %s", rrGet.Code, rrGet.Body.String())
	}
	got := decodeJSON[sessionAPIResponse](t, rrGet)
	if got.Status != "complete" {
		t.Errorf("expected status=complete after partial sweep error, got %q", got.Status)
	}
}

// TestSessionEnd_SweepIsIdempotent verifies that calling the session-end
// transition a second time (re-PATCH to terminal) does not panic, returns no
// error, and leaves already-removed profiles absent.
func TestSessionEnd_SweepIsIdempotent(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-idempotent"
	setupForkProfile(t, profilesRoot, profileName, "climb")

	d := testDaemon(t)

	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "idempotent-sweep"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
	addAgentRunToSession(t, d, sess.ID, "worker", profileName)

	// First terminal transition — sweeps profile.
	markSessionTerminal(t, d, sess.ID, "complete")

	if profileExists(t, profilesRoot, profileName) {
		t.Fatalf("profile should be gone after first sweep")
	}

	// Second terminal transition — sweep runs again on already-cleaned state.
	// Must not panic or return an error.
	rr2 := doRequest(t, d, "PATCH", "/sessions/"+sess.ID, updateSessionRequest{Status: "complete"})
	if rr2.Code != http.StatusOK {
		t.Errorf("second PATCH to terminal: %d %s", rr2.Code, rr2.Body.String())
	}

	// Profile must still be absent.
	if profileExists(t, profilesRoot, profileName) {
		t.Errorf("profile should remain absent after idempotent second sweep")
	}
}

// TestSessionEnd_SweepWorksForDifferentTerminalStatuses verifies that the
// sweep is triggered for all terminal status values, not just "complete".
func TestSessionEnd_SweepWorksForDifferentTerminalStatuses(t *testing.T) {
	terminalStatuses := []string{"failed", "blocked", "cancelled", "needs_human_review", "stalled"}

	for _, status := range terminalStatuses {
		status := status // capture for subtest
		t.Run("status="+status, func(t *testing.T) {
			profilesRoot, _ := setupBaseBelayerProfile(t)

			profileName := "blyr-local-" + filepath.Base(t.TempDir())
			setupForkProfile(t, profilesRoot, profileName, "climb")

			d := testDaemon(t)

			rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "sweep-" + status})
			if rr.Code != http.StatusCreated {
				t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
			}
			sess := decodeJSON[sessionAPIResponse](t, rr)

			addAgentRunToSession(t, d, sess.ID, "supervisor", "default")
			addAgentRunToSession(t, d, sess.ID, "worker", profileName)

			markSessionTerminal(t, d, sess.ID, status)

			if profileExists(t, profilesRoot, profileName) {
				t.Errorf("status=%q: climb-scoped profile %q should have been swept but still exists", status, profileName)
			}
		})
	}
}
