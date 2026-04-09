package session

import (
	"testing"
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
	if len(tmpl.Agents) != 2 {
		t.Errorf("Agents: got %d, want 2", len(tmpl.Agents))
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
