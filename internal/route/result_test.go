package route

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

// --- ParseBytes tests ---

func TestParseBytes_Valid(t *testing.T) {
	data := []byte(`{
		"route": "quick-bugfix-review",
		"confidence": 0.92,
		"reasoning": "Small localized fix with no behavioral changes.",
		"rejected": [
			{"route": "full-feature-review", "reason": "too broad for this change"},
			{"route": "refactor-review", "reason": "no structural changes"}
		]
	}`)
	validRoutes := []string{"quick-bugfix-review", "full-feature-review", "refactor-review"}

	result, err := ParseBytes(data, validRoutes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Route != "quick-bugfix-review" {
		t.Errorf("Route: got %q, want %q", result.Route, "quick-bugfix-review")
	}
	if result.Confidence != 0.92 {
		t.Errorf("Confidence: got %f, want 0.92", result.Confidence)
	}
	if result.Reasoning == "" {
		t.Error("Reasoning should not be empty")
	}
	if len(result.Rejected) != 2 {
		t.Errorf("Rejected: got %d entries, want 2", len(result.Rejected))
	}
}

func TestParseBytes_InvalidJSON(t *testing.T) {
	_, err := ParseBytes([]byte("not json"), []string{"route-a"})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse route result") {
		t.Errorf("error should mention parse context, got: %v", err)
	}
}

func TestParseBytes_EmptyRoute(t *testing.T) {
	data := []byte(`{"route": "", "confidence": 0.8, "reasoning": "ok", "rejected": []}`)
	_, err := ParseBytes(data, []string{"route-a", "route-b"})
	if err == nil {
		t.Fatal("expected error for empty route field")
	}
	if !strings.Contains(err.Error(), "route field is empty") {
		t.Errorf("error should mention empty route field, got: %v", err)
	}
}

func TestParseBytes_InvalidRoute(t *testing.T) {
	data := []byte(`{"route": "unknown-route", "confidence": 0.5, "reasoning": "picked something", "rejected": []}`)
	_, err := ParseBytes(data, []string{"route-a", "route-b"})
	if err == nil {
		t.Fatal("expected error for route not in valid set")
	}
	if !strings.Contains(err.Error(), "not in valid set") {
		t.Errorf("error should mention valid set, got: %v", err)
	}
}

func TestParseBytes_ValidWithZeroConfidence(t *testing.T) {
	data := []byte(`{"route": "route-a", "confidence": 0, "reasoning": "uncertain", "rejected": []}`)
	result, err := ParseBytes(data, []string{"route-a", "route-b"})
	if err != nil {
		t.Fatalf("unexpected error for zero confidence: %v", err)
	}
	if result.Confidence != 0 {
		t.Errorf("Confidence: got %f, want 0", result.Confidence)
	}
}

// --- Validate tests ---

func TestValidate_ChosenInSet(t *testing.T) {
	r := &RouteResult{Route: "full-feature-review", Confidence: 0.9, Reasoning: "broad change"}
	err := r.Validate([]string{"quick-bugfix-review", "full-feature-review", "refactor-review"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ChosenNotInSet(t *testing.T) {
	r := &RouteResult{Route: "nonexistent-route"}
	err := r.Validate([]string{"route-a", "route-b"})
	if err == nil {
		t.Fatal("expected error for route not in valid set")
	}
	if !strings.Contains(err.Error(), "not in valid set") {
		t.Errorf("error should mention valid set, got: %v", err)
	}
}

// --- BuildRoutePrompt tests ---

func TestBuildRoutePrompt_WithDescriptions(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "review-router",
		Routes: &pipeline.RouteConfig{
			Mode: "choose_one",
			Options: map[string]pipeline.RouteOption{
				"full-feature-review": {
					Pipeline:    "pipelines/full-feature-review.yaml",
					Description: "Broad or risky change. Needs browser QA, design review.",
				},
				"quick-bugfix-review": {
					Pipeline:    "pipelines/quick-bugfix-review.yaml",
					Description: "Small localized fix. Code review gate only.",
				},
			},
		},
	}

	prompt := BuildRoutePrompt(node)

	if !strings.Contains(prompt, "Choose one of the following routes:") {
		t.Error("prompt should contain route header")
	}
	if !strings.Contains(prompt, "full-feature-review") {
		t.Error("prompt should contain route name 'full-feature-review'")
	}
	if !strings.Contains(prompt, "Broad or risky change") {
		t.Error("prompt should contain route description")
	}
	if !strings.Contains(prompt, "quick-bugfix-review") {
		t.Error("prompt should contain route name 'quick-bugfix-review'")
	}
	if !strings.Contains(prompt, "Small localized fix") {
		t.Error("prompt should contain route description")
	}
	if !strings.Contains(prompt, "You MUST choose exactly one route") {
		t.Error("prompt should contain instruction to choose exactly one route")
	}
	if !strings.Contains(prompt, "structured JSON") {
		t.Error("prompt should mention structured JSON output")
	}
}

func TestBuildRoutePrompt_NoRoutes(t *testing.T) {
	nodeNilRoutes := pipeline.NodeConfig{Name: "plain-node"}
	if got := BuildRoutePrompt(nodeNilRoutes); got != "" {
		t.Errorf("expected empty string for nil Routes, got %q", got)
	}

	nodeEmptyOptions := pipeline.NodeConfig{
		Name:   "router-no-options",
		Routes: &pipeline.RouteConfig{Mode: "choose_one", Options: map[string]pipeline.RouteOption{}},
	}
	if got := BuildRoutePrompt(nodeEmptyOptions); got != "" {
		t.Errorf("expected empty string for empty Options, got %q", got)
	}
}

func TestBuildRoutePrompt_SortedOutput(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "review-router",
		Routes: &pipeline.RouteConfig{
			Mode: "choose_one",
			Options: map[string]pipeline.RouteOption{
				"zebra-route":  {Pipeline: "pipelines/zebra.yaml", Description: "Last alphabetically"},
				"alpha-route":  {Pipeline: "pipelines/alpha.yaml", Description: "First alphabetically"},
				"middle-route": {Pipeline: "pipelines/middle.yaml", Description: "Middle alphabetically"},
			},
		},
	}

	prompt := BuildRoutePrompt(node)

	alphaPos := strings.Index(prompt, "alpha-route")
	middlePos := strings.Index(prompt, "middle-route")
	zebraPos := strings.Index(prompt, "zebra-route")

	if alphaPos == -1 || middlePos == -1 || zebraPos == -1 {
		t.Fatal("prompt missing one or more route names")
	}
	if !(alphaPos < middlePos && middlePos < zebraPos) {
		t.Errorf("routes not in alphabetical order: alpha=%d, middle=%d, zebra=%d", alphaPos, middlePos, zebraPos)
	}
}
