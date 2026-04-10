package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkbenchCmdRegistersLifecycleCommands(t *testing.T) {
	cmd := newWorkbenchCmd()

	want := []string{"down", "status", "up"}
	seen := map[string]bool{}
	for _, child := range cmd.Commands() {
		seen[child.Name()] = true
	}

	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing workbench subcommand %q", name)
		}
	}
}

func TestPrintWorkbenchRendersEndpointsAndServices(t *testing.T) {
	cmd := newWorkbenchCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	printWorkbench(cmd, workbenchResponse{
		ID:     "wb-123",
		Status: "ready",
		Endpoints: map[string]string{
			"api": "http://extend-api:8080",
			"app": "http://extend-app:3000",
		},
		Services: []workbenchServiceStatus{
			{Name: "extend-api", State: "running", Health: "healthy"},
			{Name: "postgres", State: "running", Health: "healthy"},
		},
	}, true)

	rendered := out.String()
	for _, want := range []string{
		"Workbench: wb-123",
		"Status: ready",
		"Endpoints:",
		"  api: http://extend-api:8080",
		"  app: http://extend-app:3000",
		"Services:",
		"  extend-api: running/healthy",
		"  postgres: running/healthy",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, rendered)
		}
	}
}
