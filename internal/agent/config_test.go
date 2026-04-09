package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// --- LoadAgentConfig tests ---

func TestLoadAgentConfig_Valid(t *testing.T) {
	yaml := `
name: pilot-agent
role: pilot
vendor: claude
model: opus
system_prompt: "You are the pilot agent coordinating the team."
tools:
  - name: git-diff
    description: Show git diff output
    command: git diff
`
	path := writeTempYAML(t, yaml)

	cfg, err := LoadAgentConfig(path)
	if err != nil {
		t.Fatalf("LoadAgentConfig() unexpected error: %v", err)
	}
	if cfg.Name != "pilot-agent" {
		t.Errorf("Name = %q, want %q", cfg.Name, "pilot-agent")
	}
	if cfg.Role != RolePilot {
		t.Errorf("Role = %q, want %q", cfg.Role, RolePilot)
	}
	if cfg.Vendor != "claude" {
		t.Errorf("Vendor = %q, want %q", cfg.Vendor, "claude")
	}
	if cfg.Model != "opus" {
		t.Errorf("Model = %q, want %q", cfg.Model, "opus")
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(cfg.Tools))
	}
	if cfg.Tools[0].Name != "git-diff" {
		t.Errorf("Tools[0].Name = %q, want %q", cfg.Tools[0].Name, "git-diff")
	}
}

func TestLoadAgentConfig_InvalidYAML(t *testing.T) {
	path := writeTempYAML(t, "name: [unclosed bracket")

	_, err := LoadAgentConfig(path)
	if err == nil {
		t.Error("LoadAgentConfig() expected error for invalid YAML, got nil")
	}
}

func TestLoadAgentConfig_MissingFile(t *testing.T) {
	_, err := LoadAgentConfig("/nonexistent/path/agent.yaml")
	if err == nil {
		t.Error("LoadAgentConfig() expected error for missing file, got nil")
	}
}

// --- ValidateAgentConfig tests ---

func TestValidateAgentConfig_Valid(t *testing.T) {
	cfg := AgentConfig{
		Name:   "my-agent",
		Role:   RoleImplementer,
		Vendor: "codex",
	}
	if err := ValidateAgentConfig(cfg); err != nil {
		t.Errorf("ValidateAgentConfig() unexpected error: %v", err)
	}
}

func TestValidateAgentConfig_EmptyName(t *testing.T) {
	cfg := AgentConfig{
		Name:   "",
		Role:   RolePilot,
		Vendor: "claude",
	}
	if err := ValidateAgentConfig(cfg); err == nil {
		t.Error("ValidateAgentConfig() expected error for empty name, got nil")
	}
}

func TestValidateAgentConfig_InvalidRole(t *testing.T) {
	cfg := AgentConfig{
		Name:   "bad-agent",
		Role:   Role("overlord"),
		Vendor: "claude",
	}
	err := ValidateAgentConfig(cfg)
	if err == nil {
		t.Error("ValidateAgentConfig() expected error for invalid role, got nil")
	}
}

func TestValidateAgentConfig_EmptyVendor(t *testing.T) {
	cfg := AgentConfig{
		Name:   "my-agent",
		Role:   RoleReviewer,
		Vendor: "",
	}
	if err := ValidateAgentConfig(cfg); err == nil {
		t.Error("ValidateAgentConfig() expected error for empty vendor, got nil")
	}
}

// --- ToolRegistry tests ---

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	tool := Tool{Name: "git-diff", Description: "Show git diff", Command: "git diff"}

	if err := r.Register(tool); err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	got, err := r.Get("git-diff")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Name != "git-diff" {
		t.Errorf("got.Name = %q, want %q", got.Name, "git-diff")
	}
	if got.Command != "git diff" {
		t.Errorf("got.Command = %q, want %q", got.Command, "git diff")
	}
}

func TestToolRegistry_RegisterDuplicate(t *testing.T) {
	r := NewToolRegistry()
	tool := Tool{Name: "git-diff", Description: "Show git diff", Command: "git diff"}

	if err := r.Register(tool); err != nil {
		t.Fatalf("Register() first call unexpected error: %v", err)
	}
	if err := r.Register(tool); err == nil {
		t.Error("Register() expected error for duplicate tool, got nil")
	}
}

func TestToolRegistry_List_Sorted(t *testing.T) {
	r := NewToolRegistry()
	tools := []Tool{
		{Name: "zebra-tool", Command: "zebra"},
		{Name: "alpha-tool", Command: "alpha"},
		{Name: "middle-tool", Command: "middle"},
	}
	for _, tool := range tools {
		if err := r.Register(tool); err != nil {
			t.Fatalf("Register() unexpected error: %v", err)
		}
	}

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List() returned %d tools, want 3", len(list))
	}
	if list[0].Name != "alpha-tool" {
		t.Errorf("list[0].Name = %q, want %q", list[0].Name, "alpha-tool")
	}
	if list[1].Name != "middle-tool" {
		t.Errorf("list[1].Name = %q, want %q", list[1].Name, "middle-tool")
	}
	if list[2].Name != "zebra-tool" {
		t.Errorf("list[2].Name = %q, want %q", list[2].Name, "zebra-tool")
	}
}

// --- LoadAgentConfigs tests ---

func TestLoadAgentConfigs_MultipleAgents(t *testing.T) {
	yaml := `
- name: pilot-agent
  role: pilot
  vendor: claude
  model: opus
  system_prompt: "You coordinate the team."
- name: implementer-agent
  role: implementer
  vendor: codex
  model: ""
  tools:
    - name: git-status
      description: Show git status
      command: git status
- name: reviewer-agent
  role: reviewer
  vendor: claude
  model: sonnet
`
	path := writeTempYAML(t, yaml)

	cfgs, err := LoadAgentConfigs(path)
	if err != nil {
		t.Fatalf("LoadAgentConfigs() unexpected error: %v", err)
	}
	if len(cfgs) != 3 {
		t.Fatalf("len(cfgs) = %d, want 3", len(cfgs))
	}

	if cfgs[0].Name != "pilot-agent" || cfgs[0].Role != RolePilot {
		t.Errorf("cfgs[0] = {%q, %q}, want {pilot-agent, pilot}", cfgs[0].Name, cfgs[0].Role)
	}
	if cfgs[1].Name != "implementer-agent" || cfgs[1].Role != RoleImplementer {
		t.Errorf("cfgs[1] = {%q, %q}, want {implementer-agent, implementer}", cfgs[1].Name, cfgs[1].Role)
	}
	if len(cfgs[1].Tools) != 1 || cfgs[1].Tools[0].Name != "git-status" {
		t.Errorf("cfgs[1].Tools unexpected: %v", cfgs[1].Tools)
	}
	if cfgs[2].Name != "reviewer-agent" || cfgs[2].Role != RoleReviewer {
		t.Errorf("cfgs[2] = {%q, %q}, want {reviewer-agent, reviewer}", cfgs[2].Name, cfgs[2].Role)
	}
}

// --- helpers ---

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTempYAML: %v", err)
	}
	return path
}
