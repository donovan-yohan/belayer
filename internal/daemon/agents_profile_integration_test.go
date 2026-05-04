package daemon

// Phase 2.E integration tests — end-to-end coverage of the spawn →
// materialize → bridge-env → store flow. Each test uses a real filesystem
// (TempDir) but stubs the Python bridge subprocess via d.spawnBridgeAgent.
//
// These complement the unit tests in profiles_test.go and the spawn-path
// unit tests in agents_profile_spawn_test.go.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
)

// ── shared helpers ────────────────────────────────────────────────────────────

// setupIntegrationBase creates a minimal base belayer profile and wires
// HERMES_HOME so ProfilesRoot() and MaterializeProfile() use the test dir.
// Returns (profilesRoot, baseProfileDir).
func setupIntegrationBase(t *testing.T) (profilesRoot, baseProfileDir string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "profiles")
	base := filepath.Join(root, belayerBaseProfileName)
	for _, sub := range []string{
		"memories", "sessions", "skills",
		"plugins", "plugins/belayer",
	} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			t.Fatalf("mkdir base profile %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "auth.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write base auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "plugins", "belayer", "plugin.yaml"), []byte("name: belayer\n"), 0o644); err != nil {
		t.Fatalf("write plugin.yaml: %v", err)
	}
	// Point HERMES_HOME so ProfilesRoot() resolves to root.
	t.Setenv("HERMES_HOME", filepath.Join(root, belayerBaseProfileName))
	return root, base
}

// spawnAgentHTTP posts a spawn request to /sessions/{sessID}/agents and returns
// the captured bridge profile name (what the bridge would have received).
func spawnAgentHTTP(
	t *testing.T,
	d *Daemon,
	sessID string,
	req agentSpawnRequest,
) (capturedProfile string) {
	t.Helper()
	d.spawnBridgeAgent = func(r agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = r.Profile
		return nil, nil
	}
	req.SessionID = "" // filled in by HTTP path
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/agents", req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent %q: %d %s", req.Name, rr.Code, rr.Body.String())
	}
	return capturedProfile
}

// createSession creates a named session with a given workspaceDir.
func createSession(t *testing.T, d *Daemon, name, workspaceDir string) string {
	t.Helper()
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         name,
		WorkspaceDir: workspaceDir,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session %q: %d %s", name, rr.Code, rr.Body.String())
	}
	return decodeJSON[sessionAPIResponse](t, rr).ID
}

// ── Scenario 1: Parallel mains share one profile dir (Phase 5.A) ─────────────

// TestPhase2Integration_ParallelMainsShareForkDir verifies that spawning two
// parallel main agents with the same identity (backend-dev) but different names
// resolves to the SAME fork profile directory (Phase 5.A: stable crag+talent
// naming). The fork dir must have correct symlinks and both agent_runs rows must
// record the same profile name.
func TestPhase2Integration_ParallelMainsShareForkDir(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	workspace := t.TempDir()
	d := testDaemon(t)
	sessID := createSession(t, d, "parallel-mains-test", workspace)

	// Spawn backend-dev-1 and backend-dev-2 sequentially (share same daemon).
	var mu sync.Mutex
	profiles := make(map[string]string) // name → fork profile
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		profiles[req.Name] = req.Profile
		mu.Unlock()
		return nil, nil
	}

	for _, name := range []string{"backend-dev-1", "backend-dev-2"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/agents", agentSpawnRequest{
			Name:     name,
			Identity: "backend-dev",
			Role:     "backend-dev",
			Profile:  belayerBaseProfileName,
			Workdir:  workspace,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("spawn %q: %d %s", name, rr.Code, rr.Body.String())
		}
	}

	p1 := profiles["backend-dev-1"]
	p2 := profiles["backend-dev-2"]

	// Both must be fork names.
	if p1 == belayerBaseProfileName || p2 == belayerBaseProfileName {
		t.Errorf("expected fork profile names, got p1=%q p2=%q", p1, p2)
	}
	if !strings.HasPrefix(p1, "belayer-") || !strings.HasPrefix(p2, "belayer-") {
		t.Errorf("fork names must start with 'belayer-': p1=%q p2=%q", p1, p2)
	}
	// Identity "backend-dev" must appear in both names.
	if !strings.Contains(p1, "backend-dev") || !strings.Contains(p2, "backend-dev") {
		t.Errorf("identity 'backend-dev' missing from fork names: p1=%q p2=%q", p1, p2)
	}
	// Phase 5.A: same identity → same profile (stable crag+talent name, no per-run hash).
	if p1 != p2 {
		t.Errorf("parallel mains with same identity must share one fork profile; got p1=%q p2=%q", p1, p2)
	}
	// No crag link → "local" slug in both.
	if !strings.Contains(p1, "-local-") || !strings.Contains(p2, "-local-") {
		t.Errorf("expected 'local' slug in fork names: p1=%q p2=%q", p1, p2)
	}

	// Fork dir must exist with correct symlinks.
	forkDir := filepath.Join(profilesRoot, p1)
	if _, err := os.Stat(forkDir); err != nil {
		t.Fatalf("fork dir %s not created: %v", forkDir, err)
	}
	// auth.json symlink must exist.
	authLink := filepath.Join(forkDir, "auth.json")
	if _, err := os.Readlink(authLink); err != nil {
		t.Errorf("fork %s: auth.json symlink missing or broken: %v", p1, err)
	}
	// plugins/belayer symlink must exist.
	pluginLink := filepath.Join(forkDir, "plugins", "belayer")
	if _, err := os.Readlink(pluginLink); err != nil {
		t.Errorf("fork %s: plugins/belayer symlink missing or broken: %v", p1, err)
	}
	// skills symlink must exist.
	skillsLink := filepath.Join(forkDir, "skills")
	if _, err := os.Readlink(skillsLink); err != nil {
		t.Errorf("fork %s: skills symlink missing or broken: %v", p1, err)
	}

	// agent_runs.profile must reflect the (shared) fork name for each agent.
	for _, name := range []string{"backend-dev-1", "backend-dev-2"} {
		stored, err := d.store.GetAgentRun(sessID, name)
		if err != nil {
			t.Fatalf("GetAgentRun %q: %v", name, err)
		}
		if stored.Profile != p1 {
			t.Errorf("agent_runs.profile for %q = %q, want %q", name, stored.Profile, p1)
		}
	}
}

// ── Scenario 2: Generated talent gets a scoped profile name ──────────────────

// TestPhase2Integration_GeneratedTalentProfileName verifies that an agent with
// an identity starting with "generated-" produces a fork name of the form
// belayer-<crag>-generated-<something> (no per-run hash, because the generated
// talent name is already unique per-run).
//
// This exercises the generatedTalentPrefix branch in ResolveProfileName.
func TestPhase2Integration_GeneratedTalentProfileName(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	workspace := t.TempDir()
	d := testDaemon(t)
	sessID := createSession(t, d, "generated-talent-test", workspace)

	capturedProfile := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
		Name:    "generated-reviewer-1",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})

	// Must be a fork name.
	if !strings.HasPrefix(capturedProfile, "belayer-") {
		t.Fatalf("expected fork name, got %q", capturedProfile)
	}
	// Must contain the generated talent name verbatim.
	if !strings.Contains(capturedProfile, "generated-reviewer-1") {
		t.Errorf("fork name %q does not contain generated talent name 'generated-reviewer-1'", capturedProfile)
	}
	// Must NOT have an extra hash suffix appended after the generated name.
	// The generated name already encodes uniqueness; instanceID is ignored for
	// generated talents. So the name is belayer-local-generated-reviewer-1
	// (not belayer-local-generated-reviewer-1-<hash>).
	suffix := capturedProfile[strings.LastIndex(capturedProfile, "generated-reviewer-1")+len("generated-reviewer-1"):]
	if suffix != "" {
		t.Errorf("generated talent fork name %q has unexpected suffix %q after talent name (instanceID should be ignored)", capturedProfile, suffix)
	}

	// Fork dir must exist.
	forkDir := filepath.Join(profilesRoot, capturedProfile)
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("fork dir %s not created: %v", forkDir, err)
	}
}

// ── Scenario 3: Crag context resolved from .belayer/config.yaml ──────────────

// TestPhase2Integration_CragContextFromConfig verifies that:
//   - A project with .belayer/config.yaml#crag.name uses that slug in the fork name.
//   - A project without a crag link uses "local" as the fallback slug.
func TestPhase2Integration_CragContextFromConfig(t *testing.T) {
	// Must NOT be parallel: uses t.Setenv for HERMES_HOME.

	t.Run("crag slug from config file", func(t *testing.T) {
		profilesRoot, _ := setupIntegrationBase(t)

		workspace := t.TempDir()
		mustWrite(t, filepath.Join(workspace, ".belayer", "config.yaml"), "crag:\n  name: software-co\n")

		d := testDaemon(t)
		sessID := createSession(t, d, "crag-config-test", workspace)

		capturedProfile := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
			Name:    "supervisor",
			Role:    "supervisor",
			Profile: belayerBaseProfileName,
			Workdir: workspace,
		})

		if !strings.Contains(capturedProfile, "software-co") {
			t.Errorf("expected 'software-co' slug in fork name %q", capturedProfile)
		}
		forkDir := filepath.Join(profilesRoot, capturedProfile)
		if _, err := os.Stat(forkDir); err != nil {
			t.Errorf("fork dir %s not created: %v", forkDir, err)
		}
	})

	t.Run("local slug fallback when no crag link", func(t *testing.T) {
		setupIntegrationBase(t)

		workspace := t.TempDir()
		// No .belayer/config.yaml at all.

		d := testDaemon(t)
		sessID := createSession(t, d, "no-crag-test", workspace)

		capturedProfile := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
			Name:    "supervisor",
			Role:    "supervisor",
			Profile: belayerBaseProfileName,
			Workdir: workspace,
		})

		if !strings.Contains(capturedProfile, "-local-") {
			t.Errorf("expected 'local' slug in fork name %q (no crag link)", capturedProfile)
		}
		if strings.Contains(capturedProfile, "software-co") {
			t.Errorf("unexpected 'software-co' in fork name %q for unlinked project", capturedProfile)
		}
	})
}

// ── Scenario 4: spawnAgentInternal (PM auto-spawn path) gets fork materialization

// TestPhase2Integration_PMAutoSpawnGetsForkProfile verifies that the PM agent
// spawned via the auto-spawn path (handleBridgeCompletionRequested →
// spawnAgentInternal) receives a materialized fork profile rather than "default"
// or "belayer".
//
// This addresses the coverage gap flagged in 2.D quality review: only the HTTP
// spawn path was covered; spawnAgentInternal was not.
func TestPhase2Integration_PMAutoSpawnGetsForkProfile(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	workspace := t.TempDir()
	d := testDaemon(t)

	sessID := createSession(t, d, "pm-autospawn-profile-test", workspace)

	// Pre-create a supervisor agent run so handleFinishAgent can find it.
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "default", // supervisor doesn't need fork for this test
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn supervisor: %d %s", rr.Code, rr.Body.String())
	}

	// Track the PM's captured profile.
	var mu sync.Mutex
	var pmProfile string
	spawned := make(chan struct{}, 4)

	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		if req.Name == "pm" || strings.HasPrefix(req.Name, "generated-") ||
			req.Role == "pm" {
			pmProfile = req.Profile
		}
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

	// Trigger PM auto-spawn via the completion_requested bridge event path.
	rrEvent := postBridgeEvent(t, d, sessID, "bridge:completion_requested", map[string]any{
		"agent":   "supervisor",
		"summary": "all done",
	})
	if rrEvent.Code != http.StatusCreated {
		t.Fatalf("post bridge event: %d %s", rrEvent.Code, rrEvent.Body.String())
	}

	select {
	case <-spawned:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for PM auto-spawn")
	}

	// Poll until PM reaches running status so the async goroutine finishes.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := d.store.GetAgentRun(sessID, "pm")
		if err == nil && run.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	capturedPMProfile := pmProfile
	mu.Unlock()

	// PM must have received a fork profile, not the base "belayer" or "default".
	if capturedPMProfile == belayerBaseProfileName {
		t.Errorf("PM received bare %q profile — materialization did not run on spawnAgentInternal path", belayerBaseProfileName)
	}
	if capturedPMProfile == "default" {
		t.Errorf("PM received 'default' profile — fork materialization fell back (base profile missing or error)")
	}
	if !strings.HasPrefix(capturedPMProfile, "belayer-") {
		t.Errorf("PM fork profile %q does not start with 'belayer-'", capturedPMProfile)
	}
	if !strings.Contains(capturedPMProfile, "pm") {
		t.Errorf("PM fork profile %q does not contain 'pm'", capturedPMProfile)
	}

	// PM fork dir must exist on disk.
	forkDir := filepath.Join(profilesRoot, capturedPMProfile)
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("PM fork profile dir %s not created: %v", forkDir, err)
	}

	// agent_runs.profile must reflect the fork name.
	stored, err := d.store.GetAgentRun(sessID, "pm")
	if err != nil {
		t.Fatalf("GetAgentRun pm: %v", err)
	}
	if stored.Profile != capturedPMProfile {
		t.Errorf("agent_runs.profile for pm = %q, want %q", stored.Profile, capturedPMProfile)
	}
}

// ── Scenario 5: auth.json symlink survives base profile rewrite ───────────────

// TestPhase2Integration_AuthJSONSymlinkSurvivesBaseRewrite is the regression
// test for the Phase 0 spike finding: Hermes uses atomic_replace which calls
// os.path.realpath on the symlink target and renames onto that path. When the
// base auth.json is replaced via os.Rename(tmp, base/auth.json), the fork's
// symlink (fork/auth.json → base/auth.json) still points at the now-updated
// file because the symlink target path (base/auth.json) is the realpath.
//
// POSIX os.Rename onto an existing file replaces its inode-pointer atomically
// in the directory, but symlinks pointing at the path's name still resolve to
// the new content because they follow the name, not the inode.
//
// This test verifies that behaviour end-to-end.
func TestPhase2Integration_AuthJSONSymlinkSurvivesBaseRewrite(t *testing.T) {
	profilesRoot, baseDir := setupIntegrationBase(t)

	workspace := t.TempDir()
	d := testDaemon(t)
	sessID := createSession(t, d, "auth-symlink-test", workspace)

	// Confirm seed content is in base auth.json.
	baseAuthPath := filepath.Join(baseDir, "auth.json")
	seedContent := `{"version":1}`
	if err := os.WriteFile(baseAuthPath, []byte(seedContent), 0o600); err != nil {
		t.Fatalf("write seed auth.json: %v", err)
	}

	// Spawn an agent to trigger profile fork creation.
	capturedProfile := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})

	if !strings.HasPrefix(capturedProfile, "belayer-") {
		t.Fatalf("expected fork profile, got %q", capturedProfile)
	}

	forkAuthPath := filepath.Join(profilesRoot, capturedProfile, "auth.json")

	// Read fork/auth.json through the symlink — must see seed content.
	got, err := os.ReadFile(forkAuthPath)
	if err != nil {
		t.Fatalf("read fork/auth.json before rewrite: %v", err)
	}
	if string(got) != seedContent {
		t.Errorf("before rewrite: fork/auth.json = %q, want %q", string(got), seedContent)
	}

	// Simulate Hermes atomic_replace: write new content to a tmp file, then
	// os.Rename onto base/auth.json (same as os.path.realpath of the symlink).
	//
	// os.Rename replaces the directory entry for base/auth.json; the symlink at
	// fork/auth.json → base/auth.json still resolves via the name, so it now
	// reads the new content. This matches Python's pathlib.Path.replace() /
	// shutil.move() behaviour when the target is not a symlink.
	newContent := `{"version":2}`
	tmpPath := baseAuthPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0o600); err != nil {
		t.Fatalf("write tmp auth.json: %v", err)
	}
	if err := os.Rename(tmpPath, baseAuthPath); err != nil {
		t.Fatalf("atomic rename onto base/auth.json: %v", err)
	}

	// Read fork/auth.json through the symlink again — must see NEW content.
	got, err = os.ReadFile(forkAuthPath)
	if err != nil {
		t.Fatalf("read fork/auth.json after rewrite: %v", err)
	}
	if string(got) != newContent {
		t.Errorf("after rewrite: fork/auth.json = %q, want %q", string(got), newContent)
	}
}

// ── Scenario 6: Re-spawn reuses the same fork across climbs (Phase 5.A) ───────

// TestPhase2Integration_ReSpawnReusesSameForkAcrossClimbs verifies that
// spawning the same identity a second time (same session, new run row) produces
// the SAME fork profile name. Phase 5.A dropped the per-run instance hash, so
// the profile is now derived purely from crag + talent — identical across all
// climbs for the same identity.
func TestPhase2Integration_ReSpawnReusesSameForkAcrossClimbs(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	workspace := t.TempDir()
	d := testDaemon(t)
	sessID := createSession(t, d, "respawn-stable-test", workspace)

	// First spawn.
	profile1 := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})

	if !strings.HasPrefix(profile1, "belayer-") {
		t.Fatalf("first spawn: expected fork profile, got %q", profile1)
	}

	// Simulate termination so the second spawn is not rejected by capacity checks.
	if err := d.store.UpdateAgentRunStatus(sessID, "supervisor", "complete"); err != nil {
		t.Fatalf("mark supervisor complete: %v", err)
	}

	// Second spawn of the same identity.
	profile2 := spawnAgentHTTP(t, d, sessID, agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})

	if !strings.HasPrefix(profile2, "belayer-") {
		t.Fatalf("second spawn: expected fork profile, got %q", profile2)
	}

	// Both profiles must contain the talent name.
	if !strings.Contains(profile1, "supervisor") || !strings.Contains(profile2, "supervisor") {
		t.Errorf("talent name missing: profile1=%q profile2=%q", profile1, profile2)
	}

	// Phase 5.A: stable crag+talent profile name — both spawns must resolve to
	// the same fork regardless of session or run UUID.
	if profile1 != profile2 {
		t.Errorf("re-spawn must reuse the same fork profile; got profile1=%q profile2=%q", profile1, profile2)
	}

	// Fork dir must exist on disk.
	forkDir := filepath.Join(profilesRoot, profile1)
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("fork dir %s not created: %v", forkDir, err)
	}

	// agent_runs.profile must reflect the stable fork name.
	stored, err := d.store.GetAgentRun(sessID, "supervisor")
	if err != nil {
		t.Fatalf("GetAgentRun supervisor: %v", err)
	}
	if stored.Profile != profile1 {
		t.Errorf("after second spawn agent_runs.profile = %q, want %q", stored.Profile, profile1)
	}
}
