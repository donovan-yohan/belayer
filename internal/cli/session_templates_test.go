package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/docker"
	"github.com/donovan-yohan/belayer/internal/session"
)

func writeAgentTemplate(t *testing.T, belayerDir, name, vendor, model, workspace string, ephemeral bool) {
	t.Helper()
	dir := filepath.Join(belayerDir, "templates", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir template dir: %v", err)
	}
	manifest := "description: \"" + name + "\"\n" +
		"vendor: " + vendor + "\n" +
		"model: " + model + "\n" +
		"ephemeral: " + map[bool]string{true: "true", false: "false"}[ephemeral] + "\n" +
		"workspace: " + workspace + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write agent manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte("prompt for "+name), 0o644); err != nil {
		t.Fatalf("write system prompt: %v", err)
	}
}

func testEnvironmentConfig() *docker.EnvironmentConfig {
	return &docker.EnvironmentConfig{
		Name: "extend-fullstack",
		Agents: []docker.EnvironmentAgent{
			{Template: "pilot"},
			{Template: "api-implementer", Repo: "extend-api"},
			{Template: "app-implementer", Repo: "extend-app"},
		},
	}
}

func TestLoadLaunchTemplate_ClimbFullstackFromEnvironment(t *testing.T) {
	belayerDir := t.TempDir()
	writeAgentTemplate(t, belayerDir, "pilot", "claude", "opus", "none", false)
	writeAgentTemplate(t, belayerDir, "api-implementer", "claude", "sonnet", "extend-api", false)
	writeAgentTemplate(t, belayerDir, "app-implementer", "claude", "sonnet", "extend-app", false)
	writeAgentTemplate(t, belayerDir, "reviewer", "codex", "gpt-5", "none", true)

	tmpl, err := loadLaunchTemplate("", belayerDir, testEnvironmentConfig(), "climb-fullstack", "")
	if err != nil {
		t.Fatalf("loadLaunchTemplate returned error: %v", err)
	}
	if tmpl.Phase != session.PhaseImplement {
		t.Fatalf("Phase = %q, want %q", tmpl.Phase, session.PhaseImplement)
	}
	if len(tmpl.Agents) != 4 {
		t.Fatalf("Agents = %d, want 4", len(tmpl.Agents))
	}
	if tmpl.Agents[1].Repo != "extend-api" {
		t.Fatalf("api implementer repo = %q", tmpl.Agents[1].Repo)
	}
	if tmpl.Agents[3].Tier != "ephemeral" {
		t.Fatalf("reviewer tier = %q, want ephemeral", tmpl.Agents[3].Tier)
	}
}

func TestLoadLaunchTemplate_ClimbSingleRepoSelectsMatchingImplementer(t *testing.T) {
	belayerDir := t.TempDir()
	writeAgentTemplate(t, belayerDir, "pilot", "claude", "opus", "none", false)
	writeAgentTemplate(t, belayerDir, "api-implementer", "claude", "sonnet", "extend-api", false)
	writeAgentTemplate(t, belayerDir, "app-implementer", "claude", "sonnet", "extend-app", false)
	writeAgentTemplate(t, belayerDir, "reviewer", "codex", "gpt-5", "none", true)

	tmpl, err := loadLaunchTemplate("", belayerDir, testEnvironmentConfig(), "climb", "extend-api")
	if err != nil {
		t.Fatalf("loadLaunchTemplate returned error: %v", err)
	}
	if len(tmpl.Agents) != 3 {
		t.Fatalf("Agents = %d, want 3", len(tmpl.Agents))
	}
	if tmpl.Agents[1].Name != "api-implementer" {
		t.Fatalf("implementer name = %q, want api-implementer", tmpl.Agents[1].Name)
	}
}
