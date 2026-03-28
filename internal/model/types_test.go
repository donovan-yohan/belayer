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

func TestClimbInput_NewFields(t *testing.T) {
	// Round-trip with new fields populated.
	original := ClimbInput{
		Description: "test climb",
		WorkDir:     "/tmp/work",
		Branch:      "feature-branch",
		Repos:       []string{"repo-a", "repo-b"},
		BaseRef:     "main",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ClimbInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Repos) != len(original.Repos) {
		t.Fatalf("Repos length: got %d, want %d", len(decoded.Repos), len(original.Repos))
	}
	for i, r := range original.Repos {
		if decoded.Repos[i] != r {
			t.Errorf("Repos[%d]: got %q, want %q", i, decoded.Repos[i], r)
		}
	}
	if decoded.BaseRef != original.BaseRef {
		t.Errorf("BaseRef: got %q, want %q", decoded.BaseRef, original.BaseRef)
	}

	// Verify omitempty: fields absent from JSON when zero.
	empty := ClimbInput{
		Description: "minimal",
		WorkDir:     "/tmp/work",
		Branch:      "main",
	}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	jsonStr := string(emptyData)
	if contains(jsonStr, "repos") {
		t.Errorf("expected 'repos' to be omitted when empty, got: %s", jsonStr)
	}
	if contains(jsonStr, "base_ref") {
		t.Errorf("expected 'base_ref' to be omitted when empty, got: %s", jsonStr)
	}
}

func TestCompletionResult_NewFields(t *testing.T) {
	// Round-trip with new fields populated.
	original := CompletionResult{
		Outcome:   OutcomePass,
		Attempt:   1,
		CommitSHA: "abc123def456",
		BaseRef:   "main",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded CompletionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.CommitSHA != original.CommitSHA {
		t.Errorf("CommitSHA: got %q, want %q", decoded.CommitSHA, original.CommitSHA)
	}
	if decoded.BaseRef != original.BaseRef {
		t.Errorf("BaseRef: got %q, want %q", decoded.BaseRef, original.BaseRef)
	}

	// Verify omitempty: fields absent from JSON when zero.
	empty := CompletionResult{
		Outcome: OutcomePass,
		Attempt: 1,
	}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	jsonStr := string(emptyData)
	if contains(jsonStr, "commit_sha") {
		t.Errorf("expected 'commit_sha' to be omitted when empty, got: %s", jsonStr)
	}
	if contains(jsonStr, "base_ref") {
		t.Errorf("expected 'base_ref' to be omitted when empty, got: %s", jsonStr)
	}
}

// contains is a simple substring check used in tests to avoid importing strings.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})())
}
