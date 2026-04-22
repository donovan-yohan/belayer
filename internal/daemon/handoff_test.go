package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

func TestWriteHandoffArtifactIncludesInventorySections(t *testing.T) {
	d := testDaemon(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".belayer"), 0o755); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	cfg := "exit_conditions:\n  - verify acceptance criteria\npersistence_strategy:\n  - commit work\n"
	if err := os.WriteFile(filepath.Join(workspace, ".belayer", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	sessionID, err := d.store.CreateSession(store.Session{
		Name:         "handoff-session",
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, run := range []store.AgentRun{
		{SessionID: sessionID, Name: "supervisor", Role: "supervisor", Profile: "default", Workdir: workspace, Transport: "bridge", Status: "running"},
		{SessionID: sessionID, Name: "worker-1", Role: "worker", Profile: "default", Workdir: workspace, Branch: "feat/handoff", WorktreePath: filepath.Join(workspace, ".belayer", "worktrees", sessionID, "worker-1"), Transport: "bridge", Status: "complete"},
	} {
		if _, err := d.store.CreateAgentRun(run); err != nil {
			t.Fatalf("create agent run %s: %v", run.Name, err)
		}
	}

	if _, err := d.store.CreateMessage(store.Message{
		SessionID:   sessionID,
		SenderID:    "worker-1",
		RecipientID: "supervisor",
		Type:        "state_change",
		Content:     "Need review on the branch",
	}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := d.store.CreateArtifact(store.Artifact{
		SessionID: sessionID,
		Kind:      "review-report",
		Path:      "artifacts/review.md",
		Producer:  "reviewer",
		Summary:   "No findings",
	}); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	for _, evt := range []store.SessionEvent{
		{SessionID: sessionID, Type: "run_initiated", Data: `{"task":"ship handoff"}`},
		{SessionID: sessionID, Type: "bridge:finished", Data: `{"agent":"worker-1","final_response":"done"}`},
	} {
		if err := d.store.LogEvent(evt); err != nil {
			t.Fatalf("log event %s: %v", evt.Type, err)
		}
	}

	path, err := d.WriteHandoffArtifact(sessionID)
	if err != nil {
		t.Fatalf("WriteHandoffArtifact: %v", err)
	}
	if want := filepath.Join(workspace, ".belayer", "runs", sessionID, "handoff.md"); path != want {
		t.Fatalf("handoff path = %s, want %s", path, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"# Handoff",
		"## Session",
		"## Roster",
		"## Unacked Mail",
		"## Git State",
		"## Artifacts",
		"## Recent Events",
		"verify acceptance criteria",
		"worker-1",
		"Need review on the branch",
		"review-report",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected handoff.md to contain %q, got:\n%s", want, got)
		}
	}
}
