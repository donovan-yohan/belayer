package cli

import "testing"

func TestRootCmdRegistersArtifactCommand(t *testing.T) {
	cmd := NewRootCmd()
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}
	if !seen["artifact"] {
		t.Fatal("missing artifact command")
	}
}
