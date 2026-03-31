package vendor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

func TestResolve(t *testing.T) {
	t.Run("known vendors", func(t *testing.T) {
		for _, name := range []string{"claude", "codex"} {
			cfg, err := Resolve(name)
			if err != nil {
				t.Errorf("Resolve(%q) returned error: %v", name, err)
			}
			if cfg.Command == "" {
				t.Errorf("Resolve(%q) returned empty command", name)
			}
		}
	})

	t.Run("unknown vendor", func(t *testing.T) {
		_, err := Resolve("unknown-agent")
		if err == nil {
			t.Error("expected error for unknown vendor")
		}
		if !strings.Contains(err.Error(), "unknown vendor") {
			t.Errorf("error should mention 'unknown vendor', got: %v", err)
		}
	})
}

func TestBuildCommand(t *testing.T) {
	t.Run("claude non-gate", func(t *testing.T) {
		cmd, cleanup, err := BuildCommand("claude", "Implement the feature", "/tmp/work", false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanup != nil {
			t.Error("non-gate should not have cleanup")
		}
		if !strings.Contains(cmd, "claude") {
			t.Error("command should contain 'claude'")
		}
		if !strings.Contains(cmd, "-p") {
			t.Error("command should contain '-p' flag")
		}
		if !strings.Contains(cmd, "Implement the feature") {
			t.Error("command should contain the prompt")
		}
		if strings.Contains(cmd, "--json-schema") {
			t.Error("non-gate should not have json-schema flag")
		}
	})

	t.Run("codex gate with schema", func(t *testing.T) {
		dims := []pipeline.DimensionConfig{
			{Name: "code_quality", Weight: 0.5, Description: "Code quality"},
			{Name: "test_coverage", Weight: 0.5, Description: "Test coverage"},
		}
		cmd, cleanup, err := BuildCommand("codex", "$review", "/tmp/work", true, dims)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanup == nil {
			t.Error("codex gate should have cleanup for temp schema file")
		}
		if !strings.Contains(cmd, "codex") {
			t.Error("command should contain 'codex'")
		}
		if !strings.Contains(cmd, "--output-schema") {
			t.Error("gate command should contain --output-schema flag")
		}
		if !strings.Contains(cmd, "$review") {
			t.Error("command should pass $review literally")
		}
		// Verify the temp file was created
		parts := strings.Split(cmd, "--output-schema ")
		if len(parts) < 2 {
			t.Fatal("could not find schema file path in command")
		}
		schemaPath := strings.Fields(parts[1])[0]
		if _, err := os.Stat(schemaPath); err != nil {
			t.Errorf("schema temp file should exist at %s: %v", schemaPath, err)
		}
		cleanup()
		if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
			t.Error("cleanup should remove the temp file")
		}
	})

	t.Run("claude gate with inline schema", func(t *testing.T) {
		dims := []pipeline.DimensionConfig{
			{Name: "quality", Weight: 1.0, Description: "Overall quality"},
		}
		cmd, cleanup, err := BuildCommand("claude", "Review this", "/tmp/work", true, dims)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanup != nil {
			t.Error("claude gate should not have cleanup (inline schema)")
		}
		if !strings.Contains(cmd, "--json-schema") {
			t.Error("claude gate should use --json-schema flag")
		}
	})

	t.Run("unknown vendor", func(t *testing.T) {
		_, _, err := BuildCommand("unknown", "test", "/tmp", false, nil)
		if err == nil {
			t.Error("expected error for unknown vendor")
		}
	})
}

func TestGateResultSchema(t *testing.T) {
	dims := []pipeline.DimensionConfig{
		{Name: "code_quality", Weight: 0.35, Description: "Bugs and logic errors"},
		{Name: "scope_compliance", Weight: 0.30, Description: "Scope drift"},
		{Name: "production_readiness", Weight: 0.35, Description: "Production safety"},
	}

	schema := GateResultSchema(dims)

	// Should be valid JSON.
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("schema should be marshallable: %v", err)
	}

	// Should contain all dimension names.
	schemaStr := string(data)
	for _, d := range dims {
		if !strings.Contains(schemaStr, d.Name) {
			t.Errorf("schema should contain dimension %q", d.Name)
		}
	}

	// Should have required fields.
	if !strings.Contains(schemaStr, `"required"`) {
		t.Error("schema should have required fields")
	}
}

func TestWriteGateResult(t *testing.T) {
	dir := t.TempDir()

	agentOutput := []byte(`{
		"dimensions": {
			"code_quality": {"score": 8.5, "issues": ["minor style issue"], "rationale": "Well written code"},
			"test_coverage": {"score": 6.0, "issues": ["missing edge case test"], "rationale": "Needs more tests"}
		},
		"summary": "Good implementation, needs test work"
	}`)

	err := WriteGateResult(dir, agentOutput, "review", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check gate-result.json exists and is valid.
	resultData, err := os.ReadFile(dir + "/gate-result.json")
	if err != nil {
		t.Fatalf("gate-result.json should exist: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("gate-result.json should be valid JSON: %v", err)
	}
	if result["gate"] != "review" {
		t.Errorf("gate should be 'review', got %v", result["gate"])
	}

	// Check rationale.md exists.
	rationaleData, err := os.ReadFile(dir + "/rationale.md")
	if err != nil {
		t.Fatalf("rationale.md should exist: %v", err)
	}
	if !strings.Contains(string(rationaleData), "code_quality") {
		t.Error("rationale should mention code_quality dimension")
	}
}

func TestWriteGateResult_IncludesRationale(t *testing.T) {
	dir := t.TempDir()

	agentOutput := []byte(`{
		"dimensions": {
			"quality": {"score": 7.0, "issues": [], "rationale": "Solid work overall"}
		},
		"summary": "Looks good"
	}`)

	if err := WriteGateResult(dir, agentOutput, "gate", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultData, err := os.ReadFile(dir + "/gate-result.json")
	if err != nil {
		t.Fatalf("gate-result.json should exist: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	dims := result["dimensions"].(map[string]any)
	quality := dims["quality"].(map[string]any)
	rationale, ok := quality["rationale"]
	if !ok {
		t.Fatal("dimension should include 'rationale' field in gate-result.json")
	}
	if rationale != "Solid work overall" {
		t.Errorf("rationale = %q, want 'Solid work overall'", rationale)
	}
}

func TestExtractStreamResult_ValidStream(t *testing.T) {
	stream := []byte(`{"type":"start","data":"..."}
{"type":"content","text":"thinking..."}
{"type":"result","output":{"score":8,"summary":"good"}}
`)
	got, err := ExtractStreamResult(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), `"score"`) {
		t.Errorf("expected output to contain score, got: %s", got)
	}
}

func TestExtractStreamResult_NoResultEvent(t *testing.T) {
	stream := []byte(`{"type":"start","data":"..."}
{"type":"content","text":"thinking..."}
`)
	_, err := ExtractStreamResult(stream)
	if err == nil {
		t.Fatal("expected error for stream with no result event")
	}
	if !strings.Contains(err.Error(), "no result event") {
		t.Errorf("error should mention 'no result event', got: %v", err)
	}
}

func TestExtractStreamResult_EmptyStream(t *testing.T) {
	_, err := ExtractStreamResult([]byte{})
	if err == nil {
		t.Fatal("expected error for empty stream")
	}
}
