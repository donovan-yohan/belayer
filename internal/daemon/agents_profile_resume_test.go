package daemon

// Tests for Phase 3.E: resumable wake fork-profile reuse.
//
// These tests verify that:
//  1. Re-spawning a resumable (lifecycle: resumable) agent reuses the same fork
//     profile and does not wipe accumulated memory (MEMORY.md sentinel check).
//  2. Resumable lifecycle defaults memory.scope to "crag" when no explicit scope
//     is set in talent.yaml.
//  3. An explicitly climb-scoped resumable agent (operator override) is still
//     torn down on final_response — operator intent wins.
//  4. Mail-wake on an already-torn-down profile re-materializes from base without
//     error; memory is gone (operator accepted that).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/bridge"
)

// ── Test 1: Re-spawn reuses the same fork profile ─────────────────────────────

// TestResumable_ReSpawnReusesForkProfile verifies that when a resumable agent
// is spawned twice (simulating dormancy → wake), the fork profile name is
// identical on both spawns and any data written into the profile between spawns
// is preserved.
func TestResumable_ReSpawnReusesForkProfile(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// Write identity with lifecycle=resumable and memory.scope=crag.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "system-prompt.md"), "you are lorekeeper")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "agent.yaml"), "kind: side\n")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\nmemory:\n  scope: crag\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "resumable-reuse-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfiles []string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfiles = append(capturedProfiles, req.Profile)
		return nil, nil
	}

	// First spawn.
	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("first spawn: %d %s", rr.Code, rr.Body.String())
	}

	if len(capturedProfiles) < 1 {
		t.Fatal("spawnBridgeAgent not called on first spawn")
	}
	firstProfile := capturedProfiles[0]
	if !strings.HasPrefix(firstProfile, "belayer-") {
		t.Fatalf("expected fork profile name on first spawn, got %q", firstProfile)
	}

	// Write a sentinel into memories/ so we can assert it survives the second spawn.
	sentinelPath := filepath.Join(profilesRoot, firstProfile, "memories", "MEMORY.md")
	sentinelContent := "sentinel: lorekeeper memory survives dormancy"
	if err := os.WriteFile(sentinelPath, []byte(sentinelContent), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Transition run to complete (simulates the agent finishing / going dormant).
	if err := d.store.UpdateAgentRunStatus(sess.ID, "lorekeeper", "complete"); err != nil {
		t.Fatalf("update status to complete: %v", err)
	}

	// Second spawn (simulates wake — same session, same agent name).
	rr2 := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
		Message: "Resume from prior Hermes conversation.",
	})
	if rr2.Code != http.StatusCreated {
		t.Fatalf("second spawn: %d %s", rr2.Code, rr2.Body.String())
	}

	if len(capturedProfiles) < 2 {
		t.Fatal("spawnBridgeAgent not called on second spawn")
	}
	secondProfile := capturedProfiles[1]

	// Both spawns must use the identical fork profile name.
	if firstProfile != secondProfile {
		t.Errorf("profile mismatch: first spawn = %q, second spawn = %q; wake should reuse the same fork",
			firstProfile, secondProfile)
	}

	// Sentinel must still be present — profile not torn down between spawns.
	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("read sentinel after wake: %v — profile was torn down when it should have been preserved", err)
	}
	if string(got) != sentinelContent {
		t.Errorf("sentinel content changed: got %q, want %q", string(got), sentinelContent)
	}
}

// ── Test 2: Resumable lifecycle defaults memory.scope to crag ─────────────────

// TestResumable_DefaultsMemoryScopeToCrag verifies that when talent.yaml
// declares lifecycle=resumable but does NOT set memory.scope, the forked
// profile is materialized with memory_scope=crag (not climb).
func TestResumable_DefaultsMemoryScopeToCrag(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// talent.yaml with runtime.lifecycle=resumable but NO memory.scope.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "system-prompt.md"), "you are lorekeeper")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "agent.yaml"), "kind: side\n")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "resumable-default-scope-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	if !strings.HasPrefix(capturedProfile, "belayer-") {
		t.Fatalf("expected fork profile name, got %q", capturedProfile)
	}

	// .belayer-talent.yaml must have memory_scope: crag (not climb).
	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "memory_scope: crag") {
		t.Errorf(".belayer-talent.yaml must contain memory_scope: crag for resumable agent without explicit scope, got:\n%s", content)
	}

	// Confirm that loadAgentIdentity also reports crag via the default rule.
	// We check the struct directly to verify the default happens in materializeBridgeProfile.
	loaded := loadAgentIdentity(workspace, "", "lorekeeper", "")
	if loaded.MemoryScope != "" {
		t.Errorf("loadAgentIdentity MemoryScope = %q, want empty (default applied by materializeBridgeProfile, not loadAgentIdentity)", loaded.MemoryScope)
	}
	if loaded.RuntimeLifecycle != "resumable" {
		t.Errorf("loadAgentIdentity RuntimeLifecycle = %q, want %q", loaded.RuntimeLifecycle, "resumable")
	}
}

// ── Test 3: Explicit climb-scoped resumable agent is torn down ─────────────────

// TestResumable_ExplicitClimbScopeTeardown verifies that when an operator
// explicitly sets memory.scope=climb on a resumable agent, the profile is still
// torn down on final_response (operator's explicit choice wins over the default).
func TestResumable_ExplicitClimbScopeTeardown(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	// Create a climb-scoped fork profile for a resumable agent (operator's override).
	const profileName = "belayer-local-lorekeeper"
	setupForkProfile(t, profilesRoot, profileName, "climb")

	d := testDaemon(t)
	sessID := setupAgentRunWithProfile(t, d, "lorekeeper", profileName)

	// Also update the run status to running for the teardown path to see.
	if err := d.store.UpdateAgentRunStatus(sessID, "lorekeeper", "running"); err != nil {
		t.Fatalf("update run to running: %v", err)
	}

	rr := postBridgeEvent(t, d, sessID, "bridge:finished", map[string]any{
		"agent":          "lorekeeper",
		"final_response": "world state saved",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("post bridge:finished: %d %s", rr.Code, rr.Body.String())
	}

	// Climb-scoped profile must be torn down regardless of runtime.lifecycle.
	if profileExists(t, profilesRoot, profileName) {
		t.Errorf("climb-scoped resumable profile %q should have been torn down on final_response, but still exists", profileName)
	}
}

// ── Test 4: Wake on torn-down profile re-materializes from base ───────────────

// TestResumable_WakeOnTornDownProfileReMaterializes verifies the edge case
// where a resumable agent's profile was deleted (e.g. by `belayer prune`) but
// a wake event fires. The spawn must succeed and re-materialize a fresh profile
// from base without error. Memory is gone — operator accepted that.
func TestResumable_WakeOnTornDownProfileReMaterializes(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "system-prompt.md"), "you are lorekeeper")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "agent.yaml"), "kind: side\n")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\nmemory:\n  scope: crag\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "torn-down-wake-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	// Initial spawn — creates the fork profile.
	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("initial spawn: %d %s", rr.Code, rr.Body.String())
	}
	firstProfile := capturedProfile

	// Simulate `belayer prune` by manually removing the fork profile.
	forkDir := filepath.Join(profilesRoot, firstProfile)
	if err := os.RemoveAll(forkDir); err != nil {
		t.Fatalf("remove fork profile (simulate prune): %v", err)
	}
	if _, err := os.Stat(forkDir); !os.IsNotExist(err) {
		t.Fatalf("profile dir should be gone after simulated prune")
	}

	// Transition run to complete (agent went dormant).
	if err := d.store.UpdateAgentRunStatus(sess.ID, "lorekeeper", "complete"); err != nil {
		t.Fatalf("update status to complete: %v", err)
	}

	// Wake: re-spawn with the same session ID. MaterializeProfile is idempotent —
	// it will re-create the profile directory when the base profile exists.
	rr2 := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
		Message: "Resume — operator pruned storage but base profile exists.",
	})
	if rr2.Code != http.StatusCreated {
		t.Fatalf("wake after prune must succeed (re-materialize from base); got %d %s", rr2.Code, rr2.Body.String())
	}

	// Profile dir must now exist again (re-materialized).
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("fork profile %s should have been re-materialized after prune wake, but stat failed: %v", firstProfile, err)
	}

	// Profile name must be the same (Phase 5.A: stable crag+talent name — same
	// identity always resolves to the same fork regardless of run UUID).
	if capturedProfile != firstProfile {
		t.Errorf("re-materialized profile name = %q, want %q (profile name must be stable across re-spawns)", capturedProfile, firstProfile)
	}

	// Memory is gone (no MEMORY.md) — operator accepted by running prune.
	memPath := filepath.Join(forkDir, "memories", "MEMORY.md")
	if _, err := os.Stat(memPath); err == nil {
		t.Errorf("MEMORY.md should not exist after prune wake — memory is gone, and that is expected")
	}

	// agent_run must be running (spawn succeeded).
	run, err := d.store.GetAgentRun(sess.ID, "lorekeeper")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.Status != "running" && run.Status != "starting" {
		t.Errorf("agent run status after wake = %q, want running or starting", run.Status)
	}
}

// ── Unit: loadAgentIdentity reads RuntimeLifecycle ────────────────────────────

// TestLoadAgentIdentity_TalentYAML_RuntimeLifecycleResumable verifies that
// loadAgentIdentity reads runtime.lifecycle from talent.yaml into RuntimeLifecycle.
func TestLoadAgentIdentity_TalentYAML_RuntimeLifecycleResumable(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "lorekeeper", "system-prompt.md"), "you are lorekeeper")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "lorekeeper", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\nmemory:\n  scope: crag\n")

	got := loadAgentIdentity("", belayerRoot, "lorekeeper", "")
	if got.RuntimeLifecycle != "resumable" {
		t.Errorf("RuntimeLifecycle = %q, want %q", got.RuntimeLifecycle, "resumable")
	}
	if got.MemoryScope != "crag" {
		t.Errorf("MemoryScope = %q, want %q", got.MemoryScope, "crag")
	}
}

// TestLoadAgentIdentity_TalentYAML_NoRuntimeLifecycleIsEmpty verifies that
// when talent.yaml has no runtime.lifecycle, RuntimeLifecycle is empty.
func TestLoadAgentIdentity_TalentYAML_NoRuntimeLifecycleIsEmpty(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "supervisor", "system-prompt.md"), "you are supervisor")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "supervisor", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: climb\n")

	got := loadAgentIdentity("", belayerRoot, "supervisor", "")
	if got.RuntimeLifecycle != "" {
		t.Errorf("RuntimeLifecycle = %q, want empty when not set", got.RuntimeLifecycle)
	}
}

// TestSpawnProfile_ResumableNoScopeDefaultsCragInTalentYAML verifies the
// integration path: spawning with lifecycle=resumable and no memory.scope
// results in .belayer-talent.yaml recording memory_scope: crag.
func TestSpawnProfile_ResumableNoScopeDefaultsCragInTalentYAML(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "continuity-editor", "system-prompt.md"), "you are continuity-editor")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "continuity-editor", "agent.yaml"), "kind: side\n")
	// Only runtime.lifecycle — no memory.scope.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "continuity-editor", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "resumable-no-scope-default-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "continuity-editor",
		Role:    "continuity-editor",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml: %v", err)
	}
	if !strings.Contains(string(data), "memory_scope: crag") {
		t.Errorf(".belayer-talent.yaml must have memory_scope: crag for resumable agent with no explicit scope; got:\n%s", string(data))
	}
}

// TestSpawnProfile_NonResumableNoScopeDefaultsClimb verifies that the
// crag-default rule does NOT apply to non-resumable agents: without
// runtime.lifecycle=resumable, missing memory.scope still defaults to "climb".
func TestSpawnProfile_NonResumableNoScopeDefaultsClimb(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// No talent.yaml at all — not resumable, no scope.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "qa", "system-prompt.md"), "you are qa")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "qa", "agent.yaml"), "kind: side\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "non-resumable-default-climb-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "qa",
		Role:    "qa",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml: %v", err)
	}
	if !strings.Contains(string(data), "memory_scope: climb") {
		t.Errorf(".belayer-talent.yaml must have memory_scope: climb for non-resumable agent without scope; got:\n%s", string(data))
	}
}

// TestResumable_ExplicitClimbOverridesDefaultCrag verifies that setting an
// explicit memory.scope=climb on a resumable agent wins over the crag default
// (operator's explicit choice).
func TestResumable_ExplicitClimbOverridesDefaultCrag(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "system-prompt.md"), "you are lorekeeper")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "agent.yaml"), "kind: side\n")
	// Explicit climb override for a resumable agent.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "lorekeeper", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nruntime:\n  lifecycle: resumable\nmemory:\n  scope: climb\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "resumable-explicit-climb-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "lorekeeper",
		Role:    "lorekeeper",
		Kind:    "side",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml: %v", err)
	}
	// Explicit climb must win over the crag default.
	if !strings.Contains(string(data), "memory_scope: climb") {
		t.Errorf(".belayer-talent.yaml must have memory_scope: climb when operator explicitly sets climb; got:\n%s", string(data))
	}
	if strings.Contains(string(data), "memory_scope: crag") {
		t.Errorf(".belayer-talent.yaml must NOT have memory_scope: crag when operator explicitly set climb; got:\n%s", string(data))
	}
}

