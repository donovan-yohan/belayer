package intake_test

import (
	"os"
	"testing"

	"github.com/donovan-yohan/belayer/internal/intake"
)

func TestGenerateWorkflowID(t *testing.T) {
	id := intake.GenerateWorkflowID("my-pipeline", "interactive", "abc-123")
	expected := "my-pipeline/interactive/abc-123"
	if id != expected {
		t.Errorf("GenerateWorkflowID() = %q, want %q", id, expected)
	}
}

func TestGenerateWorkflowID_DifferentInputs(t *testing.T) {
	id1 := intake.GenerateWorkflowID("pipeline-a", "jira", "JIRA-1")
	id2 := intake.GenerateWorkflowID("pipeline-a", "linear", "LIN-42")

	if id1 == id2 {
		t.Errorf("expected different workflow IDs for different source/externalID, got %q for both", id1)
	}
}

func TestGenerateBranchSlug_Normal(t *testing.T) {
	got := intake.GenerateBranchSlug("Implement user authentication system")
	if got != "implement-user-authentication-system" {
		t.Errorf("got %q, want 'implement-user-authentication-system'", got)
	}
}

func TestGenerateBranchSlug_Short(t *testing.T) {
	got := intake.GenerateBranchSlug("Fix bug")
	if got != "fix-bug" {
		t.Errorf("got %q, want 'fix-bug'", got)
	}
}

func TestGenerateBranchSlug_Empty(t *testing.T) {
	got := intake.GenerateBranchSlug("")
	if got != "impl" {
		t.Errorf("got %q, want 'impl'", got)
	}
}

func TestGenerateBranchSlug_SpecialChars(t *testing.T) {
	got := intake.GenerateBranchSlug("Add $review + %{INPUT} support!")
	if got != "add-review-input-support" {
		t.Errorf("got %q, want 'add-review-input-support'", got)
	}
}

func TestGenerateBranchSlug_Long(t *testing.T) {
	got := intake.GenerateBranchSlug("implement the extremely long feature description that goes on and on")
	if len(got) > 40 {
		t.Errorf("slug too long: %d chars, max 40", len(got))
	}
	if got[len(got)-1] == '-' {
		t.Error("slug should not end with a hyphen")
	}
}

func TestResolvePipelineYAML_NoPipeline(t *testing.T) {
	// Use a temp directory with no pipeline YAML files — should return an error.
	dir, err := os.MkdirTemp("", "belayer-intake-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	_, _, err = intake.ResolvePipelineYAML(dir)
	if err == nil {
		t.Fatal("ResolvePipelineYAML() expected error when no pipeline found, got nil")
	}
}
