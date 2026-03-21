package model

import (
	"encoding/json"
	"testing"
)

func TestNodeOutcomeIsValid(t *testing.T) {
	valid := []NodeOutcome{OutcomePass, OutcomeRetry, OutcomeFail}
	for _, o := range valid {
		if !o.IsValid() {
			t.Errorf("expected %q to be valid", o)
		}
	}

	invalid := []NodeOutcome{"", "pass", "UNKNOWN", "fail"}
	for _, o := range invalid {
		if o.IsValid() {
			t.Errorf("expected %q to be invalid", o)
		}
	}
}

func TestCompletionResultJSONRoundTrip(t *testing.T) {
	original := CompletionResult{
		Outcome:    OutcomePass,
		OutputPath: "/tmp/output.json",
		Feedback:   "looks good",
		Attempt:    2,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded CompletionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Outcome != original.Outcome {
		t.Errorf("Outcome: got %q, want %q", decoded.Outcome, original.Outcome)
	}
	if decoded.OutputPath != original.OutputPath {
		t.Errorf("OutputPath: got %q, want %q", decoded.OutputPath, original.OutputPath)
	}
	if decoded.Feedback != original.Feedback {
		t.Errorf("Feedback: got %q, want %q", decoded.Feedback, original.Feedback)
	}
	if decoded.Attempt != original.Attempt {
		t.Errorf("Attempt: got %d, want %d", decoded.Attempt, original.Attempt)
	}
	if decoded.TargetNode != "" {
		t.Errorf("TargetNode should be empty (omitempty), got %q", decoded.TargetNode)
	}
}

func TestCompletionResultRetryWithTargetNode(t *testing.T) {
	retry := CompletionResult{
		Outcome:    OutcomeRetry,
		TargetNode: "lint",
		Feedback:   "failed linting, retry from lint node",
		Attempt:    1,
	}

	data, err := json.Marshal(retry)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded CompletionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Outcome != OutcomeRetry {
		t.Errorf("Outcome: got %q, want %q", decoded.Outcome, OutcomeRetry)
	}
	if decoded.TargetNode != "lint" {
		t.Errorf("TargetNode: got %q, want %q", decoded.TargetNode, "lint")
	}
	if decoded.Attempt != 1 {
		t.Errorf("Attempt: got %d, want 1", decoded.Attempt)
	}
}

func TestClimbStatusIsValid(t *testing.T) {
	valid := []ClimbStatus{ClimbActive, ClimbCompleted, ClimbFailed}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []ClimbStatus{"", "Active", "COMPLETED", "unknown"}
	for _, s := range invalid {
		if s.IsValid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}
