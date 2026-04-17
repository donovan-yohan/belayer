package cli

import "testing"

func TestRootCmdRegistersBridgeCommands(t *testing.T) {
	cmd := NewRootCmd()

	want := []string{
		"daemon", "session", "logs", "status", "recall",
		"run", "spawn", "finish", "roster", "message",
		"request-completion", "artifact", "version", "init",
	}
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}

	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing command %q", name)
		}
	}
}
