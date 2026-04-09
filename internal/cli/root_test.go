package cli

import "testing"

func TestRootCmdRegistersV6ScaffoldCommands(t *testing.T) {
	cmd := NewRootCmd()

	want := []string{"climb", "logs", "node-complete", "setup", "start", "status", "submit", "worker"}
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
