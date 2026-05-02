package generatedtalent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRecordFallsBackToDirectoryID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated-talents", "mara-underbough", "talent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir record dir: %v", err)
	}
	raw := []byte(`schema_version: belayer-generated-talent/v1
domain: story
role: tavernkeep
lifecycle: resumable
status: generated
source_request: turn-0002
reason: scene needs a reusable local authority
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}

	record, err := ReadRecord(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if record.ID != "mara-underbough" {
		t.Fatalf("record.ID = %q, want mara-underbough", record.ID)
	}
}

func TestValidateRecordReturnsDeterministicInputErrors(t *testing.T) {
	err := ValidateRecord(Record{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsInputError(err) {
		t.Fatalf("expected input error, got %T %[1]v", err)
	}
	if got, want := err.Error(), "id is required"; got != want {
		t.Fatalf("validation error = %q, want %q", got, want)
	}
}

func TestValidateRecordRequiresPromotionEvidenceForPromotedStatus(t *testing.T) {
	record := Record{
		ID:            "mara-underbough",
		Domain:        "story",
		Role:          "tavernkeep",
		Lifecycle:     "resumable",
		Status:        "promoted",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
	}
	err := ValidateRecord(record)
	if err == nil {
		t.Fatal("expected promoted record without evidence to fail")
	}
	if !IsInputError(err) {
		t.Fatalf("expected input error, got %T %[1]v", err)
	}
	if !strings.Contains(err.Error(), "promotion_evidence is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInputErrorClassificationSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("wrap: %w", ValidateRecord(Record{
		ID:            "../escape",
		Domain:        "story",
		Role:          "tavernkeep",
		SourceRequest: "turn-0002",
		Reason:        "scene needs a reusable local authority",
	}))
	if !IsInputError(err) {
		t.Fatalf("expected wrapped validation error to classify as input: %v", err)
	}
}
