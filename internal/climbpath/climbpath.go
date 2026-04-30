package climbpath

import (
	"os"
	"path/filepath"
)

const (
	DirName       = "climbs"
	LegacyDirName = "runs"
)

func Root(workspace string) string {
	return filepath.Join(workspace, ".belayer", DirName)
}

func LegacyRoot(workspace string) string {
	return filepath.Join(workspace, ".belayer", LegacyDirName)
}

func SessionDir(workspace, sessionID string) string {
	return filepath.Join(Root(workspace), sessionID)
}

func LegacySessionDir(workspace, sessionID string) string {
	return filepath.Join(LegacyRoot(workspace), sessionID)
}

func ExistingSessionDir(workspace, sessionID string) string {
	current := SessionDir(workspace, sessionID)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := LegacySessionDir(workspace, sessionID)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

func AgentDir(workspace, sessionID, agentName string) string {
	return filepath.Join(SessionDir(workspace, sessionID), agentName)
}

func ExistingAgentDir(workspace, sessionID, agentName string) string {
	current := AgentDir(workspace, sessionID, agentName)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(LegacySessionDir(workspace, sessionID), agentName)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

func WorkspaceDir(workspace, sessionID string) string {
	return filepath.Join(SessionDir(workspace, sessionID), "workspace")
}

func TranscriptsDir(workspace, sessionID string) string {
	return filepath.Join(SessionDir(workspace, sessionID), "transcripts")
}

func ExistingTranscriptsDir(workspace, sessionID string) string {
	current := TranscriptsDir(workspace, sessionID)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(LegacySessionDir(workspace, sessionID), "transcripts")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

func AgentArtifactsRel(sessionID, agentName string) string {
	return filepath.ToSlash(filepath.Join(".belayer", DirName, sessionID, agentName, "artifacts"))
}

func HandoffRel(sessionID string) string {
	return filepath.ToSlash(filepath.Join(".belayer", DirName, sessionID, "handoff.md"))
}
