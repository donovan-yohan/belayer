package gate

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func TestBuildGatePrompt_IncludesDimensions(t *testing.T) {
	node := pipeline.NodeConfig{
		Name:        "review",
		Type:        pipeline.NodeTypeGate,
		Description: "Review the code changes.",
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Description: "Does the code work?", Weight: 0.6},
			{Name: "quality", Description: "Is the code clean?", Weight: 0.4, Rubric: "9-10: great"},
		},
	}

	prompt := BuildGatePrompt(node, 0)

	if !strings.Contains(prompt, "correctness") {
		t.Error("prompt should contain dimension name 'correctness'")
	}
	if !strings.Contains(prompt, "Does the code work?") {
		t.Error("prompt should contain dimension description")
	}
	if !strings.Contains(prompt, "9-10: great") {
		t.Error("prompt should contain rubric when present")
	}
	if !strings.Contains(prompt, "gate-result-attempt-0.json") {
		t.Error("prompt should mention attempt-scoped output file")
	}
	if !strings.Contains(prompt, "rationale-attempt-0.md") {
		t.Error("prompt should mention attempt-scoped rationale file")
	}
}

func TestBuildGatePrompt_NoRubric(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "check",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "test", Description: "tests pass?", Weight: 1.0},
		},
	}

	prompt := BuildGatePrompt(node, 0)

	if strings.Contains(prompt, "Rubric:") {
		t.Error("prompt should not contain Rubric label when no rubric set")
	}
}
