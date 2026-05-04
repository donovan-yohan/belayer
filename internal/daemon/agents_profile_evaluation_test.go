package daemon

// Phase 3.D evaluation artifact tests — verify that a talent-evaluation/v1
// artifact is written to the climb's artifacts directory before a climb-scoped
// profile is torn down, and that the best-effort failure modes do not block
// teardown or session transitions.
//
// Fixture pattern: same as agents_profile_teardown_test.go. The bridge subprocess
// is stubbed via d.spawnBridgeAgent so no Python process is started; the
// final_response event is injected directly via postBridgeEvent.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// setupEvalSession creates a session with a real workspace dir and returns
// (sessionID, workspaceDir). This is used for evaluation tests that need to
// verify artifact files are written.
func setupEvalSession(t *testing.T, d *Daemon) (sessionID, workspaceDir string) {
	t.Helper()
	workspaceDir = t.TempDir()
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "eval-test",
		WorkspaceDir: workspaceDir,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)
	return sess.ID, workspaceDir
}

// setupAgentRunForEval creates a supervisor + target agent run in the given
// session with the specified profile. Returns the agent_run ID.
func setupAgentRunForEval(t *testing.T, d *Daemon, sessID, agentName, profileName string) string {
	t.Helper()
	// Supervisor is required to prevent checkSessionStalled firing unexpectedly.
	if agentName != "supervisor" {
		_, err := d.store.CreateAgentRun(store.AgentRun{
			SessionID: sessID,
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
	runID, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID,
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
	return runID
}

// createArtifactsDir creates the climb artifacts directory for a session under
// workspaceDir. Tests stub this so writeTalentEvaluationArtifact can write files.
func createArtifactsDir(t *testing.T, workspaceDir, sessionID string) string {
	t.Helper()
	artifactsDir := filepath.Join(workspaceDir, ".belayer", "climbs", sessionID, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts dir: %v", err)
	}
	return artifactsDir
}

// readArtifactInDir finds the first .json file in artifactsDir whose base name
// starts with prefix and returns its content.
func readArtifactInDir(t *testing.T, artifactsDir, prefix string) []byte {
	t.Helper()
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", artifactsDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) >= len(prefix) && e.Name()[:len(prefix)] == prefix {
			data, readErr := os.ReadFile(filepath.Join(artifactsDir, e.Name()))
			if readErr != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), readErr)
			}
			return data
		}
	}
	return nil
}

// TestEvaluation_MEMORYMDPresent verifies that when MEMORY.md exists in the
// profile's memories/ dir, the artifact is written with a single strength-type
// observation whose summary contains the MEMORY.md content.
func TestEvaluation_MEMORYMDPresent(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	profileDir := setupForkProfile(t, profilesRoot, profileName, "climb")

	// Write MEMORY.md to the profile's memories directory.
	memoriesDir := filepath.Join(profileDir, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir memories: %v", err)
	}
	const memoryContent = "# Memory\n\nI learned that X is better than Y.\n"
	if err := os.WriteFile(filepath.Join(memoriesDir, "MEMORY.md"), []byte(memoryContent), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)
	artifactsDir := createArtifactsDir(t, workspaceDir, sessID)

	setupAgentRunForEval(t, d, sessID, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	raw := readArtifactInDir(t, artifactsDir, "talent-evaluation-supervisor-")
	if raw == nil {
		t.Fatal("expected talent-evaluation artifact file, none found")
	}

	var art talentEvaluationArtifact
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}

	if art.SchemaVersion != "belayer-talent-evaluation/v1" {
		t.Errorf("schema_version = %q, want belayer-talent-evaluation/v1", art.SchemaVersion)
	}
	if art.Talent != "supervisor" {
		t.Errorf("talent = %q, want supervisor", art.Talent)
	}
	if art.Session.ID != sessID {
		t.Errorf("session.id = %q, want %q", art.Session.ID, sessID)
	}
	if len(art.Observations) == 0 {
		t.Fatal("observations should be non-empty when MEMORY.md exists")
	}
	obs := art.Observations[0]
	if obs.Type != "strength" {
		t.Errorf("observations[0].type = %q, want strength", obs.Type)
	}
	if obs.Summary != memoryContent {
		t.Errorf("observations[0].summary = %q, want %q", obs.Summary, memoryContent)
	}
}

// TestEvaluation_MEMORYMDAbsent verifies that when no MEMORY.md exists, the
// artifact is still written but with an empty observations array.
func TestEvaluation_MEMORYMDAbsent(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "climb")
	// No MEMORY.md written.

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)
	artifactsDir := createArtifactsDir(t, workspaceDir, sessID)

	setupAgentRunForEval(t, d, sessID, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	raw := readArtifactInDir(t, artifactsDir, "talent-evaluation-supervisor-")
	if raw == nil {
		t.Fatal("expected talent-evaluation artifact file even when MEMORY.md absent")
	}

	var art talentEvaluationArtifact
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if len(art.Observations) != 0 {
		t.Errorf("observations should be empty when no memories exist, got %v", art.Observations)
	}
	if art.SchemaVersion != "belayer-talent-evaluation/v1" {
		t.Errorf("schema_version = %q, want belayer-talent-evaluation/v1", art.SchemaVersion)
	}
}

// TestEvaluation_UserMDAlsoCaptured verifies that when both MEMORY.md and
// USER.md exist in the profile's memories/ directory, both are captured as a
// single strength-type observation with the contents joined by "--- USER ---".
// Uses a supervisor-named profile (setupForkProfile always writes talent_name:
// supervisor), so the artifact filename uses "supervisor".
func TestEvaluation_UserMDAlsoCaptured(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	// Use a supervisor-named profile to match what setupForkProfile writes
	// in talent_name. See setupForkProfile: it always writes talent_name: supervisor.
	const profileName = "blyr-local-supervisor"
	profileDir := setupForkProfile(t, profilesRoot, profileName, "climb")

	memoriesDir := filepath.Join(profileDir, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir memories: %v", err)
	}

	const memContent = "# Memory\n\nRemember to use Go interfaces.\n"
	const userContent = "# User\n\nThe user prefers short functions.\n"
	if err := os.WriteFile(filepath.Join(memoriesDir, "MEMORY.md"), []byte(memContent), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoriesDir, "USER.md"), []byte(userContent), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)
	artifactsDir := createArtifactsDir(t, workspaceDir, sessID)

	setupAgentRunForEval(t, d, sessID, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	raw := readArtifactInDir(t, artifactsDir, "talent-evaluation-supervisor-")
	if raw == nil {
		t.Fatal("expected talent-evaluation artifact file")
	}

	var art talentEvaluationArtifact
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	if len(art.Observations) == 0 {
		t.Fatal("observations should be non-empty when MEMORY.md and USER.md exist")
	}
	obs := art.Observations[0]
	if obs.Type != "strength" {
		t.Errorf("observations[0].type = %q, want strength", obs.Type)
	}
	wantSummary := memContent + "\n\n--- USER ---\n\n" + userContent
	if obs.Summary != wantSummary {
		t.Errorf("observations[0].summary = %q, want %q", obs.Summary, wantSummary)
	}
	if len(art.Observations) != 1 {
		t.Errorf("expected exactly 1 observation (joined MEMORY+USER), got %d", len(art.Observations))
	}
}

// TestEvaluation_ArtifactDirMissingGracefulTeardown verifies that when the
// climb artifacts directory doesn't exist and can't be created (read-only
// parent), the error is logged but teardown still proceeds.
func TestEvaluation_ArtifactDirMissingGracefulTeardown(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "climb")

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)

	// Make the .belayer/ directory read-only so MkdirAll for artifacts dir fails.
	belayerDir := filepath.Join(workspaceDir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o755); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	if err := os.Chmod(belayerDir, 0o555); err != nil {
		t.Fatalf("chmod .belayer read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(belayerDir, 0o755)
	})

	setupAgentRunForEval(t, d, sessID, "supervisor", profileName)

	// Should not panic; teardown should still succeed.
	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// Profile must be torn down despite artifact write failure.
	if profileExists(t, profilesRoot, profileName) {
		t.Errorf("climb-scoped profile %q should have been removed despite artifact dir failure", profileName)
	}

	// Agent run must still reach complete.
	run, err := d.store.GetAgentRun(sessID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "complete" {
		t.Errorf("expected status=complete, got %q", run.Status)
	}
}

// TestEvaluation_CragScopedProfileNoArtifact verifies that a crag-scoped
// profile is preserved (no teardown, no evaluation event).
func TestEvaluation_CragScopedProfileNoArtifact(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	const profileName = "blyr-local-supervisor"
	setupForkProfile(t, profilesRoot, profileName, "crag")

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)
	artifactsDir := createArtifactsDir(t, workspaceDir, sessID)

	setupAgentRunForEval(t, d, sessID, "supervisor", profileName)

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// Profile must still exist (crag-scoped = preserved).
	if !profileExists(t, profilesRoot, profileName) {
		t.Errorf("crag-scoped profile %q should be preserved but was removed", profileName)
	}

	// No artifact should have been written.
	entries, _ := os.ReadDir(artifactsDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			t.Errorf("unexpected artifact file %q found for crag-scoped profile", e.Name())
		}
	}
}

// TestEvaluation_DefaultProfileNoArtifact verifies that agents using the
// "default" profile do not trigger evaluation artifact writes.
func TestEvaluation_DefaultProfileNoArtifact(t *testing.T) {
	setupBaseBelayerProfile(t)

	d := testDaemon(t)
	sessID, workspaceDir := setupEvalSession(t, d)
	artifactsDir := createArtifactsDir(t, workspaceDir, sessID)

	// Create agent run with "default" profile.
	_, err := d.store.CreateAgentRun(store.AgentRun{
		SessionID: sessID,
		Name:      "supervisor",
		Role:      "supervisor",
		Profile:   "default",
		Status:    "running",
		Transport: "tmux",
		Outcome:   "active",
	})
	if err != nil {
		t.Fatalf("create agent run: %v", err)
	}

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "supervisor",
		"final_response": "done",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// No artifact should exist.
	entries, _ := os.ReadDir(artifactsDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			t.Errorf("unexpected artifact file %q for default-profile agent", e.Name())
		}
	}
}
