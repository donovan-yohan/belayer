package vendor

import (
	"strings"
	"testing"
)

// --- ClaudeAdapter tests ---

func TestClaudeAdapter_Name(t *testing.T) {
	a := ClaudeAdapter{}
	if got := a.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestClaudeAdapter_LaunchCmd_ContainsStreamJSON(t *testing.T) {
	a := ClaudeAdapter{}
	cmd := a.LaunchCmd("/some/dir", "")
	if !strings.Contains(cmd, "--output-format stream-json") {
		t.Errorf("LaunchCmd() = %q, want --output-format stream-json", cmd)
	}
}

func TestClaudeAdapter_LaunchCmd_WithSystemPrompt(t *testing.T) {
	a := ClaudeAdapter{}
	cmd := a.LaunchCmd("/some/dir", "be helpful")
	if !strings.Contains(cmd, "--output-format stream-json") {
		t.Errorf("LaunchCmd() missing --output-format stream-json: %q", cmd)
	}
	if !strings.Contains(cmd, "be helpful") {
		t.Errorf("LaunchCmd() missing system prompt: %q", cmd)
	}
}

func TestClaudeAdapter_ParseOutput_AssistantMessage(t *testing.T) {
	a := ClaudeAdapter{}
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello, world!"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_tokens":0,"cache_write_tokens":0}}}`
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error: %v", err)
	}
	if evt.Type != "text" {
		t.Errorf("evt.Type = %q, want %q", evt.Type, "text")
	}
	if evt.Content != "Hello, world!" {
		t.Errorf("evt.Content = %q, want %q", evt.Content, "Hello, world!")
	}
	if evt.TokenUsage == nil {
		t.Fatal("evt.TokenUsage is nil, want non-nil")
	}
	if evt.TokenUsage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", evt.TokenUsage.InputTokens)
	}
	if evt.TokenUsage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", evt.TokenUsage.OutputTokens)
	}
}

func TestClaudeAdapter_ParseOutput_ResultLine(t *testing.T) {
	a := ClaudeAdapter{}
	line := `{"type":"result","result":"done","cost_usd":0.05,"usage":{"input_tokens":100,"output_tokens":200}}`
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error: %v", err)
	}
	if evt.Type != "token_usage" {
		t.Errorf("evt.Type = %q, want %q", evt.Type, "token_usage")
	}
	if evt.TokenUsage == nil {
		t.Fatal("evt.TokenUsage is nil, want non-nil")
	}
	if evt.TokenUsage.CostUSD != 0.05 {
		t.Errorf("CostUSD = %f, want 0.05", evt.TokenUsage.CostUSD)
	}
}

func TestClaudeAdapter_ParseOutput_MalformedJSON(t *testing.T) {
	a := ClaudeAdapter{}
	_, err := a.ParseOutput(`{not valid json`)
	if err == nil {
		t.Error("ParseOutput() expected error for malformed JSON, got nil")
	}
}

func TestClaudeAdapter_ParseOutput_UnknownType(t *testing.T) {
	a := ClaudeAdapter{}
	line := `{"type":"system","content":"session started"}`
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error for unknown type: %v", err)
	}
	if evt.Raw != line {
		t.Errorf("evt.Raw = %q, want %q", evt.Raw, line)
	}
}

// --- CodexAdapter tests ---

func TestCodexAdapter_Name(t *testing.T) {
	a := CodexAdapter{}
	if got := a.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestCodexAdapter_LaunchCmd_ContainsFullAuto(t *testing.T) {
	a := CodexAdapter{}
	cmd := a.LaunchCmd("/some/dir", "")
	if !strings.Contains(cmd, "--approval-mode full-auto") {
		t.Errorf("LaunchCmd() = %q, want --approval-mode full-auto", cmd)
	}
}

func TestCodexAdapter_LaunchCmd_WithSystemPrompt(t *testing.T) {
	a := CodexAdapter{}
	cmd := a.LaunchCmd("/some/dir", "fix the bug")
	if !strings.Contains(cmd, "--approval-mode full-auto") {
		t.Errorf("LaunchCmd() missing --approval-mode full-auto: %q", cmd)
	}
	if !strings.Contains(cmd, "fix the bug") {
		t.Errorf("LaunchCmd() missing system prompt: %q", cmd)
	}
}

func TestCodexAdapter_ParseOutput_MessageLine(t *testing.T) {
	a := CodexAdapter{}
	line := `{"type":"message","content":"writing tests now"}`
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error: %v", err)
	}
	if evt.Type != "text" {
		t.Errorf("evt.Type = %q, want %q", evt.Type, "text")
	}
	if evt.Content != "writing tests now" {
		t.Errorf("evt.Content = %q, want %q", evt.Content, "writing tests now")
	}
}

func TestCodexAdapter_ParseOutput_CompletedLine(t *testing.T) {
	a := CodexAdapter{}
	line := `{"type":"completed","usage":{"input_tokens":50,"output_tokens":100}}`
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error: %v", err)
	}
	if evt.Type != "token_usage" {
		t.Errorf("evt.Type = %q, want %q", evt.Type, "token_usage")
	}
	if evt.TokenUsage == nil {
		t.Fatal("evt.TokenUsage is nil, want non-nil")
	}
	if evt.TokenUsage.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", evt.TokenUsage.InputTokens)
	}
	if evt.TokenUsage.OutputTokens != 100 {
		t.Errorf("OutputTokens = %d, want 100", evt.TokenUsage.OutputTokens)
	}
	// cost = (50*2 + 100*8) / 1_000_000 = 0.0009
	expectedCost := (50*codexInputCostPerMTok + 100*codexOutputCostPerMTok) / 1_000_000
	if evt.TokenUsage.CostUSD != expectedCost {
		t.Errorf("CostUSD = %f, want %f", evt.TokenUsage.CostUSD, expectedCost)
	}
}

func TestCodexAdapter_ParseOutput_MalformedJSON(t *testing.T) {
	a := CodexAdapter{}
	_, err := a.ParseOutput(`not json at all`)
	if err == nil {
		t.Error("ParseOutput() expected error for malformed JSON, got nil")
	}
}

// --- GenericAdapter tests ---

func TestGenericAdapter_ReturnsRawOutputEvents(t *testing.T) {
	a := NewGenericAdapter("myagent", "myagent --run")
	line := "some unstructured output line"
	evt, err := a.ParseOutput(line)
	if err != nil {
		t.Fatalf("ParseOutput() unexpected error: %v", err)
	}
	if evt.Type != "text" {
		t.Errorf("evt.Type = %q, want %q", evt.Type, "text")
	}
	if evt.Content != line {
		t.Errorf("evt.Content = %q, want %q", evt.Content, line)
	}
	if evt.Raw != line {
		t.Errorf("evt.Raw = %q, want %q", evt.Raw, line)
	}
	if evt.TokenUsage != nil {
		t.Error("evt.TokenUsage should be nil for GenericAdapter")
	}
}

func TestGenericAdapter_Name(t *testing.T) {
	a := NewGenericAdapter("myagent", "myagent --run")
	if got := a.Name(); got != "myagent" {
		t.Errorf("Name() = %q, want %q", got, "myagent")
	}
}

func TestGenericAdapter_LaunchCmd(t *testing.T) {
	a := NewGenericAdapter("myagent", "myagent --run")
	if got := a.LaunchCmd("", ""); got != "myagent --run" {
		t.Errorf("LaunchCmd() = %q, want %q", got, "myagent --run")
	}
}

// --- Registry tests ---

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	a := ClaudeAdapter{}
	r.Register(a)

	got, err := r.Get("claude")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.Name() != "claude" {
		t.Errorf("got.Name() = %q, want %q", got.Name(), "claude")
	}
}

func TestRegistry_Get_UnknownName(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Error("Get() expected error for unknown adapter, got nil")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.Register(ClaudeAdapter{})
	r.Register(CodexAdapter{})

	names := r.List()
	if len(names) != 2 {
		t.Fatalf("List() returned %d names, want 2", len(names))
	}
}

func TestDefaultRegistry_HasExpectedAdapters(t *testing.T) {
	r := DefaultRegistry()

	for _, name := range []string{"claude", "codex", "opencode"} {
		if _, err := r.Get(name); err != nil {
			t.Errorf("DefaultRegistry missing adapter %q: %v", name, err)
		}
	}
}
