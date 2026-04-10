package cli

import "testing"

func TestSessionCmdRegistersLifecycleAndAgentCommands(t *testing.T) {
	cmd := newSessionCmd()

	want := []string{"add-agent", "clean", "create", "list", "start", "stop", "wake"}
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}

	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing session subcommand %q", name)
		}
	}
}
