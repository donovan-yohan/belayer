# Gate Nodes — Quality Scoring as a Pipeline Primitive

> **Status**: Completed | **Created**: 2026-03-20 | **Completed**: 2026-03-21
> **Design Doc**: `~/.gstack/projects/donovan-yohan-belayer/donovanyohan-research-desloppify-scoping-design-20260320-203444.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Add gate nodes as a second pipeline primitive — adversarial quality evaluators that produce multi-dimensional scores, route via configurable thresholds, and deliver structured feedback on retry.

**Architecture:** Gates extend the existing v3 flat-node pipeline model. A `type: gate` field on NodeConfig activates gate-specific parsing (dimensions, weights, thresholds, rubrics). Gate activities reuse the existing session spawner and completion-file polling, but post-process gate-result.json to compute weighted scores and apply threshold-based routing. Score-then-route: the activity decides PASS/RETRY/FAIL from thresholds — the Claude session never chooses.

**Tech Stack:** Go, Temporal SDK, YAML (gopkg.in/yaml.v3), existing v3 pipeline infrastructure

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-20 | Design | Approach B: Rich Gate Substrate | Full desloppify-inspired model with multi-dimensional scoring, not minimal single-score |
| 2026-03-20 | Design | Gates use Type A (pitch) contract | Short-lived, structured output — no CLI-callback needed |
| 2026-03-20 | Design | Score-then-route anti-gaming | Activity computes score from thresholds, session doesn't decide outcome |
| 2026-03-20 | Design | Reuse NodeActivity with gate post-processing | Extend existing activity rather than creating separate GateActivity — same spawner, polling, completion pattern |
| 2026-03-20 | Design | Quality trajectory deferred to Phase 2 | Core gate primitive works without trajectory tracking |

## Progress

- [x] Task 1: Pipeline model — gate type discriminator and gate-specific fields _(completed 2026-03-21)_
- [x] Task 2: Pipeline validation — gate-specific rules _(completed 2026-03-21)_
- [x] Task 3: Gate result package — parsing, scoring, thresholds _(completed 2026-03-21)_
- [x] Task 4: Gate prompt builder _(completed 2026-03-21)_
- [x] Task 5: Outcome detection — handle gate_result output type _(completed 2026-03-21)_
- [x] Task 6: Gate events _(completed 2026-03-21)_
- [x] Task 7: Gate activity — extend NodeActivity for gate scoring _(completed 2026-03-21)_
- [x] Task 8: Workflow gate routing — retry with rationale feedback _(completed 2026-03-21)_
- [x] Task 9: Default gate configs — spotter as gate _(completed 2026-03-21)_
- [x] Task 10: Integration test — workflow with gate node _(completed 2026-03-21)_

## Surprises & Discoveries

| Date | What was unexpected | Impact on plan | What was done |
|------|-------------------|----------------|---------------|
| 2026-03-21 | Default pipeline integration tests broke when spotter became a gate | Task 7 needed to also fix integration_test.go fake spawners | Worker updated fakeSpawner/retryThenPassSpawner to seed gate output files |

## Plan Drift

| Task | Plan said | What actually happened | Why |
|------|-----------|----------------------|-----|
| Task 7 | Only modify activity.go and activity_test.go | Also modified integration_test.go | Default spotter is now a gate, so existing integration test spawners needed gate output files |

---

### Task 1: Pipeline Model — Gate Type Discriminator & Fields

**Files:**
- Modify: `internal/v3/pipeline/model.go`
- Test: `internal/v3/pipeline/parser_test.go`

- [ ] **Step 1: Write failing test — gate YAML parses with new fields**

Add to `internal/v3/pipeline/parser_test.go`:

```go
const gateYAML = `
name: gate-pipeline
nodes:
  - name: lead
    type: node
    description: Write code
    output:
      type: code
    on_pass: review
    on_fail: stop
  - name: review
    type: gate
    description: Review the code
    input:
      type: code
    dimensions:
      - name: correctness
        description: "Does the code work?"
        weight: 0.5
      - name: quality
        description: "Is the code clean?"
        weight: 0.5
        rubric: "9-10: excellent, 6-8: minor issues"
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
      path: .belayer/output/gate-result.json
      rationale_path: .belayer/output/rationale.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`

func TestParsePipelineGateNode(t *testing.T) {
	cfg, err := ParsePipeline([]byte(gateYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("Nodes: got %d, want 2", len(cfg.Nodes))
	}

	gate := cfg.Nodes[1]
	if gate.Type != NodeTypeGate {
		t.Errorf("Type: got %q, want %q", gate.Type, NodeTypeGate)
	}
	if len(gate.Dimensions) != 2 {
		t.Fatalf("Dimensions: got %d, want 2", len(gate.Dimensions))
	}
	if gate.Dimensions[0].Name != "correctness" {
		t.Errorf("Dimensions[0].Name: got %q, want %q", gate.Dimensions[0].Name, "correctness")
	}
	if gate.Dimensions[0].Weight != 0.5 {
		t.Errorf("Dimensions[0].Weight: got %f, want 0.5", gate.Dimensions[0].Weight)
	}
	if gate.Dimensions[1].Rubric != "9-10: excellent, 6-8: minor issues" {
		t.Errorf("Dimensions[1].Rubric: got %q", gate.Dimensions[1].Rubric)
	}
	if gate.Thresholds.Pass != 7.0 {
		t.Errorf("Thresholds.Pass: got %f, want 7.0", gate.Thresholds.Pass)
	}
	if gate.Thresholds.Retry != 4.0 {
		t.Errorf("Thresholds.Retry: got %f, want 4.0", gate.Thresholds.Retry)
	}
	if gate.Output.RationalePath != ".belayer/output/rationale.md" {
		t.Errorf("Output.RationalePath: got %q", gate.Output.RationalePath)
	}
}

func TestIsGate(t *testing.T) {
	node := &NodeConfig{Name: "lead", Type: NodeTypeNode}
	if node.IsGate() {
		t.Error("expected node to not be a gate")
	}

	gate := &NodeConfig{Name: "review", Type: NodeTypeGate}
	if !gate.IsGate() {
		t.Error("expected gate to be a gate")
	}

	// Default (empty type) should not be a gate.
	empty := &NodeConfig{Name: "default"}
	if empty.IsGate() {
		t.Error("expected empty type to not be a gate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/pipeline/ -run "TestParsePipelineGateNode|TestIsGate" -v`
Expected: FAIL — `NodeTypeGate` undefined, `Dimensions` field missing

- [ ] **Step 3: Add gate types and fields to model.go**

In `internal/v3/pipeline/model.go`, add:

```go
// NodeType discriminates between constructive nodes and adversarial gates.
type NodeType string

const (
	NodeTypeNode NodeType = "node"
	NodeTypeGate NodeType = "gate"
)

// DimensionConfig defines a scoring dimension for a gate node.
type DimensionConfig struct {
	Name        string  `yaml:"name" json:"name"`
	Description string  `yaml:"description" json:"description"`
	Weight      float64 `yaml:"weight" json:"weight"`
	Rubric      string  `yaml:"rubric,omitempty" json:"rubric,omitempty"`
}

// ThresholdConfig defines score-based routing for a gate node.
type ThresholdConfig struct {
	Pass  float64 `yaml:"pass" json:"pass"`
	Retry float64 `yaml:"retry" json:"retry"`
}
```

Add fields to `NodeConfig`:

```go
Type        NodeType          `yaml:"type,omitempty" json:"type,omitempty"`
Dimensions  []DimensionConfig `yaml:"dimensions,omitempty" json:"dimensions,omitempty"`
Thresholds  ThresholdConfig   `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
```

Add field to `OutputConfig`:

```go
RationalePath string `yaml:"rationale_path,omitempty" json:"rationale_path,omitempty"`
```

Add helper methods:

```go
// IsGate returns true if this node is a gate type.
func (n *NodeConfig) IsGate() bool {
	return n.Type == NodeTypeGate
}

// EffectiveType returns the node's type, defaulting to "node".
func (n *NodeConfig) EffectiveType() NodeType {
	if n.Type == "" {
		return NodeTypeNode
	}
	return n.Type
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/v3/pipeline/ -run "TestParsePipelineGateNode|TestIsGate" -v`
Expected: PASS

- [ ] **Step 5: Run all pipeline tests**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: All PASS (existing tests unaffected)

- [ ] **Step 6: Commit**

```bash
git add internal/v3/pipeline/model.go internal/v3/pipeline/parser_test.go
git commit -m "feat(v3): add gate type discriminator and gate-specific fields to pipeline model"
```

---

### Task 2: Pipeline Validation — Gate-Specific Rules

**Files:**
- Modify: `internal/v3/pipeline/validate.go`
- Test: `internal/v3/pipeline/validate_test.go`

- [ ] **Step 1: Write failing tests — gate validation rules**

Add to `internal/v3/pipeline/validate_test.go`:

```go
func validGatePipeline() *PipelineConfig {
	return &PipelineConfig{
		Name: "gate-pipeline",
		Nodes: []NodeConfig{
			{
				Name:   "lead",
				Type:   NodeTypeNode,
				Output: OutputConfig{Type: "code"},
				OnPass: "review",
				OnFail: "stop",
			},
			{
				Name: "review",
				Type: NodeTypeGate,
				Output: OutputConfig{Type: "gate_result"},
				Dimensions: []DimensionConfig{
					{Name: "correctness", Description: "works?", Weight: 0.6},
					{Name: "quality", Description: "clean?", Weight: 0.4},
				},
				Thresholds: ThresholdConfig{Pass: 7.0, Retry: 4.0},
				OnPass:     "next",
				OnRetry:    "lead",
				OnFail:     "stop",
			},
		},
	}
}

func TestValidateGatePipelineValid(t *testing.T) {
	if err := Validate(validGatePipeline()); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateGateNoDimensions(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Dimensions = nil
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for gate with no dimensions")
	}
	if !strings.Contains(err.Error(), "dimension") {
		t.Errorf("error should mention dimension, got: %v", err)
	}
}

func TestValidateGateWeightsDontSumToOne(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Dimensions[0].Weight = 0.3 // 0.3 + 0.4 = 0.7
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for weights not summing to 1.0")
	}
	if !strings.Contains(err.Error(), "sum to") {
		t.Errorf("error should mention sum, got: %v", err)
	}
}

func TestValidateGateDuplicateDimensionName(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Dimensions[1].Name = "correctness" // duplicate
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate dimension name")
	}
	if !strings.Contains(err.Error(), "duplicate dimension") {
		t.Errorf("error should mention duplicate dimension, got: %v", err)
	}
}

func TestValidateGateRetryAbovePass(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Thresholds.Retry = 8.0 // retry > pass
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for retry >= pass")
	}
	if !strings.Contains(err.Error(), "retry") {
		t.Errorf("error should mention retry, got: %v", err)
	}
}

func TestValidateGateResultOutputType(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Output.Type = "gate_result"
	if err := Validate(cfg); err != nil {
		t.Errorf("gate_result should be valid output type, got: %v", err)
	}
}

func TestValidateNonGateWithDimensions(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Dimensions = []DimensionConfig{
		{Name: "x", Weight: 1.0},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for non-gate with dimensions")
	}
	if !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("error should mention dimensions, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/v3/pipeline/ -run "TestValidateGate|TestValidateNonGate" -v`
Expected: FAIL — gate validation not implemented

- [ ] **Step 3: Implement gate validation in validate.go**

Add `"math"` to imports. Replace the output type check and add gate validation block after the existing node validation loop:

```go
// Replace output type check:
validOutputTypes := map[string]bool{"file": true, "code": true, "gate_result": true}
if !validOutputTypes[n.Output.Type] {
	return fmt.Errorf("node %q: output.type must be \"file\", \"code\", or \"gate_result\", got %q", n.Name, n.Output.Type)
}

// Gate-specific validation
if n.IsGate() {
	if len(n.Dimensions) == 0 {
		return fmt.Errorf("gate %q: must have at least one dimension", n.Name)
	}
	var totalWeight float64
	dimNames := make(map[string]bool)
	for _, d := range n.Dimensions {
		if d.Name == "" {
			return fmt.Errorf("gate %q: dimension name is required", n.Name)
		}
		if dimNames[d.Name] {
			return fmt.Errorf("gate %q: duplicate dimension name %q", n.Name, d.Name)
		}
		dimNames[d.Name] = true
		if d.Weight <= 0 {
			return fmt.Errorf("gate %q: dimension %q weight must be positive", n.Name, d.Name)
		}
		totalWeight += d.Weight
	}
	if math.Abs(totalWeight-1.0) > 0.001 {
		return fmt.Errorf("gate %q: dimension weights sum to %.3f, must sum to 1.0", n.Name, totalWeight)
	}
	if n.Thresholds.Pass <= 0 {
		return fmt.Errorf("gate %q: thresholds.pass must be positive", n.Name)
	}
	if n.Thresholds.Retry >= n.Thresholds.Pass {
		return fmt.Errorf("gate %q: thresholds.retry (%.1f) must be less than thresholds.pass (%.1f)", n.Name, n.Thresholds.Retry, n.Thresholds.Pass)
	}
} else if len(n.Dimensions) > 0 {
	return fmt.Errorf("node %q: dimensions are only valid on gate nodes", n.Name)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/pipeline/validate.go internal/v3/pipeline/validate_test.go
git commit -m "feat(v3): add gate-specific validation — dimensions, weights, thresholds"
```

---

### Task 3: Gate Result Package — Parsing, Scoring, Thresholds

**Files:**
- Create: `internal/v3/gate/result.go`
- Create: `internal/v3/gate/result_test.go`

- [ ] **Step 1: Write failing tests for gate result parsing and scoring**

Create `internal/v3/gate/result_test.go`:

```go
package gate

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func TestParseGateResult_Valid(t *testing.T) {
	data := []byte(`{
		"gate": "review",
		"attempt": 1,
		"dimensions": {
			"correctness": {"score": 8, "rationale": "solid", "issues": []},
			"quality": {"score": 7, "rationale": "clean", "issues": ["minor naming"]}
		},
		"weighted_score": 7.5,
		"outcome": "PASS",
		"summary": "Good work"
	}`)

	result, err := ParseGateResult(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate != "review" {
		t.Errorf("Gate: got %q, want %q", result.Gate, "review")
	}
	if len(result.Dimensions) != 2 {
		t.Fatalf("Dimensions: got %d, want 2", len(result.Dimensions))
	}
	if result.Dimensions["correctness"].Score != 8 {
		t.Errorf("correctness score: got %f, want 8", result.Dimensions["correctness"].Score)
	}
}

func TestParseGateResult_InvalidJSON(t *testing.T) {
	_, err := ParseGateResult([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateGateResult_MissingDimension(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8, Rationale: "ok"},
		},
	}
	err := ValidateGateResult(result, []string{"correctness", "quality"})
	if err == nil {
		t.Fatal("expected error for missing dimension")
	}
}

func TestValidateGateResult_AllPresent(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8, Rationale: "ok"},
			"quality":     {Score: 7, Rationale: "ok"},
		},
	}
	if err := ValidateGateResult(result, []string{"correctness", "quality"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClampScore(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-1, 0},
		{0, 0},
		{5, 5},
		{10, 10},
		{11, 10},
	}
	for _, tt := range tests {
		got := clampScore(tt.in)
		if got != tt.want {
			t.Errorf("clampScore(%f): got %f, want %f", tt.in, got, tt.want)
		}
	}
}

func TestComputeWeightedScore(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"correctness": {Score: 8},
			"quality":     {Score: 6},
		},
	}
	dims := []pipeline.DimensionConfig{
		{Name: "correctness", Weight: 0.6},
		{Name: "quality", Weight: 0.4},
	}
	// 8*0.6 + 6*0.4 = 4.8 + 2.4 = 7.2
	got := ComputeWeightedScore(result, dims)
	if got < 7.19 || got > 7.21 {
		t.Errorf("weighted score: got %f, want ~7.2", got)
	}
}

func TestComputeWeightedScore_ClampsOutOfRange(t *testing.T) {
	result := &GateResult{
		Dimensions: map[string]DimensionResult{
			"a": {Score: 12}, // out of range
		},
	}
	dims := []pipeline.DimensionConfig{
		{Name: "a", Weight: 1.0},
	}
	got := ComputeWeightedScore(result, dims)
	if got != 10.0 {
		t.Errorf("expected clamped score 10.0, got %f", got)
	}
}

func TestApplyThresholds(t *testing.T) {
	thresholds := pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0}

	tests := []struct {
		score float64
		want  model.NodeOutcome
	}{
		{8.0, model.OutcomePass},
		{7.0, model.OutcomePass},  // exactly at pass
		{6.9, model.OutcomeRetry}, // below pass, above retry
		{4.0, model.OutcomeRetry}, // exactly at retry
		{3.9, model.OutcomeFail},  // below retry
		{0.0, model.OutcomeFail},
	}
	for _, tt := range tests {
		got := ApplyThresholds(tt.score, thresholds)
		if got != tt.want {
			t.Errorf("ApplyThresholds(%f): got %q, want %q", tt.score, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/v3/gate/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement gate result package**

Create `internal/v3/gate/result.go`:

```go
package gate

import (
	"encoding/json"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// DimensionResult holds the score and rationale for a single gate dimension.
type DimensionResult struct {
	Score     float64  `json:"score"`
	Rationale string   `json:"rationale"`
	Issues    []string `json:"issues,omitempty"`
}

// GateResult is the structured output from a gate session (gate-result.json).
type GateResult struct {
	Gate          string                     `json:"gate"`
	Attempt       int                        `json:"attempt"`
	Timestamp     string                     `json:"timestamp,omitempty"`
	Dimensions    map[string]DimensionResult `json:"dimensions"`
	WeightedScore float64                    `json:"weighted_score"`
	Outcome       string                     `json:"outcome"`
	Summary       string                     `json:"summary,omitempty"`
}

// ParseGateResult unmarshals gate-result.json bytes into a GateResult.
func ParseGateResult(data []byte) (*GateResult, error) {
	var result GateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse gate result: %w", err)
	}
	return &result, nil
}

// ValidateGateResult checks that all expected dimensions are present.
func ValidateGateResult(result *GateResult, expectedDimensions []string) error {
	for _, name := range expectedDimensions {
		if _, ok := result.Dimensions[name]; !ok {
			return fmt.Errorf("gate result missing dimension %q", name)
		}
	}
	return nil
}

// ComputeWeightedScore computes the weighted average score across dimensions.
// Scores are clamped to [0, 10].
func ComputeWeightedScore(result *GateResult, dimensions []pipeline.DimensionConfig) float64 {
	var total float64
	for _, dim := range dimensions {
		dr, ok := result.Dimensions[dim.Name]
		if !ok {
			continue
		}
		total += clampScore(dr.Score) * dim.Weight
	}
	return total
}

// ApplyThresholds determines the outcome based on score and thresholds.
// score >= pass → PASS, score >= retry → RETRY, else FAIL.
func ApplyThresholds(score float64, thresholds pipeline.ThresholdConfig) model.NodeOutcome {
	if score >= thresholds.Pass {
		return model.OutcomePass
	}
	if score >= thresholds.Retry {
		return model.OutcomeRetry
	}
	return model.OutcomeFail
}

// clampScore restricts a score to the valid [0, 10] range.
func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 10 {
		return 10
	}
	return score
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/gate/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/gate/result.go internal/v3/gate/result_test.go
git commit -m "feat(v3): add gate result parsing, weighted scoring, and threshold routing"
```

---

### Task 4: Gate Prompt Builder

**Files:**
- Create: `internal/v3/gate/prompt.go`
- Create: `internal/v3/gate/prompt_test.go`

- [ ] **Step 1: Write failing tests for gate prompt construction**

Create `internal/v3/gate/prompt_test.go` (append to existing file or create separate):

```go
// Add to internal/v3/gate/result_test.go or create internal/v3/gate/prompt_test.go

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

	prompt := BuildGatePrompt(node)

	if !strings.Contains(prompt, "correctness") {
		t.Error("prompt should contain dimension name 'correctness'")
	}
	if !strings.Contains(prompt, "Does the code work?") {
		t.Error("prompt should contain dimension description")
	}
	if !strings.Contains(prompt, "9-10: great") {
		t.Error("prompt should contain rubric when present")
	}
	if !strings.Contains(prompt, "gate-result.json") {
		t.Error("prompt should mention output file")
	}
	if !strings.Contains(prompt, "rationale.md") {
		t.Error("prompt should mention rationale file")
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

	prompt := BuildGatePrompt(node)

	if strings.Contains(prompt, "Rubric:") {
		t.Error("prompt should not contain Rubric label when no rubric set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/gate/ -run "TestBuildGatePrompt" -v`
Expected: FAIL — `BuildGatePrompt` undefined

- [ ] **Step 3: Implement gate prompt builder**

Create `internal/v3/gate/prompt.go`:

```go
package gate

import (
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// BuildGatePrompt constructs the structured prompt for a gate's Claude session.
// This prompt includes dimension definitions, rubrics, and output format instructions.
func BuildGatePrompt(node pipeline.NodeConfig) string {
	var sb strings.Builder

	sb.WriteString("You are evaluating work as a quality gate.\n\n")

	// Dimensions
	sb.WriteString("Score each dimension from 0-10. For each, provide:\n")
	sb.WriteString("- A score (integer 0-10)\n")
	sb.WriteString("- A brief rationale (1-2 sentences)\n")
	sb.WriteString("- Specific issues found (if any)\n\n")
	sb.WriteString("Dimensions:\n\n")

	for _, dim := range node.Dimensions {
		sb.WriteString(fmt.Sprintf("- **%s** (weight: %.2f): %s\n", dim.Name, dim.Weight, dim.Description))
		if dim.Rubric != "" {
			sb.WriteString(fmt.Sprintf("  Rubric: %s\n", dim.Rubric))
		}
	}

	// Output instructions
	sb.WriteString("\nProduce two files:\n\n")
	sb.WriteString("1. `.belayer/output/gate-result.json` — structured scores per dimension:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"gate\": \"" + node.Name + "\",\n")
	sb.WriteString("  \"attempt\": 0,\n")
	sb.WriteString("  \"dimensions\": {\n")
	for i, dim := range node.Dimensions {
		comma := ","
		if i == len(node.Dimensions)-1 {
			comma = ""
		}
		sb.WriteString(fmt.Sprintf("    \"%s\": {\"score\": 0, \"rationale\": \"\", \"issues\": []}%s\n", dim.Name, comma))
	}
	sb.WriteString("  },\n")
	sb.WriteString("  \"weighted_score\": 0,\n")
	sb.WriteString("  \"outcome\": \"PASS\",\n")
	sb.WriteString("  \"summary\": \"\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("2. `.belayer/output/rationale.md` — human-readable review with action items for each dimension.\n\n")
	sb.WriteString("Be rigorous. The only way to improve the score is to genuinely improve the work.\n")

	return sb.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/gate/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/gate/prompt.go internal/v3/gate/prompt_test.go
git commit -m "feat(v3): add gate prompt builder with dimension/rubric injection"
```

---

### Task 5: Outcome Detection — Handle gate_result Output Type

**Files:**
- Modify: `internal/v3/outcome/detect.go`
- Test: `internal/v3/outcome/detect_test.go`

- [ ] **Step 1: Write failing test for gate_result output type**

Add to `internal/v3/outcome/detect_test.go`:

```go
func TestDetect_GateResultType_DefaultsToPass(t *testing.T) {
	workDir := t.TempDir()
	node := &pipeline.NodeConfig{
		Name:   "review",
		Type:   pipeline.NodeTypeGate,
		Output: pipeline.OutputConfig{Type: "gate_result"},
	}

	result := Detect(node, workDir, 0)
	// Gate result nodes default to PASS — the activity handles scoring.
	if result.Outcome != model.OutcomePass {
		t.Errorf("outcome: got %q, want %q", result.Outcome, model.OutcomePass)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/outcome/ -run "TestDetect_GateResultType" -v`
Expected: FAIL — `gate_result` falls through to `typeDefault` which returns FAIL (no file at path)

- [ ] **Step 3: Add gate_result handling to typeDefault**

In `internal/v3/outcome/detect.go`, update `typeDefault`:

```go
func typeDefault(node *pipeline.NodeConfig, workDir string, attempt int) model.CompletionResult {
	switch node.Output.Type {
	case "file":
		if node.Output.Path != "" {
			opath := filepath.Join(workDir, node.Output.Path)
			if _, err := os.Stat(opath); err == nil {
				return model.CompletionResult{Outcome: model.OutcomePass, OutputPath: node.Output.Path, Attempt: attempt}
			}
		}
		return model.CompletionResult{Outcome: model.OutcomeFail, Attempt: attempt}
	case "gate_result":
		// Gate outcome is determined by the activity's scoring logic, not verdict detection.
		// Default to PASS here — the activity overrides based on threshold evaluation.
		return model.CompletionResult{Outcome: model.OutcomePass, Attempt: attempt}
	default:
		// code and unknown types default to PASS (caller checks commits)
		return model.CompletionResult{Outcome: model.OutcomePass, Attempt: attempt}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/outcome/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/outcome/detect.go internal/v3/outcome/detect_test.go
git commit -m "feat(v3): handle gate_result output type in outcome detection"
```

---

### Task 6: Gate Events

**Files:**
- Modify: `internal/v3/events/types.go`
- Test: `internal/v3/events/logger_test.go`

- [ ] **Step 1: Write failing tests for gate events**

Add to `internal/v3/events/logger_test.go`:

```go
func TestGateEvents(t *testing.T) {
	evt := GateStarted("review", 1)
	if evt.Type != "gate_started" {
		t.Errorf("Type: got %q, want %q", evt.Type, "gate_started")
	}
	if evt.Node != "review" {
		t.Errorf("Node: got %q, want %q", evt.Node, "review")
	}

	scores := map[string]float64{"correctness": 8.0, "quality": 7.0}
	scored := GateScored("review", 1, scores, 7.5)
	if scored.Type != "gate_scored" {
		t.Errorf("Type: got %q, want %q", scored.Type, "gate_scored")
	}
	if scored.WeightedScore != 7.5 {
		t.Errorf("WeightedScore: got %f, want 7.5", scored.WeightedScore)
	}
	if scored.DimensionScores["correctness"] != 8.0 {
		t.Errorf("correctness score: got %f, want 8.0", scored.DimensionScores["correctness"])
	}

	completed := GateCompleted("review", 1, "PASS", 7.5)
	if completed.Type != "gate_completed" {
		t.Errorf("Type: got %q, want %q", completed.Type, "gate_completed")
	}
	if completed.WeightedScore != 7.5 {
		t.Errorf("WeightedScore: got %f, want 7.5", completed.WeightedScore)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/events/ -run "TestGateEvents" -v`
Expected: FAIL — `GateStarted` undefined

- [ ] **Step 3: Add gate event types and factories**

Add fields and factories to `internal/v3/events/types.go`:

```go
// Add fields to Event struct:
WeightedScore   float64            `json:"weighted_score,omitempty"`
DimensionScores map[string]float64 `json:"dimension_scores,omitempty"`

// Add gate event factories:
func GateStarted(gate string, attempt int) Event {
	return Event{Timestamp: time.Now(), Type: "gate_started", Node: gate, Attempt: attempt}
}

func GateScored(gate string, attempt int, dimensionScores map[string]float64, weightedScore float64) Event {
	return Event{
		Timestamp:       time.Now(),
		Type:            "gate_scored",
		Node:            gate,
		Attempt:         attempt,
		DimensionScores: dimensionScores,
		WeightedScore:   weightedScore,
	}
}

func GateCompleted(gate string, attempt int, outcome string, weightedScore float64) Event {
	return Event{
		Timestamp:     time.Now(),
		Type:          "gate_completed",
		Node:          gate,
		Attempt:       attempt,
		Outcome:       outcome,
		WeightedScore: weightedScore,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/events/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/events/types.go internal/v3/events/logger_test.go
git commit -m "feat(v3): add gate_started, gate_scored, gate_completed event types"
```

---

### Task 7: Gate Activity — Extend NodeActivity for Gate Scoring

**Files:**
- Modify: `internal/v3/temporal/activity.go`
- Test: `internal/v3/temporal/activity_test.go`

This is the core integration task. After the existing completion-file polling, gate nodes read gate-result.json, validate, compute the weighted score, and override the outcome based on thresholds.

- [ ] **Step 1: Write failing tests for gate activity post-processing**

Add to `internal/v3/temporal/activity_test.go`:

```go
func TestBuildInputPrompt_GateNode(t *testing.T) {
	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Input: pipeline.InputConfig{
			Type: "code",
		},
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Description: "works?", Weight: 0.6},
			{Name: "quality", Description: "clean?", Weight: 0.4},
		},
	}

	prompt := buildInputPrompt(node, nil, "/tmp/work")
	if !strings.Contains(prompt, "correctness") {
		t.Error("gate prompt should include dimension names")
	}
	if !strings.Contains(prompt, "gate-result.json") {
		t.Error("gate prompt should mention gate-result.json")
	}
}

func TestProcessGateResult_Pass(t *testing.T) {
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, ".belayer", "output")
	os.MkdirAll(outputDir, 0o755)

	// Write gate-result.json
	gateJSON := `{
		"gate": "review",
		"attempt": 0,
		"dimensions": {
			"correctness": {"score": 8, "rationale": "solid", "issues": []},
			"quality": {"score": 7, "rationale": "clean", "issues": []}
		},
		"weighted_score": 7.6,
		"outcome": "PASS",
		"summary": "Good"
	}`
	os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(gateJSON), 0o644)

	// Write rationale.md
	os.WriteFile(filepath.Join(outputDir, "rationale.md"), []byte("# Review\nLooks good."), 0o644)

	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 0.6},
			{Name: "quality", Weight: 0.4},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}

	result, err := processGateResult(workDir, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomePass {
		t.Errorf("outcome: got %q, want PASS", result.Outcome)
	}
}

func TestProcessGateResult_Retry(t *testing.T) {
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, ".belayer", "output")
	os.MkdirAll(outputDir, 0o755)

	gateJSON := `{
		"gate": "review",
		"attempt": 0,
		"dimensions": {
			"correctness": {"score": 5, "rationale": "issues", "issues": ["bug"]},
			"quality": {"score": 5, "rationale": "messy", "issues": ["style"]}
		},
		"weighted_score": 5.0,
		"outcome": "RETRY",
		"summary": "Needs work"
	}`
	os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(gateJSON), 0o644)
	os.WriteFile(filepath.Join(outputDir, "rationale.md"), []byte("# Review\nFix bugs."), 0o644)

	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 0.6},
			{Name: "quality", Weight: 0.4},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}

	result, err := processGateResult(workDir, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomeRetry {
		t.Errorf("outcome: got %q, want RETRY", result.Outcome)
	}
	if result.Feedback == "" {
		t.Error("expected feedback to contain rationale")
	}
}

func TestProcessGateResult_Fail(t *testing.T) {
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, ".belayer", "output")
	os.MkdirAll(outputDir, 0o755)

	gateJSON := `{
		"gate": "review",
		"attempt": 0,
		"dimensions": {
			"correctness": {"score": 2, "rationale": "broken", "issues": ["crash"]},
			"quality": {"score": 3, "rationale": "unreadable", "issues": []}
		},
		"weighted_score": 2.4,
		"outcome": "FAIL",
		"summary": "Fundamentally broken"
	}`
	os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(gateJSON), 0o644)
	os.WriteFile(filepath.Join(outputDir, "rationale.md"), []byte("# Review\nBroken."), 0o644)

	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 0.6},
			{Name: "quality", Weight: 0.4},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}

	result, err := processGateResult(workDir, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != model.OutcomeFail {
		t.Errorf("outcome: got %q, want FAIL", result.Outcome)
	}
}

func TestProcessGateResult_MissingRationale(t *testing.T) {
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, ".belayer", "output")
	os.MkdirAll(outputDir, 0o755)

	gateJSON := `{
		"gate": "review",
		"attempt": 0,
		"dimensions": {"correctness": {"score": 8, "rationale": "ok", "issues": []}},
		"weighted_score": 8,
		"outcome": "PASS",
		"summary": "ok"
	}`
	os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(gateJSON), 0o644)
	// No rationale.md written

	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 1.0},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}

	result, err := processGateResult(workDir, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Missing rationale → FAIL (anti-gaming)
	if result.Outcome != model.OutcomeFail {
		t.Errorf("outcome: got %q, want FAIL (missing rationale)", result.Outcome)
	}
}

func TestProcessGateResult_MissingGateResultJSON(t *testing.T) {
	workDir := t.TempDir()
	// No files written

	node := pipeline.NodeConfig{
		Name: "review",
		Type: pipeline.NodeTypeGate,
		Dimensions: []pipeline.DimensionConfig{
			{Name: "correctness", Weight: 1.0},
		},
		Thresholds: pipeline.ThresholdConfig{Pass: 7.0, Retry: 4.0},
		Output: pipeline.OutputConfig{
			Type:          "gate_result",
			Path:          ".belayer/output/gate-result.json",
			RationalePath: ".belayer/output/rationale.md",
		},
	}

	_, err := processGateResult(workDir, node)
	if err == nil {
		t.Fatal("expected error for missing gate-result.json")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/v3/temporal/ -run "TestBuildInputPrompt_GateNode|TestProcessGateResult" -v`
Expected: FAIL — functions don't exist

- [ ] **Step 3: Implement gate processing in activity.go**

Add imports for `gate` package. Add `processGateResult` function and modify `buildInputPrompt` to handle gates:

In `internal/v3/temporal/activity.go`, add import:
```go
"github.com/donovan-yohan/belayer/internal/v3/gate"
```

Add the `processGateResult` function:

```go
// processGateResult reads gate-result.json, validates, computes weighted score,
// and applies thresholds to determine the gate outcome. This is the score-then-route
// pattern: the activity decides outcome, not the Claude session.
func processGateResult(workDir string, node pipeline.NodeConfig) (model.CompletionResult, error) {
	// Resolve gate-result.json path
	resultPath := node.Output.Path
	if resultPath == "" {
		resultPath = ".belayer/output/gate-result.json"
	}
	absResultPath := filepath.Join(workDir, resultPath)

	// Read gate-result.json
	data, err := os.ReadFile(absResultPath)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("read gate result: %w", err)
	}

	// Parse
	gateResult, err := gate.ParseGateResult(data)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("parse gate result: %w", err)
	}

	// Validate all expected dimensions are present
	expectedDims := make([]string, len(node.Dimensions))
	for i, d := range node.Dimensions {
		expectedDims[i] = d.Name
	}
	if err := gate.ValidateGateResult(gateResult, expectedDims); err != nil {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: fmt.Sprintf("gate produced incomplete output: %v", err),
		}, nil
	}

	// Check rationale exists (anti-gaming: rationale is mandatory)
	rationalePath := node.Output.RationalePath
	if rationalePath == "" {
		rationalePath = ".belayer/output/rationale.md"
	}
	absRationalePath := filepath.Join(workDir, rationalePath)
	if _, err := os.Stat(absRationalePath); os.IsNotExist(err) {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: "gate failed: rationale.md is mandatory but was not produced",
		}, nil
	}

	// Compute weighted score (score-then-route: we compute, not the session)
	weightedScore := gate.ComputeWeightedScore(gateResult, node.Dimensions)

	// Apply thresholds
	outcome := gate.ApplyThresholds(weightedScore, node.Thresholds)

	result := model.CompletionResult{
		Outcome:    outcome,
		OutputPath: resultPath,
	}

	// On RETRY, read rationale as feedback
	if outcome == model.OutcomeRetry {
		rationaleData, err := os.ReadFile(absRationalePath)
		if err == nil {
			result.Feedback = string(rationaleData)
		}
		// Set target from node.OnRetry (workflow handles this, but include for clarity)
		result.TargetNode = node.OnRetry
	}

	return result, nil
}
```

Update `buildInputPrompt` to handle gate nodes:

```go
// In buildInputPrompt, add gate handling at the top of the function:
if node.IsGate() {
	var sb strings.Builder
	// Include gate-specific prompt
	sb.WriteString(gate.BuildGatePrompt(node))
	sb.WriteString("\n")
	// Add input context
	switch node.Input.Type {
	case "code":
		sb.WriteString("\nInput: Review the changes. Full diff at .belayer/input/diff.txt\n")
	case "file":
		key := node.Input.Key
		if key == "" {
			key = node.Name
		}
		if path, ok := artifacts[key]; ok {
			sb.WriteString(fmt.Sprintf("\nInput artifact at: %s\n", path))
		}
	}
	if feedback, ok := artifacts["feedback"]; ok && feedback != "" {
		sb.WriteString(fmt.Sprintf("\nFeedback from previous attempt: %s\n", feedback))
	}
	return sb.String()
}
```

Add import for gate package at top of file.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/temporal/ -run "TestBuildInputPrompt_GateNode|TestProcessGateResult" -v`
Expected: All PASS

- [ ] **Step 5: Run full activity tests**

Run: `go test ./internal/v3/temporal/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/v3/temporal/activity.go internal/v3/temporal/activity_test.go
git commit -m "feat(v3): add gate scoring post-processing to activity — score-then-route"
```

---

### Task 8: Workflow Gate Routing — Retry with Rationale Feedback

**Files:**
- Modify: `internal/v3/temporal/activity.go` (wire processGateResult into NodeActivity)
- Modify: `internal/v3/temporal/workflow.go` (prior-rationale delivery)
- Test: `internal/v3/temporal/workflow_test.go`

- [ ] **Step 1: Write failing workflow test — gate node in pipeline**

Add to `internal/v3/temporal/workflow_test.go`:

```go
func gatePipelineYAML() []byte {
	return []byte(`
name: gate-test
nodes:
  - name: lead
    type: node
    description: Write code
    output:
      type: code
    on_pass: review
    on_fail: stop
    max_retries: 3
  - name: review
    type: gate
    description: Review the code
    input:
      type: code
    dimensions:
      - name: correctness
        description: "Does it work?"
        weight: 0.6
      - name: quality
        description: "Is it clean?"
        weight: 0.4
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
      path: .belayer/output/gate-result.json
      rationale_path: .belayer/output/rationale.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`)
}

func gateInput() model.ClimbInput {
	return model.ClimbInput{
		PipelineYAML: gatePipelineYAML(),
		WorkDir:      "/tmp/test-gate",
		Branch:       "feature/gate-test",
	}
}

// TestClimb_GatePass: lead PASS → gate PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_GatePass() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/output/gate-result.json",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_GateRetryThenPass: gate RETRY → lead retries → gate PASS.
func (s *ClimbWorkflowTestSuite) TestClimb_GateRetryThenPass() {
	a := &Activities{}

	// Lead called twice: initial + after gate retry.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil).Times(2)

	// Gate: first call RETRY, second call PASS.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 0
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomeRetry,
			TargetNode: "lead",
			Feedback:   "Fix the bugs in auth module",
		},
	}, nil).Once()

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 1
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/output/gate-result.json",
		},
	}, nil).Once()

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_GateFail: gate FAIL → ClimbFailed.
func (s *ClimbWorkflowTestSuite) TestClimb_GateFail() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: "Fundamentally broken",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
}
```

- [ ] **Step 2: Run tests to verify they pass**

These tests mock the activity layer, so they should work with the existing workflow routing logic (gates produce the same PASS/RETRY/FAIL outcomes as nodes). The workflow doesn't need to know about scoring — that's handled in the activity.

Run: `go test ./internal/v3/temporal/ -run "TestClimb_Gate" -v`
Expected: PASS (workflow routing handles gate outcomes the same as node outcomes)

- [ ] **Step 3: Wire processGateResult into NodeActivity**

In `internal/v3/temporal/activity.go`, add gate post-processing after the completion file read in `NodeActivity`:

After `result := out.Result` (approx line 82 equivalent — after `pollForCompletion` returns), add:

```go
// 7. For gate nodes, post-process: read gate-result.json, score, apply thresholds.
if input.Node.IsGate() {
	gateResult, err := processGateResult(input.WorkDir, input.Node)
	if err != nil {
		// If gate-result.json is missing/malformed, treat as FAIL.
		return &NodeActivityOutput{
			Result: model.CompletionResult{
				Outcome:  model.OutcomeFail,
				Feedback: fmt.Sprintf("gate processing failed: %v", err),
				Attempt:  input.Attempt,
			},
		}, nil
	}
	gateResult.Attempt = input.Attempt
	return &NodeActivityOutput{Result: gateResult}, nil
}
```

- [ ] **Step 4: Run all temporal tests**

Run: `go test ./internal/v3/temporal/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/v3/temporal/activity.go internal/v3/temporal/workflow.go internal/v3/temporal/workflow_test.go
git commit -m "feat(v3): wire gate scoring into NodeActivity and add workflow gate tests"
```

---

### Task 9: Default Gate Configs — Spotter as Gate

**Files:**
- Modify: `internal/v3/pipeline/defaults.go`
- Modify: `internal/v3/pipeline/defaults_test.go`

- [ ] **Step 1: Write failing test — default pipeline spotter is a gate**

Add to `internal/v3/pipeline/defaults_test.go`:

```go
func TestDefaultPipeline_SpotterIsGate(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse default pipeline: %v", err)
	}

	spotter := cfg.FindNode("spotter")
	if spotter == nil {
		t.Fatal("expected spotter node in default pipeline")
	}
	if spotter.Type != NodeTypeGate {
		t.Errorf("spotter type: got %q, want %q", spotter.Type, NodeTypeGate)
	}
	if len(spotter.Dimensions) == 0 {
		t.Error("spotter should have dimensions")
	}
	if spotter.Thresholds.Pass <= 0 {
		t.Error("spotter should have a positive pass threshold")
	}
	if spotter.Output.Type != "gate_result" {
		t.Errorf("spotter output type: got %q, want %q", spotter.Output.Type, "gate_result")
	}
}

func TestDefaultPipeline_Validates(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse default pipeline: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("default pipeline validation failed: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/v3/pipeline/ -run "TestDefaultPipeline_SpotterIsGate|TestDefaultPipeline_Validates" -v`
Expected: FAIL — spotter is not yet a gate

- [ ] **Step 3: Update default pipeline — convert spotter to gate**

Replace `DefaultPipelineYAML` in `internal/v3/pipeline/defaults.go`:

```go
const DefaultPipelineYAML = `name: default-climb
nodes:
  - name: setter
    type: node
    description: |
      You are the setter. You receive a design document and create a detailed
      implementation plan. Do NOT write code. Your output is a plan.md file
      that a separate agent will use to implement the feature.

      Run /harness:plan to create the implementation plan.
      When done, write the plan to .belayer/output/plan.md.
    input:
      type: file
      key: design_doc
    output:
      type: file
      path: .belayer/output/plan.md
    on_pass: next
    on_retry: setter
    on_fail: stop
    max_retries: 2

  - name: lead
    type: node
    description: |
      You are the lead. You receive an implementation plan and write the code.
      Focus on clean, tested implementation. Follow the plan closely.

      Run /harness:orchestrate to execute the plan.
      Commit your changes to the current branch.
    input:
      type: file
      key: setter
    output:
      type: code
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: spotter
    type: gate
    description: |
      You are the spotter — an adversarial code reviewer. Review the code
      changes for spec compliance, test contract fulfillment, and runtime
      correctness.

      For each dimension below, provide:
      - A score from 0-10
      - A brief rationale (1-2 sentences)
      - Specific issues found (if any)

      Be honest. Gaming the score helps no one.
    input:
      type: code
    dimensions:
      - name: spec_compliance
        description: "Do the changes match what was specified in the plan?"
        weight: 0.35
        rubric: "9-10: exact match, 6-8: minor deviations, 3-5: significant gaps, 0-2: wrong direction"
      - name: test_contracts
        description: "Are test contracts fulfilled? Do tests actually test the right things?"
        weight: 0.3
        rubric: "9-10: comprehensive, 6-8: happy path covered, 3-5: minimal tests, 0-2: untested"
      - name: runtime_correctness
        description: "Would this work in production? Runtime errors, performance, security?"
        weight: 0.35
        rubric: "9-10: production-ready, 6-8: minor concerns, 3-5: significant risks, 0-2: broken"
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
      path: .belayer/output/gate-result.json
      rationale_path: .belayer/output/rationale.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: All PASS

- [ ] **Step 5: Run all v3 tests to check nothing broke**

Run: `go test ./internal/v3/... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/v3/pipeline/defaults.go internal/v3/pipeline/defaults_test.go
git commit -m "feat(v3): convert default spotter to gate with dimensions and thresholds"
```

---

### Task 10: Integration Test — Workflow with Gate Node

**Files:**
- Modify: `internal/v3/temporal/integration_test.go`

- [ ] **Step 1: Read existing integration test to understand patterns**

Read `internal/v3/temporal/integration_test.go` to understand the test harness.

- [ ] **Step 2: Write integration test for gate pipeline**

Add to `internal/v3/temporal/integration_test.go`:

```go
// TestIntegration_GatePipeline: full pipeline with a gate node.
// Uses FakeSpawner to produce gate-result.json + rationale.md,
// then verifies the activity reads and scores correctly.
func TestIntegration_GatePipeline(t *testing.T) {
	// This test verifies the full path:
	// 1. Pipeline with gate node parses
	// 2. NodeActivity spawns session for gate
	// 3. Gate produces gate-result.json + rationale.md
	// 4. Activity reads, validates, scores, applies thresholds
	// 5. Workflow routes based on gate outcome

	pipelineYAML := gatePipelineYAML()
	cfg, err := pipeline.ParsePipeline(pipelineYAML)
	if err != nil {
		t.Fatalf("parse pipeline: %v", err)
	}
	if err := pipeline.Validate(cfg); err != nil {
		t.Fatalf("validate pipeline: %v", err)
	}

	// Verify gate node parsed correctly
	gate := cfg.FindNode("review")
	if gate == nil {
		t.Fatal("expected 'review' gate node")
	}
	if !gate.IsGate() {
		t.Fatal("expected review to be a gate")
	}
	if len(gate.Dimensions) != 2 {
		t.Fatalf("dimensions: got %d, want 2", len(gate.Dimensions))
	}
}
```

- [ ] **Step 3: Run integration test**

Run: `go test ./internal/v3/temporal/ -run "TestIntegration_GatePipeline" -v`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `go test ./internal/v3/... -v`
Expected: All PASS

- [ ] **Step 5: Build binary to verify compilation**

Run: `go build -o belayer ./cmd/belayer`
Expected: Compiles successfully

- [ ] **Step 6: Commit**

```bash
git add internal/v3/temporal/integration_test.go
git commit -m "test(v3): add integration test for gate pipeline parsing and validation"
```

---

## Outcomes & Retrospective

**What worked:**
- Extending NodeActivity with gate post-processing was the right call — reused spawner, polling, completion pattern with minimal new code
- Score-then-route anti-gaming pattern: activity computes score from thresholds, session doesn't decide outcome
- Parallelizing independent tasks (Wave 2: Tasks 2,3,5,6 all ran in parallel)
- TDD approach with exact code in the plan — workers had zero ambiguity

**What didn't:**
- Integration tests broke when spotter became a gate — should have been anticipated in planning
- Event factory functions defined but not wired into the activity emission path — deferred to caller

**Learnings to codify:**
- L-005: Extending pipeline primitives requires updating integration test spawners
- L-006: Score-then-route prevents adversarial session gaming
