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

func TestExistingSessionDirFallsBackToLegacyRuns(t *testing.T) {
	workspace := t.TempDir()
	sessionID := "session-1"
	legacy := LegacySessionDir(workspace, sessionID)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}

	if got := ExistingSessionDir(workspace, sessionID); got != legacy {
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
