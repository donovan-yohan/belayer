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
	if !strings.HasPrefix(capturedProfile, "belayer-") {
		t.Errorf("fork profile name %q does not start with 'belayer-'", capturedProfile)
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
		if strings.HasPrefix(e.Name(), "belayer-") {
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
		if strings.HasPrefix(e.Name(), "belayer-") {
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
