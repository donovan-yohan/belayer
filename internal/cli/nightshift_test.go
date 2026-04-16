package cli

import (
	"strings"
	"testing"
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
