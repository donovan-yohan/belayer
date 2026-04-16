package hermes

import (
	"strings"
	"testing"
)

func TestBuildLaunchCmd_EnablesProjectPluginsAndSkillInjection(t *testing.T) {
	cmd, err := BuildLaunchCmd(LaunchConfig{
		Profile:    "default",
		Workdir:    "/tmp/project",
		SocketPath: "/tmp/belayer.sock",
		SessionID:  "sess-123",
		AgentID:    "planner",
		RunDir:     "/tmp/project/.belayer/runs/sess-123/planner",
		Skills:     []string{"belayer-support:belayer-communication"},
	})
	if err != nil {
		t.Fatalf("BuildLaunchCmd returned error: %v", err)
	}
	for _, want := range []string{
		"HERMES_ENABLE_PROJECT_PLUGINS='true'",
		"hermes --profile 'default' --skills 'belayer-support:belayer-communication'",
		"belayer finish --blocked",
		".belayer-finished",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got:\n%s", want, cmd)
		}
	}
}
