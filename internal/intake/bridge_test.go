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
