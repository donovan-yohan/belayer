package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadAgentIdentity_ProjectLocalWinsOverShipped verifies that an agent
// identity file in the project-local <workdir>/.belayer/agents/<id>/ tree
// overrides a shipped default under <BelayerRoot>/agents/<id>/. This is the
// customization contract — users can override any shipped agent by dropping
// a file in .belayer/agents/.
func TestLoadAgentIdentity_ProjectLocalWinsOverShipped(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "project")
	belayerRoot := filepath.Join(root, "shipped")

	// Shipped default.
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "system-prompt.md"), "shipped prompt")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "agent.yaml"), "belayer_tools:\n  - shipped_tool\n")

	// Project-local override.
	mustWrite(t, filepath.Join(workdir, ".belayer", "agents", "reviewer", "system-prompt.md"), "project prompt")
	mustWrite(t, filepath.Join(workdir, ".belayer", "agents", "reviewer", "agent.yaml"), "belayer_tools:\n  - project_tool\n")

	got := loadAgentIdentity(workdir, belayerRoot, "reviewer", "")
	if got.SystemPrompt != "project prompt" {
		t.Errorf("SystemPrompt = %q, want %q (project-local should win)", got.SystemPrompt, "project prompt")
	}
	if !reflect.DeepEqual(got.BelayerTools, []string{"project_tool"}) {
		t.Errorf("BelayerTools = %v, want [project_tool] (project-local should win)", got.BelayerTools)
	}
}

// TestLoadAgentIdentity_FallsBackToShipped verifies that when no project-local
// override exists the shipped default is loaded. Without this the daemon
// would ignore the shipped team (supervisor, backend-dev, web-dev, pm, qa,
// reviewer) for any project without .belayer/agents/.
func TestLoadAgentIdentity_FallsBackToShipped(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "project")
	belayerRoot := filepath.Join(root, "shipped")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	mustWrite(t, filepath.Join(belayerRoot, "agents", "pm", "system-prompt.md"), "you are the pm")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "pm", "agent.yaml"),
		"model: claude-opus-4\nmax_turns: 50\nbelayer_tools:\n  - belayer_approve_completion\n  - belayer_reject_completion\n")

	got := loadAgentIdentity(workdir, belayerRoot, "pm", "")
	if got.SystemPrompt != "you are the pm" {
		t.Errorf("SystemPrompt = %q, want %q", got.SystemPrompt, "you are the pm")
	}
	if got.Model != "claude-opus-4" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-opus-4")
	}
	if got.MaxTurns != 50 {
		t.Errorf("MaxTurns = %d, want 50", got.MaxTurns)
	}
	want := []string{"belayer_approve_completion", "belayer_reject_completion"}
	if !reflect.DeepEqual(got.BelayerTools, want) {
		t.Errorf("BelayerTools = %v, want %v", got.BelayerTools, want)
	}
}

func TestLoadAgentIdentity_ReadsRuntimeKindAndEphemeral(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "mara-underbough", "agent.yaml"),
		"kind: side\nephemeral: false\n")

	got := loadAgentIdentity("", belayerRoot, "mara-underbough", "")
	if got.Kind != "side" {
		t.Errorf("Kind = %q, want side", got.Kind)
	}
	if got.Ephemeral == nil {
		t.Fatal("Ephemeral = nil, want false pointer")
	}
	if *got.Ephemeral {
		t.Errorf("Ephemeral = true, want false")
	}
}

// TestLoadAgentIdentity_ModelOverrideWins verifies that an explicit spawn
// request model takes precedence over the model: line in agent.yaml. The
// supervisor can force a specialist onto a specific model for a task.
func TestLoadAgentIdentity_ModelOverrideWins(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "agent.yaml"), "model: claude-sonnet-4\n")

	got := loadAgentIdentity("", belayerRoot, "reviewer", "claude-opus-4")
	if got.Model != "claude-opus-4" {
		t.Errorf("Model = %q, want %q (explicit override should win over agent.yaml)", got.Model, "claude-opus-4")
	}
}

// TestLoadAgentIdentity_MissingDirReturnsEmpty verifies that a missing
// identity dir produces an empty result rather than an error. Daemon code
// handles this by passing empty SystemPrompt into bridge.Config, which lets
// tests spawn agents without needing shipped identity files.
func TestLoadAgentIdentity_MissingDirReturnsEmpty(t *testing.T) {
	got := loadAgentIdentity(t.TempDir(), t.TempDir(), "ghost", "")
	if got.SystemPrompt != "" || got.SystemPromptPath != "" {
		t.Errorf("expected empty SystemPrompt for missing identity; got %+v", got)
	}
	if got.YAMLPath != "" {
		t.Errorf("expected empty YAMLPath for missing identity; got %q", got.YAMLPath)
	}
	if got.BelayerTools != nil || got.EnabledToolsets != nil {
		t.Errorf("expected nil tool lists for missing identity; got tools=%v toolsets=%v", got.BelayerTools, got.EnabledToolsets)
	}
}

// TestLoadAgentIdentity_InlineYAMLLists verifies that inline list syntax
// (belayer_tools: [a, b]) parses identically to block syntax. The agent.yaml
// parser predates any migration and supports both shapes — regressing
// either would silently drop tool allowlists.
func TestLoadAgentIdentity_InlineYAMLLists(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "agent.yaml"),
		"belayer_tools: [tool_a, tool_b]\nenabled_toolsets: [core, search]\n")

	got := loadAgentIdentity("", belayerRoot, "reviewer", "")
	if !reflect.DeepEqual(got.BelayerTools, []string{"tool_a", "tool_b"}) {
		t.Errorf("BelayerTools = %v, want [tool_a tool_b]", got.BelayerTools)
	}
	if !reflect.DeepEqual(got.EnabledToolsets, []string{"core", "search"}) {
		t.Errorf("EnabledToolsets = %v, want [core search]", got.EnabledToolsets)
	}
}

// TestLoadAgentIdentity_EmptyExplicitListMeansZero verifies that
// `belayer_tools: []` yields a non-nil empty slice (explicitly configured,
// zero tools) — distinct from an absent key. This lets an identity opt out
// of all belayer tools without changing the gating default.
func TestLoadAgentIdentity_EmptyExplicitListMeansZero(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "bare", "agent.yaml"), "belayer_tools: []\n")

	got := loadAgentIdentity("", belayerRoot, "bare", "")
	if got.BelayerTools == nil {
		t.Fatalf("expected non-nil empty slice for explicit `belayer_tools: []`, got nil")
	}
	if len(got.BelayerTools) != 0 {
		t.Errorf("expected zero tools, got %v", got.BelayerTools)
	}
}

func TestLoadAgentIdentity_MalformedInlineListsDoNotBecomeEmptyAllowlists(t *testing.T) {
	root := t.TempDir()
	belayerRoot := filepath.Join(root, "shipped")
	mustWrite(t, filepath.Join(belayerRoot, "agents", "reviewer", "agent.yaml"),
		"belayer_tools: not-a-list\nenabled_toolsets: [unterminated\n")

	got := loadAgentIdentity("", belayerRoot, "reviewer", "")
	if got.BelayerTools != nil {
		t.Errorf("BelayerTools = %v, want nil for malformed inline list", got.BelayerTools)
	}
	if got.EnabledToolsets != nil {
		t.Errorf("EnabledToolsets = %v, want nil for malformed inline list", got.EnabledToolsets)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
