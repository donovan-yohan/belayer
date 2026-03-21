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
