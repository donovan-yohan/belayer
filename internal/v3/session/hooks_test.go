package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHooksConfig_CreatesFile(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-abc"
	nodeName := "coder"
	attempt := 1

	if err := WriteHooksConfig(workDir, taskID, nodeName, attempt); err != nil {
		t.Fatalf("WriteHooksConfig returned error: %v", err)
	}

	path := filepath.Join(workDir, ".belayer", "hooks.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected hooks.json to exist at %s", path)
	}
}

func TestWriteHooksConfig_ValidJSON(t *testing.T) {
	workDir := t.TempDir()

	if err := WriteHooksConfig(workDir, "task-xyz", "reviewer", 2); err != nil {
		t.Fatalf("WriteHooksConfig returned error: %v", err)
	}

	path := filepath.Join(workDir, ".belayer", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
}

func TestWriteHooksConfig_StopHookPresent(t *testing.T) {
	workDir := t.TempDir()

	if err := WriteHooksConfig(workDir, "task-xyz", "reviewer", 2); err != nil {
		t.Fatalf("WriteHooksConfig returned error: %v", err)
	}

	path := filepath.Join(workDir, ".belayer", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}

	var parsed struct {
		Hooks map[string]interface{} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse hooks.json: %v", err)
	}

	if _, ok := parsed.Hooks["Stop"]; !ok {
		t.Fatal("expected a 'Stop' key under 'hooks'")
	}
}

func TestWriteHooksConfig_CommandContainsNodeComplete(t *testing.T) {
	workDir := t.TempDir()
	taskID := "task-123"
	nodeName := "planner"

	if err := WriteHooksConfig(workDir, taskID, nodeName, 3); err != nil {
		t.Fatalf("WriteHooksConfig returned error: %v", err)
	}

	path := filepath.Join(workDir, ".belayer", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "node-complete") {
		t.Error("expected command to contain 'node-complete'")
	}
	if !strings.Contains(content, taskID) {
		t.Errorf("expected command to contain task ID %q", taskID)
	}
	if !strings.Contains(content, nodeName) {
		t.Errorf("expected command to contain node name %q", nodeName)
	}
}

func TestHooksConfigPath(t *testing.T) {
	workDir := "/some/work/dir"
	got := HooksConfigPath(workDir)
	want := filepath.Join(workDir, ".belayer", "hooks.json")
	if got != want {
		t.Errorf("HooksConfigPath = %q, want %q", got, want)
	}
}
