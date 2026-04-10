package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/agent"
)

func TestRuntimeMetadata_RoundTrip(t *testing.T) {
	sandboxDir := t.TempDir()
	want := RuntimeMetadata{
		SessionID:   "sess-123",
		Environment: "extend-fullstack",
		Workbench: &WorkbenchConfigSpec{
			Timeout: "5m",
			Services: []ServiceDecl{
				{Name: "api", Image: "example/api:latest", Ports: []string{"8080"}},
			},
		},
		Tools: []agent.ToolSpec{
			{Name: "curl-api", Exec: agent.ToolExec{Target: "workbench", Command: "curl http://extend-api:8080"}},
		},
		RepoWorktrees: map[string]string{
			"extend-api": "/tmp/extend-api",
		},
	}

	if err := WriteRuntimeMetadata(sandboxDir, want); err != nil {
		t.Fatalf("WriteRuntimeMetadata returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sandboxDir, "runtime.json")); err != nil {
		t.Fatalf("expected runtime.json to exist: %v", err)
	}

	got, err := LoadRuntimeMetadata(sandboxDir)
	if err != nil {
		t.Fatalf("LoadRuntimeMetadata returned error: %v", err)
	}

	if got.SessionID != want.SessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, want.SessionID)
	}
	if got.Environment != want.Environment {
		t.Fatalf("Environment = %q, want %q", got.Environment, want.Environment)
	}
	if got.Workbench == nil || len(got.Workbench.Services) != 1 {
		t.Fatalf("expected workbench service in metadata, got %#v", got.Workbench)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "curl-api" {
		t.Fatalf("unexpected tools payload: %#v", got.Tools)
	}
	if got.RepoWorktrees["extend-api"] != "/tmp/extend-api" {
		t.Fatalf("RepoWorktrees[extend-api] = %q", got.RepoWorktrees["extend-api"])
	}
}
