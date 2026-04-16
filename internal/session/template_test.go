package session

import (
	"testing"

	yamlPkg "gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// LoadTemplate
// ---------------------------------------------------------------------------

func TestLoadTemplate_Intake(t *testing.T) {
	tmpl, err := LoadTemplate("intake")
	if err != nil {
		t.Fatalf("LoadTemplate(\"intake\") unexpected error: %v", err)
	}
	if tmpl.Name != "intake" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "intake")
	}
	if tmpl.Phase != PhaseIntake {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseIntake)
	}
	if len(tmpl.Agents) != 1 {
		t.Errorf("Agents: got %d, want 1", len(tmpl.Agents))
	}
}

func TestLoadTemplate_Implement(t *testing.T) {
	tmpl, err := LoadTemplate("implement")
	if err != nil {
		t.Fatalf("LoadTemplate(\"implement\") unexpected error: %v", err)
	}
	if tmpl.Name != "implement" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "implement")
	}
	if tmpl.Phase != PhaseImplement {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseImplement)
	}
	if len(tmpl.Agents) != 3 {
		t.Errorf("Agents: got %d, want 3", len(tmpl.Agents))
	}
	// Must include a pilot.
	var hasPilot bool
	for _, a := range tmpl.Agents {
		if a.Name == "pilot" {
			hasPilot = true
			break
		}
	}
	if !hasPilot {
		t.Error("implement template missing agent named \"pilot\"")
	}
}

func TestLoadTemplate_Deliver(t *testing.T) {
	tmpl, err := LoadTemplate("deliver")
	if err != nil {
		t.Fatalf("LoadTemplate(\"deliver\") unexpected error: %v", err)
	}
	if tmpl.Name != "deliver" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "deliver")
	}
	if tmpl.Phase != PhaseDeliver {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseDeliver)
	}
	if len(tmpl.Agents) != 3 {
		t.Errorf("Agents: got %d, want 3", len(tmpl.Agents))
	}
}

func TestLoadTemplate_Unknown(t *testing.T) {
	_, err := LoadTemplate("unknown")
	if err == nil {
		t.Fatal("LoadTemplate(\"unknown\") expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// ValidateTemplate — valid cases
// ---------------------------------------------------------------------------

func TestValidateTemplate_ValidImplement(t *testing.T) {
	tmpl, err := LoadTemplate("implement")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ValidateTemplate(tmpl); err != nil {
		t.Errorf("ValidateTemplate(implement) unexpected error: %v", err)
	}
}

func TestValidateTemplate_ValidIntake(t *testing.T) {
	tmpl, err := LoadTemplate("intake")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Intake has no pilot — that must be fine (pilot invariant is implement-only).
	if err := ValidateTemplate(tmpl); err != nil {
		t.Errorf("ValidateTemplate(intake) unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateTemplate — error cases
// ---------------------------------------------------------------------------

func TestValidateTemplate_EmptyName(t *testing.T) {
	tmpl := SessionTemplate{Agents: []AgentSpec{{Name: "a", Vendor: "claude"}}}
	if err := ValidateTemplate(tmpl); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateTemplate_NoAgents(t *testing.T) {
	tmpl := SessionTemplate{Name: "test"}
	if err := ValidateTemplate(tmpl); err == nil {
		t.Fatal("expected error for no agents")
	}
}

func TestValidateTemplate_AgentMissingVendor(t *testing.T) {
	tmpl := SessionTemplate{Name: "test", Agents: []AgentSpec{{Name: "a"}}}
	if err := ValidateTemplate(tmpl); err == nil {
		t.Fatal("expected error for agent with no vendor")
	}
}

func TestValidateTemplate_InvalidAgentName(t *testing.T) {
	tmpl := SessionTemplate{
		Name: "test",
		Agents: []AgentSpec{
			{Name: "; rm -rf /", Vendor: "claude"},
		},
	}
	if err := ValidateTemplate(tmpl); err == nil {
		t.Error("expected error for invalid agent name, got nil")
	}
}

func TestValidateTemplate_InvalidEnvKey(t *testing.T) {
	tmpl := SessionTemplate{
		Name: "test",
		Agents: []AgentSpec{
			{Name: "pilot", Vendor: "claude", Env: map[string]string{
				"GOOD_KEY": "value",
				"BAD;KEY":  "value",
			}},
		},
	}
	if err := ValidateTemplate(tmpl); err == nil {
		t.Error("expected error for invalid env key, got nil")
	}
}

func TestValidateTemplate_ValidAgentName(t *testing.T) {
	tmpl := SessionTemplate{
		Name: "test",
		Agents: []AgentSpec{
			{Name: "api-implementer", Vendor: "claude"},
			{Name: "pilot.v2", Vendor: "opencode"},
		},
	}
	if err := ValidateTemplate(tmpl); err != nil {
		t.Errorf("expected no error for valid agent names, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListTemplates
// ---------------------------------------------------------------------------

func TestListTemplates(t *testing.T) {
	names := ListTemplates()
	want := map[string]bool{"implement": true, "intake": true, "deliver": true}
	if len(names) != len(want) {
		t.Fatalf("ListTemplates: got %d names, want %d", len(names), len(want))
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("ListTemplates: unexpected name %q", n)
		}
	}
}

func TestAgentSpec_RepoField(t *testing.T) {
	yamlStr := `
name: fullstack
phase: implement
description: Multi-repo session
agents:
  - name: pilot
    vendor: claude
    model: opus
  - name: api-impl
    vendor: claude
    model: sonnet
    repo: extend-api
  - name: app-impl
    vendor: claude
    model: sonnet
    repo: extend-app
`
	var tmpl SessionTemplate
	if err := yamlPkg.Unmarshal([]byte(yamlStr), &tmpl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if tmpl.Agents[0].Repo != "" {
		t.Errorf("pilot should have empty repo, got %q", tmpl.Agents[0].Repo)
	}
	if tmpl.Agents[1].Repo != "extend-api" {
		t.Errorf("api-impl should have repo 'extend-api', got %q", tmpl.Agents[1].Repo)
	}
	if tmpl.Agents[2].Repo != "extend-app" {
		t.Errorf("app-impl should have repo 'extend-app', got %q", tmpl.Agents[2].Repo)
	}
}

func TestAgentSpec_TierField(t *testing.T) {
	yamlStr := `
name: tiered
phase: implement
description: Tiered agents
agents:
  - name: pilot
    vendor: claude
    model: opus
    tier: main
  - name: reviewer
    vendor: claude
    model: sonnet
    tier: ephemeral
`
	var tmpl SessionTemplate
	if err := yamlPkg.Unmarshal([]byte(yamlStr), &tmpl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tmpl.Agents[0].Tier != "main" {
		t.Fatalf("pilot tier = %q, want main", tmpl.Agents[0].Tier)
	}
	if tmpl.Agents[1].Tier != "ephemeral" {
		t.Fatalf("reviewer tier = %q, want ephemeral", tmpl.Agents[1].Tier)
	}
}
