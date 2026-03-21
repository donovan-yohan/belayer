package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/model"
)

func makeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "node-complete-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestWriteCompletionFile(t *testing.T) {
	dir := makeTempDir(t)

	want := model.CompletionResult{
		Outcome:    model.OutcomePass,
		OutputPath: "/some/output.md",
		TargetNode: "",
		Feedback:   "looks good",
		Attempt:    2,
	}

	if err := writeCompletionFile(dir, "task-abc", "spotter", want); err != nil {
		t.Fatalf("writeCompletionFile: %v", err)
	}

	path := completionFilePath(dir, "task-abc", "spotter", 2)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read completion file: %v", err)
	}

	var got model.CompletionResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != want.Outcome {
		t.Errorf("outcome: got %s, want %s", got.Outcome, want.Outcome)
	}
	if got.OutputPath != want.OutputPath {
		t.Errorf("output_path: got %s, want %s", got.OutputPath, want.OutputPath)
	}
	if got.Feedback != want.Feedback {
		t.Errorf("feedback: got %s, want %s", got.Feedback, want.Feedback)
	}
	if got.Attempt != want.Attempt {
		t.Errorf("attempt: got %d, want %d", got.Attempt, want.Attempt)
	}
}

func TestCompletionFilePath_AttemptScoped(t *testing.T) {
	dir := makeTempDir(t)

	path1 := completionFilePath(dir, "task-xyz", "lead", 1)
	path2 := completionFilePath(dir, "task-xyz", "lead", 2)

	if path1 == path2 {
		t.Errorf("attempt 1 and attempt 2 should produce different paths")
	}

	if !strings.Contains(filepath.Base(path1), "attempt-1") {
		t.Errorf("path1 should contain 'attempt-1', got %s", filepath.Base(path1))
	}
	if !strings.Contains(filepath.Base(path2), "attempt-2") {
		t.Errorf("path2 should contain 'attempt-2', got %s", filepath.Base(path2))
	}
}
