package cli

import (
	"strings"
	"testing"
)

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

func TestChannelsFooterAppearsInObservabilityHelp(t *testing.T) {
	root := NewRootCmd()
	for _, name := range []string{"logs", "bridges", "daemon"} {
		var found bool
		for _, child := range root.Commands() {
			if child.Name() != name {
				continue
			}
			found = true
			if !strings.Contains(child.Long, ChannelsFooter) {
				t.Fatalf("%s help missing Channels footer; got:\n%s", name, child.Long)
			}
			break
		}
		if !found {
			t.Fatalf("command %q not registered", name)
		}
	}
}
