package climbpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingSessionDirPrefersClimbs(t *testing.T) {
	workspace := t.TempDir()
	sessionID := "session-1"

	current := SessionDir(workspace, sessionID)
	legacy := LegacySessionDir(workspace, sessionID)
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir current: %v", err)
	}
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}

	if got := ExistingSessionDir(workspace, sessionID); got != current {
		t.Fatalf("expected current climb dir %q, got %q", current, got)
	}
}

func TestSessionDirFallsBackToLegacyRuns(t *testing.T) {
	workspace := t.TempDir()
	sessionID := "session-1"
	legacy := LegacySessionDir(workspace, sessionID)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}

	if got := SessionDir(workspace, sessionID); got != legacy {
		t.Fatalf("expected legacy run dir %q, got %q", legacy, got)
	}
}

func TestExistingAgentDirFallsBackToLegacyRuns(t *testing.T) {
	workspace := t.TempDir()
	sessionID := "session-1"
	agentName := "reviewer"
	legacy := filepath.Join(LegacySessionDir(workspace, sessionID), agentName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("mkdir legacy agent: %v", err)
	}

	if got := ExistingAgentDir(workspace, sessionID, agentName); got != legacy {
		t.Fatalf("expected legacy agent dir %q, got %q", legacy, got)
	}
}

func TestAgentArtifactsRelUsesLegacyRunsForLegacySession(t *testing.T) {
	workspace := t.TempDir()
	sessionID := "session-1"
	agentName := "reviewer"
	if err := os.MkdirAll(LegacySessionDir(workspace, sessionID), 0o755); err != nil {
		t.Fatalf("mkdir legacy session: %v", err)
	}

	got := AgentArtifactsRel(workspace, sessionID, agentName)
	want := filepath.ToSlash(filepath.Join(".belayer", LegacyDirName, sessionID, agentName, "artifacts"))
	if got != want {
		t.Fatalf("artifact rel = %q, want %q", got, want)
	}
}

func TestAgentArtifactsRelUsesClimbsForNewSession(t *testing.T) {
	workspace := t.TempDir()
	got := AgentArtifactsRel(workspace, "session-1", "reviewer")
	want := filepath.ToSlash(filepath.Join(".belayer", DirName, "session-1", "reviewer", "artifacts"))
	if got != want {
		t.Fatalf("artifact rel = %q, want %q", got, want)
	}
}
