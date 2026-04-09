package session

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadTemplate
// ---------------------------------------------------------------------------

func TestLoadTemplate_Explore(t *testing.T) {
	tmpl, err := LoadTemplate("explore")
	if err != nil {
		t.Fatalf("LoadTemplate(\"explore\") unexpected error: %v", err)
	}
	if tmpl.Name != "explore" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "explore")
	}
	if tmpl.Phase != PhaseExplore {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseExplore)
	}
	if len(tmpl.Agents) != 1 {
		t.Errorf("Agents: got %d, want 1", len(tmpl.Agents))
	}
}

func TestLoadTemplate_Climb(t *testing.T) {
	tmpl, err := LoadTemplate("climb")
	if err != nil {
		t.Fatalf("LoadTemplate(\"climb\") unexpected error: %v", err)
	}
	if tmpl.Name != "climb" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "climb")
	}
	if tmpl.Phase != PhaseClimb {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseClimb)
	}
	if len(tmpl.Agents) != 3 {
		t.Errorf("Agents: got %d, want 3", len(tmpl.Agents))
	}
	// Must include a pilot.
	var hasPilot bool
	for _, a := range tmpl.Agents {
		if a.Role == "pilot" {
			hasPilot = true
			break
		}
	}
	if !hasPilot {
		t.Error("climb template missing agent with role \"pilot\"")
	}
}

func TestLoadTemplate_Summit(t *testing.T) {
	tmpl, err := LoadTemplate("summit")
	if err != nil {
		t.Fatalf("LoadTemplate(\"summit\") unexpected error: %v", err)
	}
	if tmpl.Name != "summit" {
		t.Errorf("Name: got %q, want %q", tmpl.Name, "summit")
	}
	if tmpl.Phase != PhaseSummit {
		t.Errorf("Phase: got %q, want %q", tmpl.Phase, PhaseSummit)
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

func TestValidateTemplate_ValidClimb(t *testing.T) {
	tmpl, err := LoadTemplate("climb")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := ValidateTemplate(tmpl); err != nil {
		t.Errorf("ValidateTemplate(climb) unexpected error: %v", err)
	}
}

func TestValidateTemplate_ValidExplore(t *testing.T) {
	tmpl, err := LoadTemplate("explore")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Explore has no pilot — that must be fine (pilot invariant is climb-only).
	if err := ValidateTemplate(tmpl); err != nil {
		t.Errorf("ValidateTemplate(explore) unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateTemplate — climb trio invariant failures
// ---------------------------------------------------------------------------

func TestValidateTemplate_ClimbMissingPilot(t *testing.T) {
	tmpl, _ := LoadTemplate("climb")
	// Strip the pilot agent.
	var filtered []AgentSpec
	for _, a := range tmpl.Agents {
		if a.Role != "pilot" {
			filtered = append(filtered, a)
		}
	}
	tmpl.Agents = filtered

	err := ValidateTemplate(tmpl)
	if err == nil {
		t.Fatal("ValidateTemplate expected error for missing pilot, got nil")
	}
	if !strings.Contains(err.Error(), "pilot") {
		t.Errorf("error %q should contain \"pilot\"", err.Error())
	}
}

func TestValidateTemplate_ClimbMissingReviewer(t *testing.T) {
	tmpl, _ := LoadTemplate("climb")
	var filtered []AgentSpec
	for _, a := range tmpl.Agents {
		if a.Role != "reviewer" {
			filtered = append(filtered, a)
		}
	}
	tmpl.Agents = filtered

	err := ValidateTemplate(tmpl)
	if err == nil {
		t.Fatal("ValidateTemplate expected error for missing reviewer, got nil")
	}
	if !strings.Contains(err.Error(), "reviewer") {
		t.Errorf("error %q should contain \"reviewer\"", err.Error())
	}
}

func TestValidateTemplate_ClimbMissingImplementer(t *testing.T) {
	tmpl, _ := LoadTemplate("climb")
	var filtered []AgentSpec
	for _, a := range tmpl.Agents {
		if a.Role != "implementer" {
			filtered = append(filtered, a)
		}
	}
	tmpl.Agents = filtered

	err := ValidateTemplate(tmpl)
	if err == nil {
		t.Fatal("ValidateTemplate expected error for missing implementer, got nil")
	}
	if !strings.Contains(err.Error(), "implementer") {
		t.Errorf("error %q should contain \"implementer\"", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ListTemplates
// ---------------------------------------------------------------------------

func TestListTemplates(t *testing.T) {
	names := ListTemplates()
	want := map[string]bool{"climb": true, "explore": true, "summit": true}
	if len(names) != len(want) {
		t.Fatalf("ListTemplates: got %d names, want %d", len(names), len(want))
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("ListTemplates: unexpected name %q", n)
		}
	}
}
