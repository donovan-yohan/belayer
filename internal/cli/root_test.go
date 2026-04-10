package cli

import "testing"

func TestRootCmdRegistersV6ScaffoldCommands(t *testing.T) {
	cmd := NewRootCmd()

	want := []string{"attach", "context", "daemon", "debug", "logs", "message", "note", "recall", "session", "setup", "status", "submit", "watch"}
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}

	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing scaffold command %q", name)
		}
	}
}
