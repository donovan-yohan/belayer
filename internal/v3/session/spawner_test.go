package session

import (
	"strings"
	"testing"
)

var testOpts = SpawnOpts{
	NodeName:    "coder",
	TaskID:      "abcdef1234567890",
	Attempt:     2,
	WorkDir:     "/tmp/work",
	Description: "write some code",
	HooksPath:   "/tmp/work/.belayer/hooks.json",
	InputPrompt: "implement the feature",
}

func TestWindowName_TruncatesTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "reviewer", TaskID: "abcdef1234567890"}
	got := opts.WindowName()
	want := "reviewer-abcdef12"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}

func TestWindowName_ShortTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "planner", TaskID: "abc"}
	got := opts.WindowName()
	want := "planner-abc"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}

func TestBuildClaudeCommand_ContainsDangerouslySkipPermissions(t *testing.T) {
	cmd := buildClaudeCommand(testOpts)
	if !strings.Contains(cmd, "--dangerously-skip-permissions") {
		t.Errorf("expected --dangerously-skip-permissions in command, got: %s", cmd)
	}
}

func TestBuildClaudeCommand_ContainsAppendSystemPrompt(t *testing.T) {
	cmd := buildClaudeCommand(testOpts)
	if !strings.Contains(cmd, "--append-system-prompt") {
		t.Errorf("expected --append-system-prompt in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, testOpts.Description) {
		t.Errorf("expected description %q in command, got: %s", testOpts.Description, cmd)
	}
}

func TestBuildClaudeCommand_ContainsSettings(t *testing.T) {
	cmd := buildClaudeCommand(testOpts)
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, testOpts.HooksPath) {
		t.Errorf("expected hooks path %q in command, got: %s", testOpts.HooksPath, cmd)
	}
}

func TestBuildClaudeCommand_ContainsInputPrompt(t *testing.T) {
	cmd := buildClaudeCommand(testOpts)
	if !strings.Contains(cmd, testOpts.InputPrompt) {
		t.Errorf("expected input prompt %q in command, got: %s", testOpts.InputPrompt, cmd)
	}
}

func TestBuildEnvExports_ContainsBelayerTaskID(t *testing.T) {
	exports := buildEnvExports(testOpts)
	if !strings.Contains(exports, "BELAYER_TASK_ID") {
		t.Errorf("expected BELAYER_TASK_ID in exports, got: %s", exports)
	}
	if !strings.Contains(exports, testOpts.TaskID) {
		t.Errorf("expected task ID %q in exports, got: %s", testOpts.TaskID, exports)
	}
}

func TestBuildEnvExports_ContainsBelayerNode(t *testing.T) {
	exports := buildEnvExports(testOpts)
	if !strings.Contains(exports, "BELAYER_NODE") {
		t.Errorf("expected BELAYER_NODE in exports, got: %s", exports)
	}
	if !strings.Contains(exports, testOpts.NodeName) {
		t.Errorf("expected node name %q in exports, got: %s", testOpts.NodeName, exports)
	}
}

func TestBuildEnvExports_ContainsBelayerAttempt(t *testing.T) {
	exports := buildEnvExports(testOpts)
	if !strings.Contains(exports, "BELAYER_ATTEMPT") {
		t.Errorf("expected BELAYER_ATTEMPT in exports, got: %s", exports)
	}
}
