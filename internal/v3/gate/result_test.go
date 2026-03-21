package gate

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func TestParseGateResult_Valid(t *testing.T) {
	data := []byte(`{
		"gate": "review",
		"attempt": 1,
		"dimensions": {
			"correctness": {"score": 8, "rationale": "solid", "issues": []},
			"quality": {"score": 7, "rationale": "clean", "issues": ["minor naming"]}
		},
		"weighted_score": 7.5,
		"outcome": "PASS",
		"summary": "Good work"
	}`)

	result, err := ParseGateResult(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate != "review" {
		t.Errorf("Gate: got %q, want %q", result.Gate, "review")
	}
	if len(result.Dimensions) != 2 {
		t.Fatalf("Dimensions: got %d, want 2", len(result.Dimensions))
	}
	if result.Dimensions["correctness"].Score != 8 {
		t.Errorf("correctness score: got %f, want 8", result.Dimensions["correctness"].Score)
	}
}

func TestParseGateResult_InvalidJSON(t *testing.T) {
	_, err := ParseGateResult([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateGateResult_MissingDimension(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8, Rationale: "ok"},
		},
	}
	err := ValidateGateResult(result, []string{"correctness", "quality"})
	if err == nil {
		t.Fatal("expected error for missing dimension")
	}
}

func TestValidateGateResult_AllPresent(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8, Rationale: "ok"},
			"quality":     {Score: 7, Rationale: "ok"},
		},
	}
	if err := ValidateGateResult(result, []string{"correctness", "quality"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClampScore(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-1, 0},
		{0, 0},
		{5, 5},
		{10, 10},
		{11, 10},
	}
	for _, tt := range tests {
		got := clampScore(tt.in)
		if got != tt.want {
			t.Errorf("clampScore(%f): got %f, want %f", tt.in, got, tt.want)
		}
	}
}

func TestComputeWeightedScore(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8},
			"quality":     {Score: 6},
		},
	}
	dims := []pipeline.DimensionConfig{
		{Name: "correctness", Weight: 0.6},
		{Name: "quality", Weight: 0.4},
	}
	// 8*0.6 + 6*0.4 = 4.8 + 2.4 = 7.2
	got := ComputeWeightedScore(result, dims)
	if got < 7.19 || got > 7.21 {
		t.Errorf("weighted score: got %f, want ~7.2", got)
	}
}

func TestComputeWeightedScore_ClampsOutOfRange(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"a": {Score: 12}, // out of range
		},
	}
	dims := []pipeline.DimensionConfig{
		{Name: "a", Weight: 1.0},
	}
	got := ComputeWeightedScore(result, dims)
	if got != 10.0 {
		t.Errorf("expected clamped score 10.0, got %f", got)
	}
}

func TestApplyThresholds(t *testing.T) {
	thresholds := pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0}

	tests := []struct {
		score float64
		want  model.NodeOutcome
	}{
		{8.0, model.OutcomePass},
		{7.0, model.OutcomePass},  // exactly at pass
		{6.9, model.OutcomeRetry}, // below pass, above retry
		{4.0, model.OutcomeRetry}, // exactly at retry
		{3.9, model.OutcomeFail},  // below retry
		{0.0, model.OutcomeFail},
	}
	for _, tt := range tests {
		got := ApplyThresholds(tt.score, thresholds)
		if got != tt.want {
			t.Errorf("ApplyThresholds(%f): got %q, want %q", tt.score, got, tt.want)
		}
	}
}
