package temporal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/v3/gate"
	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// --- helpers ---

func writeCompletionFile(t *testing.T, workDir, taskID, nodeName string, attempt int, result model.CompletionResult) {
	t.Helper()
	dir := filepath.Join(workDir, ".belayer", "completion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, _ := json.Marshal(result)
	filename := filepath.Join(dir, taskID+"-"+nodeName+"-attempt-"+strconv.Itoa(attempt)+".json")
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatalf("write completion file: %v", err)
	}
}


// --- tests ---

func TestNodeActivity_DetectsCompletionFile(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-001"
	nodeName := "coder"
	attempt := 1

	want := model.CompletionResult{
		Outcome: model.OutcomePass,
		Attempt: attempt,
	}
	writeCompletionFile(t, workDir, taskID, nodeName, attempt, want)

	got, err := readCompletionFile(workDir, taskID, nodeName, attempt)
	if err != nil {
		t.Fatalf("readCompletionFile returned error: %v", err)
	}
	if got.Outcome != want.Outcome {
		t.Errorf("outcome = %q, want %q", got.Outcome, want.Outcome)
	}
	if got.Attempt != want.Attempt {
		t.Errorf("attempt = %d, want %d", got.Attempt, want.Attempt)
	}
}

func TestNodeActivity_CompletionFileNotFound(t *testing.T) {
	workDir := t.TempDir()
	_, err := readCompletionFile(workDir, "no-task", "no-node", 0)
	if err == nil {
		t.Fatal("expected error when completion file does not exist, got nil")
	}
}

func TestNodeActivity_CleansStaleCompletionFiles(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-002"
	nodeName := "reviewer"

	// Write attempt 1 file (stale relative to attempt 2)
	writeCompletionFile(t, workDir, taskID, nodeName, 1, model.CompletionResult{Outcome: model.OutcomeRetry, Attempt: 1})

	// Also write attempt 2 file so we can confirm it's untouched
	writeCompletionFile(t, workDir, taskID, nodeName, 2, model.CompletionResult{Outcome: model.OutcomePass, Attempt: 2})

	if err := cleanStaleCompletionFiles(workDir, taskID, nodeName, 2); err != nil {
		t.Fatalf("cleanStaleCompletionFiles: %v", err)
	}

	// Attempt 1 should be gone.
	staleFile := filepath.Join(workDir, ".belayer", "completion", taskID+"-"+nodeName+"-attempt-1.json")
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Error("expected attempt-1.json to be removed")
	}

	// Attempt 2 should remain.
	currentFile := filepath.Join(workDir, ".belayer", "completion", taskID+"-"+nodeName+"-attempt-2.json")
	if _, err := os.Stat(currentFile); os.IsNotExist(err) {
		t.Error("expected attempt-2.json to remain")
	}
}

func TestPrepareNodeInputs_DesignDocInput(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "designer",
		Input: pipeline.InputConfig{
			Type: "file",
			Key:  "design",
		},
	}
	artifacts := map[string]string{
		"design": "/tmp/design.md",
	}

	prompt := buildInputPrompt(node, artifacts, "/tmp/work")
	if !strings.Contains(prompt, "/tmp/design.md") {
		t.Errorf("expected artifact path in prompt, got: %s", prompt)
	}
}

func TestPrepareNodeInputs_CodeInput(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "coder",
		Input: pipeline.InputConfig{
			Type: "code",
		},
	}

	prompt := buildInputPrompt(node, nil, "/tmp/work")
	if !strings.Contains(strings.ToLower(prompt), "diff") {
		t.Errorf("expected 'diff' in code input prompt, got: %s", prompt)
	}
}

func TestPollForCompletion_Timeout(t *testing.T) {
	workDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pollForCompletion(ctx, workDir, "task-xyz", "coder", 1, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context to be done")
	}
}

func TestBuildInputPrompt_GateNode(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Input: pipeline.InputConfig{
			Type: "code",
		},
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Description: "works?", Weight: 0.6},
			{Name: "quality", Description: "clean?", Weight: 0.4},
		},
	}

	prompt := buildInputPrompt(node, nil, "/tmp/work")
	if !strings.Contains(prompt, "correctness") {
		t.Error("gate prompt should include dimension names")
	}
	if !strings.Contains(prompt, "gate-result-attempt-0.json") {
		t.Error("gate prompt should mention attempt-scoped gate-result file")
	}
}

func testGateNode() pipeline.NodeConfig {
	return pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 0.6},
			{Name: "quality", Weight: 0.4},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}
}

// writeGateOutputs writes gate-result.json and rationale.md at attempt-scoped paths.
func writeGateOutputs(t *testing.T, workDir string, node pipeline.NodeConfig, attempt int, gateJSON, rationale string) {
	t.Helper()
	outputDir := filepath.Join(workDir, ".belayer", "output")
	os.MkdirAll(outputDir, 0o755)
	resultPath := gate.ScopedPath(node.Output.Path, attempt)
	os.WriteFile(filepath.Join(workDir, resultPath), []byte(gateJSON), 0o644)
	if rationale != "" {
		rationalePath := gate.ScopedPath(node.Output.RationalePath, attempt)
		os.WriteFile(filepath.Join(workDir, rationalePath), []byte(rationale), 0o644)
	}
}

func TestProcessGateResult_Pass(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()
	writeGateOutputs(t, workDir, node, 0, `{
		"gate": "review", "attempt": 0,
		"dimensions": {
			"correctness": {"score": 8, "rationale": "solid", "issues": []},
			"quality": {"score": 7, "rationale": "clean", "issues": []}
		},
		"weighted_score": 7.6, "outcome": "PASS", "summary": "Good"
	}`, "# Review\nLooks good.")

	result, err := processGateResult(workDir, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomePass {
		t.Errorf("outcome: got %q, want PASS", result.Outcome)
	}
}

func TestProcessGateResult_Retry(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()
	writeGateOutputs(t, workDir, node, 0, `{
		"gate": "review", "attempt": 0,
		"dimensions": {
			"correctness": {"score": 5, "rationale": "issues", "issues": ["bug"]},
			"quality": {"score": 5, "rationale": "messy", "issues": ["style"]}
		},
		"weighted_score": 5.0, "outcome": "RETRY", "summary": "Needs work"
	}`, "# Review\nFix bugs.")

	result, err := processGateResult(workDir, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomeRetry {
		t.Errorf("outcome: got %q, want RETRY", result.Outcome)
	}
	if result.Feedback == "" {
		t.Error("expected feedback to contain rationale")
	}
}

func TestProcessGateResult_Fail(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()
	writeGateOutputs(t, workDir, node, 0, `{
		"gate": "review", "attempt": 0,
		"dimensions": {
			"correctness": {"score": 2, "rationale": "broken", "issues": ["crash"]},
			"quality": {"score": 3, "rationale": "unreadable", "issues": []}
		},
		"weighted_score": 2.4, "outcome": "FAIL", "summary": "Fundamentally broken"
	}`, "# Review\nBroken.")

	result, err := processGateResult(workDir, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomeFail {
		t.Errorf("outcome: got %q, want FAIL", result.Outcome)
	}
}

func TestProcessGateResult_MissingRationale(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()
	node.Dimensions = []pipeline.DimensionConfig{{Name: "correctness", Weight: 1.0}}
	// Write gate-result but NO rationale
	writeGateOutputs(t, workDir, node, 0, `{
		"gate": "review", "attempt": 0,
		"dimensions": {"correctness": {"score": 8, "rationale": "ok", "issues": []}},
		"weighted_score": 8, "outcome": "PASS", "summary": "ok"
	}`, "") // empty rationale → not written

	result, err := processGateResult(workDir, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomeFail {
		t.Errorf("outcome: got %q, want FAIL (missing rationale)", result.Outcome)
	}
}

func TestProcessGateResult_MissingGateResultJSON(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()
	node.Dimensions = []pipeline.DimensionConfig{{Name: "correctness", Weight: 1.0}}

	_, err := processGateResult(workDir, node, 0)
	if err == nil {
		t.Fatal("expected error for missing gate-result.json")
	}
}

func TestProcessGateResult_StaleAttemptIgnored(t *testing.T) {
	workDir := t.TempDir()
	node := testGateNode()

	// Write gate output for attempt 0 (stale)
	writeGateOutputs(t, workDir, node, 0, `{
		"gate": "review", "attempt": 0,
		"dimensions": {
			"correctness": {"score": 9, "rationale": "stale", "issues": []},
			"quality": {"score": 9, "rationale": "stale", "issues": []}
		},
		"weighted_score": 9, "outcome": "PASS", "summary": "Stale"
	}`, "# Stale rationale")

	// Reading attempt 1 should fail — no files at attempt-1 paths
	_, err := processGateResult(workDir, node, 1)
	if err == nil {
		t.Fatal("expected error — attempt 1 gate output should not exist, proving stale attempt 0 is not reused")
	}
}
