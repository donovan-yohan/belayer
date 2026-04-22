package daemon

import (
	"strings"
	"testing"
)

// TestBuildAgentInitialMessage_WithBranch verifies that when a branch is
// provided, the initial message is prepended with the workspace context header
// containing the worktree path and branch name.
func TestBuildAgentInitialMessage_WithBranch(t *testing.T) {
	worktreePath := "/home/user/project/.belayer/worktrees/sess1/backend-dev"
	branch := "feat/my-feature"
	original := "Implement the login endpoint."

	result := buildAgentInitialMessage(branch, worktreePath, original)

	expectedHeader := "[workspace: " + worktreePath + " (git worktree on branch " + branch + ")]"
	if !strings.HasPrefix(result, expectedHeader) {
		t.Errorf("expected result to start with workspace header\n  want prefix: %q\n  got:         %q", expectedHeader, result)
	}
	if !strings.Contains(result, original) {
		t.Errorf("expected result to contain original message %q, got: %q", original, result)
	}
	// Header and body should be separated by a blank line.
	if !strings.Contains(result, "]\n\n") {
		t.Errorf("expected blank line between header and body, got: %q", result)
	}
}

// TestBuildAgentInitialMessage_EmptyBranch verifies that when no branch is set,
// the message is returned unchanged (no workspace header prepended).
func TestBuildAgentInitialMessage_EmptyBranch(t *testing.T) {
	original := "Implement the login endpoint."
	result := buildAgentInitialMessage("", "/some/path", original)
	if result != original {
		t.Errorf("expected message to be unchanged when branch is empty\n  want: %q\n  got:  %q", original, result)
	}
}

// TestBuildAgentInitialMessage_EmptyMessage verifies that when the original
// message is empty, only the header is returned (no leading blank lines beyond
// the header separator).
func TestBuildAgentInitialMessage_EmptyMessage(t *testing.T) {
	worktreePath := "/tmp/wt"
	branch := "fix/bug"

	result := buildAgentInitialMessage(branch, worktreePath, "")

	expectedHeader := "[workspace: " + worktreePath + " (git worktree on branch " + branch + ")]"
	if !strings.HasPrefix(result, expectedHeader) {
		t.Errorf("expected workspace header, got: %q", result)
	}
}

// TestBuildAgentInitialMessage_HeaderFormat validates the exact format of the
// injected header so future readers can rely on it being parseable.
func TestBuildAgentInitialMessage_HeaderFormat(t *testing.T) {
	result := buildAgentInitialMessage("main", "/repo/wt", "do the thing")
	want := "[workspace: /repo/wt (git worktree on branch main)]\n\ndo the thing"
	if result != want {
		t.Errorf("header format mismatch\n  want: %q\n  got:  %q", want, result)
	}
}
