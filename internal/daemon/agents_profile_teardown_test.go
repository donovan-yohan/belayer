package daemon

// Phase 3.B teardown tests — verify that climb-scoped fork profiles are removed
// on bridge:finished(final_response) and that crag/talent-scoped profiles survive.
//
// Fixture pattern: same as agents_profile_spawn_test.go and
// agents_profile_integration_test.go. The bridge subprocess is stubbed via
// d.spawnBridgeAgent so no Python process is started; the final_response event is
// injected directly via postBridgeEvent.

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// setupForkProfile creates a minimal fork profile directory under the test
// profiles root, writes .belayer-talent.yaml with the given memoryScope, and
// returns the profile directory path. An empty memoryScope means no
// .belayer-talent.yaml is written (simulates missing metadata).
func setupForkProfile(t *testing.T, profilesRoot, profileName, memoryScope string) string {
	t.Helper()
	profileDir := filepath.Join(profilesRoot, profileName)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir fork profile %s: %v", profileName, err)
	}
	if memoryScope != "" {
		meta := "profile_name: " + profileName + "\n" +
			"talent_name: supervisor\n" +
			"crag_slug: local\n" +
			"memory_scope: " + memoryScope + "\n" +
			"materialized_at: 2026-01-01T00:00:00Z\n"
		if err := os.WriteFile(filepath.Join(profileDir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
			t.Fatalf("write .belayer-talent.yaml: %v", err)
		}
	}
	return profileDir
}

// setupAgentRunWithProfile creates a session + agent_run row with the given
// profile and "running" status, returning the session ID.
func setupAgentRunWithProfile(t *testing.T, d *Daemon, agentName, profileName string) string {
	t.Helper()
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "teardown-test"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)

	// Also create supervisor so checkSessionStalled doesn't fire in unexpected ways.
	if agentName != "supervisor" {
		_, err := d.store.CreateAgentRun(store.AgentRun{
			SessionID: sess.ID,
			Name:      "supervisor",
			Role:      "supervisor",
			Profile:   "default",
			Status:    "running",
			Transport: "tmux",
		})
		if err != nil {
			t.Fatalf("create supervisor run: %v", err)
		}
	}

	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sess.ID,
		Name:      agentName,
		Role:      agentName,
		Profile:   profileName,
		Status:    "running",
		Transport: "tmux",
		Outcome:   "active",
	})
	if err != nil {
		t.Fatalf("create agent run %q: %v", agentName, err)
	}
	return sess.ID
}

// profileExists returns true if the profile directory exists on disk.
func profileExists(t *testing.T, profilesRoot, profileName string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(profilesRoot, profileName))
	return err == nil
}

// TestTeardown_ClimbScopedProfileRemovedOnFinalResponse verifies that a
// climb-scoped fork profile is deleted when bridge:finished with final_response
// is received.
func TestTeardown_ClimbScopedProfileRemovedOnFinalResponse(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "climb")

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	if profileExists(t, profilesRoot, profileName) {
		t.Errorf("climb-scoped profile %q should have been removed, but still exists", profileName)
	}
}

// TestTeardown_CragScopedProfilePreservedOnFinalResponse verifies that a
// crag-scoped fork profile is NOT deleted on final_response.
func TestTeardown_CragScopedProfilePreservedOnFinalResponse(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "crag")

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	if !profileExists(t, profilesRoot, profileName) {
		t.Errorf("crag-scoped profile %q should have been preserved, but was removed", profileName)
	}
}

// TestTeardown_TalentScopedProfilePreservedOnFinalResponse verifies that a
// talent-scoped fork profile is NOT deleted on final_response.
func TestTeardown_TalentScopedProfilePreservedOnFinalResponse(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "talent")

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	if !profileExists(t, profilesRoot, profileName) {
		t.Errorf("talent-scoped profile %q should have been preserved, but was removed", profileName)
	}
}

// TestTeardown_DefaultProfileNoTeardownAttempted verifies that the legacy
// "default" profile triggers no teardown (backwards compat).
func TestTeardown_DefaultProfileNoTeardownAttempted(t *testing.T) {
	// Ensure HERMES_HOME is set to a temp dir so ProfilesRoot() resolves safely.
	setupBaseBelayerProfile(t)

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", "default")

	// Should not panic or return an error even though there is no "default"
	// directory under ProfilesRoot().
	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished with default profile: %d %s", rr.Code, rr.Body.String())
	}
	// Agent run status must still reach complete (no error path triggered).
	run, err := d.store.GetAgentRun(sessID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Errorf("expected status=complete, got %q", run.Status)
	}
}

// TestTeardown_CustomProfileNoTeardownAttempted verifies that a custom
// operator-override profile (e.g. "nightshift-planner") is not torn down
// because it does not start with "blyr-".
func TestTeardown_CustomProfileNoTeardownAttempted(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "nightshift-planner"
	// Create a directory to verify it survives.
	profileDir := filepath.Join(profilesRoot, profileName)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir custom profile: %v", err)
	}

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// The custom profile directory must still exist — no teardown attempted.
	if !profileExists(t, profilesRoot, profileName) {
		t.Errorf("custom profile %q should NOT have been torn down, but directory is gone", profileName)
	}
}

// TestTeardown_MissingMetadataDefaultsToClimb verifies that when
// .belayer-talent.yaml is absent (empty memoryScope arg to setupForkProfile),
// the daemon treats the profile as climb-scoped and tears it down.
func TestTeardown_MissingMetadataDefaultsToClimb(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	// Pass empty memoryScope → no .belayer-talent.yaml written.
	setupForkProfile(t, profilesRoot, profileName, "")

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	if profileExists(t, profilesRoot, profileName) {
		t.Errorf("profile with missing metadata should have been treated as climb-scoped and removed, but still exists")
	}
}

// TestTeardown_TeardownErrorDoesNotBlockCompletion verifies that when
// TeardownProfile encounters an error (e.g. permission denied), the agent's
// completion status is still updated correctly and the event handler returns
// without propagating the error.
//
// We simulate a teardown failure by making the profile directory read-only so
// os.RemoveAll fails on it, then restoring permissions in a cleanup function.
func TestTeardown_TeardownErrorDoesNotBlockCompletion(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	profileDir := setupForkProfile(t, profilesRoot, profileName, "climb")

	// Make the profile directory itself read-only so RemoveAll cannot delete it.
	if err := os.Chmod(profileDir, 0o555); err != nil {
		t.Fatalf("chmod profile dir read-only: %v", err)
	}
	// Always restore permissions so t.TempDir cleanup can delete it.
	t.Cleanup(func() {
		_ = os.Chmod(profileDir, 0o755)
	})

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "all done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// Agent run must still reach complete despite teardown failure.
	run, err := d.store.GetAgentRun(sessID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Errorf("expected status=complete despite teardown error, got %q", run.Status)
	}
}
