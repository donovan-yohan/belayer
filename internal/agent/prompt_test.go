package agent

import (
	"strings"
	"testing"
)

func baseCtx() PromptContext {
	return PromptContext{
		Config: AgentConfig{
			Name:         "pilot-agent",
			Vendor:       "claude",
			Model:        "opus",
			SystemPrompt: "You are the pilot agent coordinating the team.",
		},
		TaskInput:   "Implement feature X as described in spec.md.",
		CoreMemory:  "Always write tests before implementation.",
		PersonalMem: "Prefers small, focused commits.",
		RestartCtx:  "Resuming after crash at step 3.",
		Team: []TeamMember{
			{Name: "implementer-agent", Vendor: "opencode", Model: "sonnet", Role: "Code implementation"},
			{Name: "reviewer-agent", Vendor: "opencode", Model: "codex", Role: "Code review"},
		},
		SessionID: "session-abc123",
	}
}

// TestCompilePrompt_AllFields verifies that a fully-populated PromptContext
// produces a prompt containing all expected sections and values.
func TestCompilePrompt_AllFields(t *testing.T) {
	ctx := baseCtx()
	result := CompilePrompt(ctx)

	checks := []struct {
		name string
		want string
	}{
		{"role header", "# Role\n"},
		{"system prompt", ctx.Config.SystemPrompt},
		{"task header", "# Task\n"},
		{"task input", ctx.TaskInput},
		{"team header", "# Your Team\n"},
		{"session id", "Session: session-abc123"},
		{"implementer in roster", "**implementer-agent** (opencode/sonnet)"},
		{"reviewer in roster", "**reviewer-agent** (opencode/codex)"},
		{"implementer role", "Code implementation"},
		{"reviewer role", "Code review"},
		{"core memory header", "# Your Memory\n"},
		{"core memory tags", "<core_memory>"},
		{"core memory content", ctx.CoreMemory},
		{"personal memory header", "# Personal Memory\n"},
		{"personal memory", ctx.PersonalMem},
		{"belayer cli header", "# Belayer CLI\n"},
		{"restart context header", "# Restart Context\n"},
		{"restart context", ctx.RestartCtx},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(result, c.want) {
				t.Errorf("CompilePrompt() missing %q\ngot:\n%s", c.want, result)
			}
		})
	}
}

// TestCompilePrompt_EmptyTaskInput verifies that an empty TaskInput still
// produces the Task section (just with no content).
func TestCompilePrompt_EmptyTaskInput(t *testing.T) {
	ctx := baseCtx()
	ctx.TaskInput = ""
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "# Task\n") {
		t.Errorf("CompilePrompt() missing Task section when TaskInput is empty")
	}
}

// TestCompilePrompt_NoCoreMemory verifies that the Your Memory section
// is omitted entirely when CoreMemory is empty.
func TestCompilePrompt_NoCoreMemory(t *testing.T) {
	ctx := baseCtx()
	ctx.CoreMemory = ""
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Your Memory") {
		t.Errorf("CompilePrompt() should omit Your Memory section when CoreMemory is empty\ngot:\n%s", result)
	}
}

// TestCompilePrompt_NoPersonalMemory verifies that the Personal Memory section
// is omitted entirely when PersonalMem is empty.
func TestCompilePrompt_NoPersonalMemory(t *testing.T) {
	ctx := baseCtx()
	ctx.PersonalMem = ""
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Personal Memory") {
		t.Errorf("CompilePrompt() should omit Personal Memory section when PersonalMem is empty\ngot:\n%s", result)
	}
}

// TestCompilePrompt_MemoryIndex verifies that the Memory Index section
// appears when MemoryIndex is set and is omitted when empty.
func TestCompilePrompt_MemoryIndex(t *testing.T) {
	ctx := baseCtx()
	ctx.MemoryIndex = "- codebase.md — architecture and conventions\n- team.md — agent tendencies"
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "# Memory Index\n") {
		t.Errorf("CompilePrompt() missing Memory Index header\ngot:\n%s", result)
	}
	if !strings.Contains(result, "codebase.md") {
		t.Errorf("CompilePrompt() missing memory index content\ngot:\n%s", result)
	}
}

// TestCompilePrompt_NoMemoryIndex verifies that the Memory Index section
// is omitted entirely when MemoryIndex is empty.
func TestCompilePrompt_NoMemoryIndex(t *testing.T) {
	ctx := baseCtx()
	ctx.MemoryIndex = ""
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Memory Index") {
		t.Errorf("CompilePrompt() should omit Memory Index section when empty\ngot:\n%s", result)
	}
}

// TestCompilePrompt_RestartContext verifies that the Restart Context section
// appears when RestartCtx is set.
func TestCompilePrompt_RestartContext(t *testing.T) {
	ctx := baseCtx()
	ctx.RestartCtx = "Resuming from checkpoint 7."
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "# Restart Context\n") {
		t.Errorf("CompilePrompt() missing Restart Context header\ngot:\n%s", result)
	}
	if !strings.Contains(result, "Resuming from checkpoint 7.") {
		t.Errorf("CompilePrompt() missing restart context content\ngot:\n%s", result)
	}
}

// TestCompilePrompt_NoRestartContext verifies that the Restart Context section
// is omitted entirely when RestartCtx is empty.
func TestCompilePrompt_NoRestartContext(t *testing.T) {
	ctx := baseCtx()
	ctx.RestartCtx = ""
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Restart Context") {
		t.Errorf("CompilePrompt() should omit Restart Context section when empty\ngot:\n%s", result)
	}
}

// TestCompilePrompt_TeamRoster verifies that all team members appear with
// their vendor, model, and role in the output.
func TestCompilePrompt_TeamRoster(t *testing.T) {
	ctx := baseCtx()
	ctx.Team = []TeamMember{
		{Name: "alpha", Vendor: "claude", Model: "opus", Role: "Orchestrator"},
		{Name: "beta", Vendor: "opencode", Model: "sonnet", Role: "Implementer"},
		{Name: "gamma", Vendor: "opencode", Model: "codex", Role: "Reviewer"},
	}
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "**alpha** (claude/opus) — Orchestrator") {
		t.Errorf("CompilePrompt() missing alpha in team roster\ngot:\n%s", result)
	}
	if !strings.Contains(result, "**beta** (opencode/sonnet) — Implementer") {
		t.Errorf("CompilePrompt() missing beta in team roster\ngot:\n%s", result)
	}
	if !strings.Contains(result, "**gamma** (opencode/codex) — Reviewer") {
		t.Errorf("CompilePrompt() missing gamma in team roster\ngot:\n%s", result)
	}
}

// TestCompilePrompt_NoTeam verifies that a solo agent gets a Session section
// instead of a Team section.
func TestCompilePrompt_NoTeam(t *testing.T) {
	ctx := baseCtx()
	ctx.Team = nil
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Your Team") {
		t.Errorf("CompilePrompt() should not show Team section when no teammates\ngot:\n%s", result)
	}
	if !strings.Contains(result, "# Session\n") {
		t.Errorf("CompilePrompt() should show Session section for solo agent\ngot:\n%s", result)
	}
	if !strings.Contains(result, "You are the only agent") {
		t.Errorf("CompilePrompt() should indicate solo agent\ngot:\n%s", result)
	}
}

// TestCompilePrompt_BelayerCLIAlwaysPresent verifies that the Belayer CLI
// section is always included regardless of other fields.
func TestCompilePrompt_BelayerCLIAlwaysPresent(t *testing.T) {
	// Minimal context — almost everything empty.
	ctx := PromptContext{
		Config: AgentConfig{
			SystemPrompt: "minimal",
		},
	}
	result := CompilePrompt(ctx)

	cliChecks := []string{
		"# Belayer CLI\n",
		"belayer message send",
		"belayer message broadcast",
		"belayer context",
		"belayer recall",
		"belayer note",
	}
	for _, want := range cliChecks {
		if !strings.Contains(result, want) {
			t.Errorf("CompilePrompt() missing CLI docs %q\ngot:\n%s", want, result)
		}
	}
}
