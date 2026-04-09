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
		TaskInput:     "Implement feature X as described in spec.md.",
		CoreMemory:    "Always write tests before implementation.",
		PersonalMem:   "Prefers small, focused commits.",
		StaleWarnings: []string{"core.md not updated in 30 days"},
		RestartCtx:    "Resuming after crash at step 3.",
		OtherAgents:   []string{"implementer-agent", "reviewer-agent"},
		SessionID:     "session-abc123",
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
		{"session context header", "# Session Context\n"},
		{"session id", "Session: session-abc123"},
		{"other agents", "Other agents in this session: implementer-agent, reviewer-agent"},
		{"core learnings header", "# Core Learnings\n"},
		{"core memory", ctx.CoreMemory},
		{"personal memory header", "# Personal Memory\n"},
		{"personal memory", ctx.PersonalMem},
		{"stale warnings header", "# Stale Warnings\n"},
		{"stale warning entry", "WARNING: core.md not updated in 30 days"},
		{"belayer cli header", "# Belayer CLI (Messaging Plane)\n"},
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

// TestCompilePrompt_NoCoreMemory verifies that missing CoreMemory shows the
// fallback message instead of an empty section.
func TestCompilePrompt_NoCoreMemory(t *testing.T) {
	ctx := baseCtx()
	ctx.CoreMemory = ""
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "No core learnings available.") {
		t.Errorf("CompilePrompt() expected fallback text for empty CoreMemory\ngot:\n%s", result)
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

// TestCompilePrompt_StaleWarnings verifies that each warning is prefixed with
// "WARNING: " and all appear in the output.
func TestCompilePrompt_StaleWarnings(t *testing.T) {
	ctx := baseCtx()
	ctx.StaleWarnings = []string{"file A is stale", "file B is stale"}
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "WARNING: file A is stale") {
		t.Errorf("CompilePrompt() missing WARNING prefix for first warning\ngot:\n%s", result)
	}
	if !strings.Contains(result, "WARNING: file B is stale") {
		t.Errorf("CompilePrompt() missing WARNING prefix for second warning\ngot:\n%s", result)
	}
}

// TestCompilePrompt_NoStaleWarnings verifies that the Stale Warnings section
// is omitted entirely when there are no warnings.
func TestCompilePrompt_NoStaleWarnings(t *testing.T) {
	ctx := baseCtx()
	ctx.StaleWarnings = nil
	result := CompilePrompt(ctx)

	if strings.Contains(result, "# Stale Warnings") {
		t.Errorf("CompilePrompt() should omit Stale Warnings section when none exist\ngot:\n%s", result)
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

// TestCompilePrompt_OtherAgents verifies that all other agent names appear in
// the Session Context section.
func TestCompilePrompt_OtherAgents(t *testing.T) {
	ctx := baseCtx()
	ctx.OtherAgents = []string{"alpha", "beta", "gamma"}
	result := CompilePrompt(ctx)

	if !strings.Contains(result, "Other agents in this session: alpha, beta, gamma") {
		t.Errorf("CompilePrompt() missing other agents list\ngot:\n%s", result)
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
		"# Belayer CLI (Messaging Plane)\n",
		"belayer message send",
		"belayer message broadcast",
		"belayer context",
		"belayer recall search",
		"belayer note",
		"belayer tool run",
	}
	for _, want := range cliChecks {
		if !strings.Contains(result, want) {
			t.Errorf("CompilePrompt() missing CLI docs %q\ngot:\n%s", want, result)
		}
	}
}
