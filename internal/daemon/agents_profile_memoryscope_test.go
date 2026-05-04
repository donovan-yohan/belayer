package daemon

// Tests for Phase 3.A: reading talent.yaml#memory.scope at identity load time
// and passing it through to MaterializeOptions.MemoryScope during
// materializeBridgeProfile.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/bridge"
)

// ── loadAgentIdentity unit tests ─────────────────────────────────────────────

// TestLoadAgentIdentity_TalentYAML_MemoryScopeCrag verifies that
// loadAgentIdentity reads memory.scope from talent.yaml and populates
// agentIdentity.MemoryScope.
func TestLoadAgentIdentity_TalentYAML_MemoryScopeCrag(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "qa", "system-prompt.md"), "you are qa")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "qa", "agent.yaml"), "kind: side\n")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "qa", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: crag\n")

	got := loadAgentIdentity("", belayerRoot, "qa", "")
	if got.MemoryScope != "crag" {
		t.Errorf("MemoryScope = %q, want %q", got.MemoryScope, "crag")
	}
}

// TestLoadAgentIdentity_TalentYAML_MemoryScopeTalent verifies the "talent"
// scope value is parsed correctly.
func TestLoadAgentIdentity_TalentYAML_MemoryScopeTalent(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "backend-dev", "system-prompt.md"), "you are backend-dev")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "backend-dev", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: talent\n")

	got := loadAgentIdentity("", belayerRoot, "backend-dev", "")
	if got.MemoryScope != "talent" {
		t.Errorf("MemoryScope = %q, want %q", got.MemoryScope, "talent")
	}
}

// TestLoadAgentIdentity_NoTalentYAML_MemoryScopeDefaultsEmpty verifies that
// when talent.yaml is absent, MemoryScope is left empty (callers default to
// "climb").
func TestLoadAgentIdentity_NoTalentYAML_MemoryScopeDefaultsEmpty(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "supervisor", "system-prompt.md"), "you are supervisor")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "supervisor", "agent.yaml"), "kind: main\n")
	// No talent.yaml.

	got := loadAgentIdentity("", belayerRoot, "supervisor", "")
	if got.MemoryScope != "" {
		t.Errorf("MemoryScope = %q, want empty string when talent.yaml is absent", got.MemoryScope)
	}
}

// TestLoadAgentIdentity_TalentYAML_MissingMemoryScope verifies that when
// talent.yaml exists but lacks the memory.scope field, MemoryScope is empty.
func TestLoadAgentIdentity_TalentYAML_MissingMemoryScope(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "system-prompt.md"), "you are reviewer")
	// talent.yaml present but no memory block.
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nretention:\n  scope: crag\n")

	got := loadAgentIdentity("", belayerRoot, "reviewer", "")
	if got.MemoryScope != "" {
		t.Errorf("MemoryScope = %q, want empty string when memory.scope is absent", got.MemoryScope)
	}
}

// TestLoadAgentIdentity_TalentYAML_InvalidMemoryScope verifies graceful
// handling of an unrecognised scope value: MemoryScope stays empty and the
// spawn is not hard-failed. The caller (materializeBridgeProfile) passes an
// empty string to MaterializeProfile which then defaults to "climb".
func TestLoadAgentIdentity_TalentYAML_InvalidMemoryScope(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "pm", "system-prompt.md"), "you are pm")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "pm", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: unlimited\n")

	got := loadAgentIdentity("", belayerRoot, "pm", "")
	if got.MemoryScope != "" {
		t.Errorf("MemoryScope = %q, want empty string for invalid scope (graceful default)", got.MemoryScope)
	}
}

// TestLoadAgentIdentity_TalentYAML_ProjectLocalOverridesShipped verifies that
// a project-local talent.yaml takes precedence over a shipped default (mirrors
// the agent.yaml override contract).
func TestLoadAgentIdentity_TalentYAML_ProjectLocalOverridesShipped(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "project")
	belayerRoot := filepath.Join(root, "shipped")

	mustWrite(t, filepath.Join(belayerRoot, "agents", "web-dev", "system-prompt.md"), "shipped prompt")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "web-dev", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: climb\n")

	// Project-local overrides scope to "crag".
	mustWrite(t, filepath.Join(workdir, ".belayer", "agents", "web-dev", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: crag\n")

	got := loadAgentIdentity(workdir, belayerRoot, "web-dev", "")
	if got.MemoryScope != "crag" {
		t.Errorf("MemoryScope = %q, want %q (project-local should win)", got.MemoryScope, "crag")
	}
}

// ── Integration tests: materializeBridgeProfile → .belayer-talent.yaml ───────

// TestSpawnProfile_TalentYAMLScopeCrag verifies that when an agent identity
// includes a talent.yaml with memory.scope=crag, the forked profile's
// .belayer-talent.yaml records memory_scope: crag.
func TestSpawnProfile_TalentYAMLScopeCrag(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()

	// Write a project-local identity with talent.yaml scope=crag.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "system-prompt.md"), "you are supervisor")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: crag\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "scope-crag-test",
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

	// The fork profile dir must exist and .belayer-talent.yaml must record
	// memory_scope: crag.
	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml from fork %s: %v", capturedProfile, err)
	}
	content := string(data)
	if !strings.Contains(content, "memory_scope: crag") {
		t.Errorf(".belayer-talent.yaml should have memory_scope: crag, got:\n%s", content)
	}
}

// TestSpawnProfile_NoTalentYAMLDefaultsToClimb verifies that when no
// talent.yaml is present, the forked profile defaults to memory_scope: climb.
func TestSpawnProfile_NoTalentYAMLDefaultsToClimb(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	// No talent.yaml — supervisor uses shipped identity (also without talent.yaml).

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "no-talent-yaml-test",
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

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml from fork %s: %v", capturedProfile, err)
	}
	if !strings.Contains(string(data), "memory_scope: climb") {
		t.Errorf(".belayer-talent.yaml should have memory_scope: climb (default), got:\n%s", string(data))
	}
}

// TestSpawnProfile_TalentYAMLMissingScopeDefaultsToClimb verifies that when
// talent.yaml is present but has no memory.scope field, the fork defaults to
// memory_scope: climb.
func TestSpawnProfile_TalentYAMLMissingScopeDefaultsToClimb(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "system-prompt.md"), "you are supervisor")
	// talent.yaml present but no memory block.
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nretention:\n  scope: crag\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "missing-scope-test",
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

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml from fork %s: %v", capturedProfile, err)
	}
	if !strings.Contains(string(data), "memory_scope: climb") {
		t.Errorf(".belayer-talent.yaml should have memory_scope: climb (default), got:\n%s", string(data))
	}
}

// TestSpawnProfile_TalentYAMLInvalidScopeDefaultsToClimb verifies graceful
// handling: an invalid scope value in talent.yaml is silently ignored and the
// fork defaults to memory_scope: climb (spawn is not hard-failed).
func TestSpawnProfile_TalentYAMLInvalidScopeDefaultsToClimb(t *testing.T) {
	profilesRoot, _ := setupBaseBelayerProfile(t)

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "system-prompt.md"), "you are supervisor")
	mustWrite(t, filepath.Join(workspace, ".belayer", "agents", "supervisor", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: unlimited\n")

	d := testDaemon(t)
	sessRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:         "invalid-scope-test",
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
		t.Fatalf("spawn agent (invalid scope): %d %s — should not hard-fail", rr.Code, rr.Body.String())
	}

	// Fork must still be created and default to climb.
	if !strings.HasPrefix(capturedProfile, "blyr-") {
		t.Fatalf("expected fork profile name, got %q", capturedProfile)
	}

	forkDir := filepath.Join(profilesRoot, capturedProfile)
	data, err := os.ReadFile(filepath.Join(forkDir, ".belayer-talent.yaml"))
	if err != nil {
		t.Fatalf("read .belayer-talent.yaml from fork %s: %v", capturedProfile, err)
	}
	if !strings.Contains(string(data), "memory_scope: climb") {
		t.Errorf(".belayer-talent.yaml should have memory_scope: climb (invalid scope graceful default), got:\n%s", string(data))
	}
}
