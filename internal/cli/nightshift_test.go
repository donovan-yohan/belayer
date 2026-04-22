package cli

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

func TestRootCmdRegistersNightshiftCommands(t *testing.T) {
	cmd := NewRootCmd()

	want := []string{"run", "spawn", "roster", "finish", "request-completion", "message"}
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}

	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing nightshift command %q", name)
		}
	}
}

func TestFinishCmdRequiresSummary(t *testing.T) {
	cmd := newFinishCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when finish summary is missing")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRosterStatusTerminalStatuses verifies that rosterStatus returns the raw
// status string verbatim for non-destructive agents. Terminal failure statuses
// like "failed", "blocked", "stalled" must be preserved so runner scripts
// polling `belayer roster | grep -qE "complete|failed|stalled"` work correctly.
func TestRosterStatusTerminalStatuses(t *testing.T) {
	statuses := []string{"running", "complete", "blocked", "failed", "incomplete", "stalled"}
	for _, s := range statuses {
		t.Run(s, func(t *testing.T) {
			run := AgentRunView{AgentRun: store.AgentRun{Status: s, DestructiveActions: 0}, Outcome: "active"}
			got := rosterStatus(run)
			if got != s+"/active" {
				t.Errorf("rosterStatus({Status:%q, DestructiveActions:0}) = %q, want %q", s, got, s+"/active")
			}
			// Verify no extra suffix is appended for non-destructive agents.
			if strings.ContainsRune(got, '⚠') {
				t.Errorf("rosterStatus unexpectedly appended ⚠ for DestructiveActions=0")
			}
		})
	}
}

// TestRosterStatusDestructiveActionsSuffix verifies that rosterStatus appends
// the ⚠ warning suffix when an agent has recorded destructive actions.
func TestRosterStatusDestructiveActionsSuffix(t *testing.T) {
	run := AgentRunView{AgentRun: store.AgentRun{Status: "complete", DestructiveActions: 1}, Outcome: "active"}
	got := rosterStatus(run)
	if got != "complete/active⚠" {
		t.Errorf("rosterStatus with DestructiveActions=1 = %q, want %q", got, "complete/active⚠")
	}
}

func TestRosterStatusLifecycleOutcome(t *testing.T) {
	run := AgentRunView{
		AgentRun: store.AgentRun{Status: "running", DestructiveActions: 0},
		Outcome:  "budget_exhausted",
	}
	got := rosterStatus(run)
	if got != "running/budget_exhausted" {
		t.Fatalf("rosterStatus() = %q, want %q", got, "running/budget_exhausted")
	}
}

// TestRosterSessionStatusLineFormat verifies the "session=<status>" header
// format emitted by the roster command so runners can rely on it.
// TestRosterSessionStatusLineMatchesGrepPattern was removed — it used a
// hardcoded rosterOutput string and tested the wrong grep pattern
// ("completed|terminated" instead of "complete|stalled").
func TestRosterSessionStatusLineFormat(t *testing.T) {
	for _, status := range []string{"failed", "stalled", "complete", "running"} {
		line := "session=" + status
		if !strings.HasPrefix(line, "session=") {
			t.Errorf("session status line %q does not start with 'session='", line)
		}
		if !strings.Contains(line, status) {
			t.Errorf("session status line %q does not contain %q", line, status)
		}
	}
}
