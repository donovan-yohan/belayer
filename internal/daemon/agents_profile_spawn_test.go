package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/bridge"
)

// setupBaseBelayerProfile creates a minimal ~/.hermes/profiles/belayer base
// directory structure so that MaterializeProfile can succeed. It sets
// HERMES_HOME so ProfilesRoot() returns the test-local profiles root.
// Returns the profiles root path and the base profile dir path.
func setupBaseBelayerProfile(t *testing.T) (profilesRoot, baseProfileDir string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "profiles")
	base := filepath.Join(root, belayerBaseProfileName)
	for _, sub := range []string{"memories", "sessions", "skills", "plugins", "plugins/belayer"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			t.Fatalf("mkdir base profile %s: %v", sub, err)
		}
	}
	// Create auth.json stub so symlinks can be verified.
	if err := os.WriteFile(filepath.Join(base, "auth.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	// Point HERMES_HOME so ProfilesRoot() returns root.
	t.Setenv("HERMES_HOME", filepath.Join(root, belayerBaseProfileName))
	return root, base
}

// TestSpawnProfile_BelayerProfileMaterializesFork verifies that spawning with
// Profile == "belayer" causes a per-talent-instance fork profile to be
// materialized and the bridge subprocess receives the fork profile name in
// BELAYER_PROFILE (resolved via bridge.Config.Profile).
func TestSpawnProfile_BelayerProfileMaterializesFork(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "profile-fork-test",
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
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	// The captured profile must be the fork name, not "belayer".
	if capturedProfile == belayerBaseProfileName {
		t.Errorf("expected fork profile name, got bare %q — materialization did not run", belayerBaseProfileName)
	}
	if !strings.HasPrefix(capturedProfile, "blyr-") {
		t.Errorf("fork profile name %q does not start with 'blyr-'", capturedProfile)
	}
	// No crag link → should include "local" slug.
	if !strings.Contains(capturedProfile, "-local-") {
		t.Errorf("expected 'local' crag slug in fork name %q (no crag link present)", capturedProfile)
	}
	// Should include talent name.
	if !strings.Contains(capturedProfile, "supervisor") {
		t.Errorf("expected talent name 'supervisor' in fork name %q", capturedProfile)
	}

	// The fork directory must exist on disk.
	forkDir := filepath.Join(profilesRoot, capturedProfile)
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("fork profile dir %s not created: %v", forkDir, err)
	}

	// agent_runs.profile must be updated to the fork name.
	stored, err := d.store.GetAgentRun(sess.ID, "supervisor")
	if err != nil {
		t.Fatalf("get agent run: %v", err)
	}
	if stored.Profile != capturedProfile {
		t.Errorf("agent_runs.profile = %q, want %q", stored.Profile, capturedProfile)
	}
}

// TestSpawnProfile_DefaultProfileSkipsMaterialization verifies that spawning
// with Profile == "default" preserves the legacy behaviour: no fork profile is
// created and the bridge gets Profile "default" verbatim.
func TestSpawnProfile_DefaultProfileSkipsMaterialization(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)
	_ = profilesRoot // base profile available but should not be forked

	workspace := t.TempDir()
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "default-profile-test",
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
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "default",
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	// Profile must be "default" unchanged.
	if capturedProfile != "default" {
		t.Errorf("expected profile %q, got %q", "default", capturedProfile)
	}

	// No fork directories should have been created under the profiles root.
	entries, _ := os.ReadDir(profilesRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "blyr-") {
			t.Errorf("unexpected fork profile dir created for 'default' spawn: %s", e.Name())
		}
	}
}

// TestSpawnProfile_CustomProfileSkipsMaterialization verifies that a custom
// operator override profile name is passed through unchanged to the bridge
// and no fork is created.
func TestSpawnProfile_CustomProfileSkipsMaterialization(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)
	_ = profilesRoot

	workspace := t.TempDir()
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "custom-profile-test",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	const customProfile = "nightshift-planner"
	var capturedProfile string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		capturedProfile = req.Profile
		return nil, nil
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: customProfile,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	if capturedProfile != customProfile {
		t.Errorf("expected profile %q, got %q", customProfile, capturedProfile)
	}

	// No fork directories should have been created.
	entries, _ := os.ReadDir(profilesRoot)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "blyr-") {
			t.Errorf("unexpected fork profile dir created for custom profile spawn: %s", e.Name())
		}
	}
}

// TestSpawnProfile_CragLinkPresentUsesCragSlug verifies that when a project
// has a crag link, the fork name uses the crag slug from config.yaml.
func TestSpawnProfile_CragLinkPresentUsesCragSlug(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// Write a .belayer/config.yaml with a crag link.
	cfgPath := filepath.Join(workspace, ".belayer", "config.yaml")
	mustWrite(t, cfgPath, "crag:\n  name: \"software-co\"\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "crag-link-test",
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
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(capturedProfile, "software-co") {
		t.Errorf("expected crag slug 'software-co' in fork name %q", capturedProfile)
	}

	// Fork dir should exist.
	forkDir := filepath.Join(profilesRoot, capturedProfile)
	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("fork profile dir %s not created: %v", forkDir, err)
	}
}

// TestSpawnProfile_NoCragLinkUsesLocalSlug verifies that when no crag link
// exists, the fork name contains the "local" fallback slug.
func TestSpawnProfile_NoCragLinkUsesLocalSlug(t *testing.T) {
	setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// No .belayer/config.yaml at all.

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "no-crag-test",
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
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent: %d %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(capturedProfile, "-local-") {
		t.Errorf("expected 'local' slug in fork name %q (no crag link)", capturedProfile)
	}
}

// TestSpawnProfile_BaseProfileMissingFallsBackToDefault verifies graceful
// degradation: when the base belayer profile directory does not exist (operator
// hasn't run `belayer auth`), the spawn succeeds with Profile == "default"
// so agents continue working as before the profiles feature.
func TestSpawnProfile_BaseProfileMissingFallsBackToDefault(t *testing.T) {
	// Set HERMES_HOME to a temp root that has NO belayer profile inside.
	emptyRoot := filepath.Join(t.TempDir(), "profiles")
	if err := os.MkdirAll(emptyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Point HERMES_HOME so ProfilesRoot() returns emptyRoot.
	t.Setenv("HERMES_HOME", filepath.Join(emptyRoot, belayerBaseProfileName))

	workspace := t.TempDir()
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "no-base-profile-test",
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
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("spawn agent must succeed even when base profile is missing; got %d %s", rr.Code, rr.Body.String())
	}

	// Fall-back means profile == "default".
	if capturedProfile != "default" {
		t.Errorf("expected fallback profile 'default', got %q", capturedProfile)
	}
}

// ── Phase 5.A: stable cross-climb profile naming ─────────────────────────────

// TestSpawnProfile_StableProfileNameAcrossClimbs verifies that spawning the
// same identity in two different sessions (climbs) resolves to the same fork
// profile name. Phase 5.A drops the per-run instance hash so the profile is
// keyed purely on crag+talent.
func TestSpawnProfile_StableProfileNameAcrossClimbs(t *testing.T) {
	setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	d := testDaemon(t)

	// Climb 1: create a session and spawn supervisor.
	sess1RR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "climb-1-stable-name",
		WorkspaceDir: workspace,
	})
	if sess1RR.Code != http.StatusCreated {
		t.Fatalf("create session 1: %d %s", sess1RR.Code, sess1RR.Body.String())
	}
	sess1 := decodeJSON[sessionAPIResponse](t, sess1RR)

	var profile1 string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		profile1 = req.Profile
		return nil, nil
	}

	rr1 := doRequest(t, d, "POST", "/sessions/"+sess1.ID+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr1.Code != http.StatusCreated {
		t.Fatalf("climb-1 spawn: %d %s", rr1.Code, rr1.Body.String())
	}
	if !strings.HasPrefix(profile1, "blyr-") {
		t.Fatalf("climb-1: expected fork profile, got %q", profile1)
	}

	// Climb 2: different session, same project, same identity.
	sess2RR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "climb-2-stable-name",
		WorkspaceDir: workspace,
	})
	if sess2RR.Code != http.StatusCreated {
		t.Fatalf("create session 2: %d %s", sess2RR.Code, sess2RR.Body.String())
	}
	sess2 := decodeJSON[sessionAPIResponse](t, sess2RR)

	var profile2 string
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		profile2 = req.Profile
		return nil, nil
	}

	rr2 := doRequest(t, d, "POST", "/sessions/"+sess2.ID+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: belayerBaseProfileName,
		Workdir: workspace,
	})
	if rr2.Code != http.StatusCreated {
		t.Fatalf("climb-2 spawn: %d %s", rr2.Code, rr2.Body.String())
	}
	if !strings.HasPrefix(profile2, "blyr-") {
		t.Fatalf("climb-2: expected fork profile, got %q", profile2)
	}

	// Phase 5.A: same crag + same identity → same profile across climbs.
	if profile1 != profile2 {
		t.Errorf("cross-climb profile mismatch: climb-1=%q climb-2=%q; same identity in same crag must resolve to the same fork", profile1, profile2)
	}
}

// TestSpawnProfile_ParallelMainsShareProfile verifies that two parallel main
// agents with the same identity (backend-dev) but different names resolve to
// the same profile: belayer-local-backend-dev. Phase 5.A dropped per-run
// hashes, so identity alone determines the fork.
func TestSpawnProfile_ParallelMainsShareProfile(t *testing.T) {
	setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	d := testDaemon(t)

	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "parallel-mains-share-profile",
		WorkspaceDir: workspace,
	})
	if sessRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", sessRR.Code, sessRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, sessRR)

	profiles := make(map[string]string)
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		profiles[req.Name] = req.Profile
		return nil, nil
	}

	for _, name := range []string{"backend-dev-1", "backend-dev-2"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sess.ID+"/agents", agentSpawnRequest{
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

	// Both must resolve to the same stable profile name.
	if p1 != p2 {
		t.Errorf("parallel mains with same identity must share one profile; got backend-dev-1=%q backend-dev-2=%q", p1, p2)
	}

	// Profile name must be blyr-local-backend-dev (no hash suffix).
	const wantProfile = "blyr-local-backend-dev"
	if p1 != wantProfile {
		t.Errorf("profile = %q, want %q", p1, wantProfile)
	}
}

// TestResolveCragSlug tests the crag slug resolver helper directly.
func TestResolveCragSlug(t *testing.T) {
	t.Parallel()

	t.Run("no config file returns local", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		slug, err := ResolveCragSlug(dir)
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != localCragSlug {
			t.Errorf("slug = %q, want %q", slug, localCragSlug)
		}
	})

	t.Run("config file without crag block returns local", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".belayer", "config.yaml"), "worktrees: false\n")
		slug, err := ResolveCragSlug(dir)
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != localCragSlug {
			t.Errorf("slug = %q, want %q", slug, localCragSlug)
		}
	})

	t.Run("config file with crag.name quoted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".belayer", "config.yaml"), "crag:\n  name: \"software-co\"\n")
		slug, err := ResolveCragSlug(dir)
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != "software-co" {
			t.Errorf("slug = %q, want %q", slug, "software-co")
		}
	})

	t.Run("config file with crag.name unquoted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".belayer", "config.yaml"), "crag:\n  name: myproject\n")
		slug, err := ResolveCragSlug(dir)
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != "myproject" {
			t.Errorf("slug = %q, want %q", slug, "myproject")
		}
	})

	t.Run("empty projectDir returns local", func(t *testing.T) {
		t.Parallel()
		slug, err := ResolveCragSlug("")
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != localCragSlug {
			t.Errorf("slug = %q, want %q", slug, localCragSlug)
		}
	})

	t.Run("config file with empty crag.name returns local", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".belayer", "config.yaml"), "crag:\n  name: \"\"\n")
		slug, err := ResolveCragSlug(dir)
		if err != nil {
			t.Fatalf("ResolveCragSlug: %v", err)
		}
		if slug != localCragSlug {
			t.Errorf("empty name: slug = %q, want %q", slug, localCragSlug)
		}
	})
}
