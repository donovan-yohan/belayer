# Belayer v3: Temporal Activity Pipeline Implementation Plan

> **Status**: Active | **Created**: 2026-03-20 | **Last Updated**: 2026-03-20T20:27
> **Design Doc**: `~/.gstack/projects/donovan-yohan-belayer/donovanyohan-master-design-20260320-190000.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Implement `belayer climb --file design.md` → Temporal workflow → setter → lead → spotter → implementation branch with PASS/RETRY/FAIL routing.

**Architecture:** Each pipeline node is a Temporal Activity that spawns an interactive Claude Code session via tmux. The session receives input artifacts, a natural language role prompt via `--append-system-prompt`, and a Stop hook that calls `belayer node-complete`. File-based rendezvous (completion files) replaces Temporal Signals — the activity polls for a JSON file written by the Stop hook. YAML pipeline config with natural language node descriptions.

**Tech Stack:** Go 1.24, Temporal SDK (`go.temporal.io/sdk`), `gopkg.in/yaml.v3`, Cobra CLI, testify, Temporal test suite

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-20 | Design | Activity Per Node (Approach A) | Simplest model: 1 node = 1 activity. Temporal handles retries, timeouts, visibility. Upgrade to child workflows later if needed. |
| 2026-03-20 | Design | File-based completion over Temporal Signals | Eliminates signal complexity. Activity polls for attempt-scoped completion file written by Stop hook. |
| 2026-03-20 | Design | Tmux for session PTY (MVP) | Claude Code needs interactive TTY. Existing `internal/tmux` package provides this. Raw subprocess (pty library) is a future simplification. |
| 2026-03-20 | Design | Single Stop hook for lifecycle | Hook calls `belayer node-complete` which reads output, determines outcome, writes completion file. |
| 2026-03-20 | Design | v3 in `internal/v3/` clean break | Same pattern as v1→v2. All three coexist; CLI wiring adds v3 commands alongside existing ones. |
| 2026-03-20 | Design | Attempt-scoped completion files | `.belayer/completion/<task-id>-<node>-attempt-<N>.json` prevents stale files from attempt 1 satisfying attempt 2. |
| 2026-03-20 | Review | Fixed 9 issues from plan review | Feedback file writing, git branch creation, diff base (main..HEAD), event logger integration, Temporal API types, import fixes, CLI wiring target |

## Progress

- [x] Task 1: v3 Domain Types _(completed 2026-03-20)_
- [x] Task 2: Pipeline Config Model _(completed 2026-03-20)_
- [x] Task 3: Pipeline Parser & Validator _(completed 2026-03-20)_
- [x] Task 4: Default Pipeline Config _(completed 2026-03-20)_
- [x] Task 5: Event Logger _(completed 2026-03-20)_
- [x] Task 6: Outcome Detection _(completed 2026-03-20)_
- [x] Task 7: Hooks Config Generator _(completed 2026-03-20)_
- [x] Task 8: `belayer node-complete` Command _(completed 2026-03-20)_
- [x] Task 9: Session Spawner _(completed 2026-03-20)_
- [x] Task 10: Node Activity _(completed 2026-03-20)_
- [x] Task 11: Climb Workflow _(completed 2026-03-20)_
- [x] Task 12: `belayer climb` CLI Command _(completed 2026-03-20)_
- [x] Task 13: Status Command & CLI Wiring _(completed 2026-03-20)_
- [x] Task 14: Integration Test _(completed 2026-03-20)_

## Surprises & Discoveries

| Date | Surprise | Impact | Resolution |
|------|----------|--------|------------|
| 2026-03-20 | `activity.RecordHeartbeat` panics outside Temporal worker context | Tests couldn't call heartbeat directly | Added `recordHeartbeat` wrapper with `recover()` in activity.go — idiomatic for Temporal Go SDK |
| 2026-03-20 | Temporal test env runs activities synchronously — ticker never fires | Integration tests with fakeSpawner would hang | Added pre-tick check in `pollForCompletion` before starting ticker — also improves production behavior |

## Plan Drift

| Task | Plan Said | Actually Did | Why |
|------|-----------|--------------|-----|
| 13 | `newClimbCmd()` (lowercase) | `NewClimbCmd()` (uppercase) | Task 12 implemented exported constructors; Task 13 matched the actual code |
| 10 | `pollForCompletion` with ticker only | Added pre-tick immediate check | Temporal test env doesn't advance real time; also better for production |

---

## File Structure

```
internal/v3/
├── model/
│   ├── types.go              # NodeOutcome, CompletionResult, ClimbInput/Output
│   └── types_test.go
├── pipeline/
│   ├── model.go              # PipelineConfig, NodeConfig, InputConfig, OutputConfig
│   ├── parser.go             # ParsePipeline from YAML bytes/file
│   ├── parser_test.go
│   ├── validate.go           # Pipeline validation rules
│   ├── validate_test.go
│   ├── defaults.go           # Default setter→lead→spotter pipeline YAML
│   └── defaults_test.go
├── events/
│   ├── types.go              # Event type constants and constructors
│   ├── logger.go             # JSONL event file writer
│   └── logger_test.go
├── outcome/
│   ├── detect.go             # Outcome detection (verdict.txt > output first line > type default)
│   └── detect_test.go
├── session/
│   ├── hooks.go              # Generate hooks.json for --settings
│   ├── hooks_test.go
│   ├── spawner.go            # Spawn claude session in tmux with env vars
│   └── spawner_test.go
├── temporal/
│   ├── constants.go          # Task queue name, signal channel name
│   ├── activity.go           # NodeActivity: spawn session, heartbeat, poll completion
│   ├── activity_test.go
│   ├── workflow.go           # ClimbWorkflow: node sequencing, retry, artifact passing
│   └── workflow_test.go
└── cli/
    ├── root.go               # RegisterV3Commands
    ├── climb.go              # belayer climb --file/--prompt/--node/--detach
    ├── node_complete.go      # belayer node-complete (Stop hook handler)
    ├── node_complete_test.go
    └── status.go             # belayer status
```

---

### Task 1: v3 Domain Types

**Files:**
- Create: `internal/v3/model/types.go`
- Create: `internal/v3/model/types_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/v3/model/types_test.go
package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeOutcome_Valid(t *testing.T) {
	assert.True(t, OutcomePass.IsValid())
	assert.True(t, OutcomeRetry.IsValid())
	assert.True(t, OutcomeFail.IsValid())
	assert.False(t, NodeOutcome("bogus").IsValid())
}

func TestCompletionResult_JSON(t *testing.T) {
	cr := CompletionResult{
		Outcome:    OutcomePass,
		OutputPath: "plan.md",
		Attempt:    1,
	}
	data, err := json.Marshal(cr)
	require.NoError(t, err)

	var decoded CompletionResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, cr, decoded)
}

func TestCompletionResult_RetryWithTarget(t *testing.T) {
	cr := CompletionResult{
		Outcome:    OutcomeRetry,
		TargetNode: "lead",
		Feedback:   "address review comments",
		Attempt:    2,
	}
	assert.Equal(t, OutcomeRetry, cr.Outcome)
	assert.Equal(t, "lead", cr.TargetNode)
}

func TestClimbStatus_Valid(t *testing.T) {
	assert.True(t, ClimbActive.IsValid())
	assert.True(t, ClimbCompleted.IsValid())
	assert.True(t, ClimbFailed.IsValid())
	assert.False(t, ClimbStatus("bogus").IsValid())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/model/ -v`
Expected: Compilation failure — package doesn't exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/v3/model/types.go
package model

// NodeOutcome is the result of a pipeline node execution.
type NodeOutcome string

const (
	OutcomePass  NodeOutcome = "PASS"
	OutcomeRetry NodeOutcome = "RETRY"
	OutcomeFail  NodeOutcome = "FAIL"
)

func (o NodeOutcome) IsValid() bool {
	switch o {
	case OutcomePass, OutcomeRetry, OutcomeFail:
		return true
	}
	return false
}

// CompletionResult is written by `belayer node-complete` and read by the activity.
// Attempt-scoped: stored at .belayer/completion/<task-id>-<node>-attempt-<N>.json
type CompletionResult struct {
	Outcome    NodeOutcome `json:"outcome"`
	OutputPath string      `json:"output_path,omitempty"`
	TargetNode string      `json:"target_node,omitempty"` // For RETRY: which node to loop back to
	Feedback   string      `json:"feedback,omitempty"`    // For RETRY: feedback file path or message
	Attempt    int         `json:"attempt"`
}

// ClimbStatus tracks the state of a pipeline run (belayer's view).
type ClimbStatus string

const (
	ClimbActive    ClimbStatus = "active"
	ClimbCompleted ClimbStatus = "completed"
	ClimbFailed    ClimbStatus = "failed"
)

func (s ClimbStatus) IsValid() bool {
	switch s {
	case ClimbActive, ClimbCompleted, ClimbFailed:
		return true
	}
	return false
}

// ClimbInput is the input to the Climb workflow.
type ClimbInput struct {
	Description  string `json:"description"`
	DesignFile   string `json:"design_file,omitempty"`
	PipelineFile string `json:"pipeline_file,omitempty"`
	PipelineYAML []byte `json:"pipeline_yaml,omitempty"` // Serialized pipeline config
	FromNode     string `json:"from_node,omitempty"`     // Resume from this node
	InputPath    string `json:"input_path,omitempty"`    // Input artifact for --from node
	WorkDir      string `json:"work_dir"`
	Branch       string `json:"branch"` // Git branch name: belayer/climb-<workflow-id>
}

// ClimbOutput is the output of a completed Climb workflow.
type ClimbOutput struct {
	Status      ClimbStatus        `json:"status"`
	NodeOutputs map[string]string  `json:"node_outputs"` // node name → output artifact path
	Message     string             `json:"message,omitempty"`
	Branch      string             `json:"branch"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/model/ -v`
Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/model/
git commit -m "feat(v3): add domain types — NodeOutcome, CompletionResult, ClimbInput/Output"
```

---

### Task 2: Pipeline Config Model

**Files:**
- Create: `internal/v3/pipeline/model.go`

- [ ] **Step 1: Write the pipeline config types**

```go
// internal/v3/pipeline/model.go
package pipeline

// PipelineConfig is the top-level pipeline definition (belayer-pipeline.yaml).
type PipelineConfig struct {
	Name  string       `yaml:"name" json:"name"`
	Nodes []NodeConfig `yaml:"nodes" json:"nodes"`
}

// NodeConfig defines a single pipeline node.
type NodeConfig struct {
	Name        string       `yaml:"name" json:"name"`
	Description string       `yaml:"description" json:"description"` // Natural language role prompt
	Input       InputConfig  `yaml:"input" json:"input"`
	Output      OutputConfig `yaml:"output" json:"output"`
	OnPass      string       `yaml:"on_pass" json:"on_pass"`     // "next", "stop", or node name
	OnRetry     string       `yaml:"on_retry" json:"on_retry"`   // node name, "self", or "stop"
	OnFail      string       `yaml:"on_fail" json:"on_fail"`     // "stop" or node name
	MaxRetries  int          `yaml:"max_retries" json:"max_retries"`
}

// InputConfig specifies what a node receives.
type InputConfig struct {
	Type string `yaml:"type" json:"type"` // "file" or "code"
	Key  string `yaml:"key" json:"key"`   // Artifact key from upstream node output
}

// OutputConfig specifies what a node produces.
type OutputConfig struct {
	Type string `yaml:"type" json:"type"` // "file" or "code"
	Path string `yaml:"path,omitempty" json:"path,omitempty"` // For type=file: expected output path
	Key  string `yaml:"key,omitempty" json:"key,omitempty"`   // Artifact key (defaults to node name)
}

// OutputKey returns the artifact key for this node's output.
// Defaults to the node name if no explicit key is set.
func (n *NodeConfig) OutputKey() string {
	if n.Output.Key != "" {
		return n.Output.Key
	}
	return n.Name
}

// FindNode returns the node with the given name, or nil.
func (p *PipelineConfig) FindNode(name string) *NodeConfig {
	for i := range p.Nodes {
		if p.Nodes[i].Name == name {
			return &p.Nodes[i]
		}
	}
	return nil
}

// NodeNames returns all node names in order.
func (p *PipelineConfig) NodeNames() []string {
	names := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		names[i] = n.Name
	}
	return names
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/v3/pipeline/model.go
git commit -m "feat(v3): add pipeline config model — PipelineConfig, NodeConfig, InputConfig, OutputConfig"
```

---

### Task 3: Pipeline Parser & Validator

**Files:**
- Create: `internal/v3/pipeline/parser.go`
- Create: `internal/v3/pipeline/parser_test.go`
- Create: `internal/v3/pipeline/validate.go`
- Create: `internal/v3/pipeline/validate_test.go`

- [ ] **Step 1: Write parser tests**

```go
// internal/v3/pipeline/parser_test.go
package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePipeline_ValidYAML(t *testing.T) {
	yaml := `
name: test-pipeline
nodes:
  - name: setter
    description: "Create a plan"
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
    description: "Write code"
    input:
      type: file
      key: setter
    output:
      type: code
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3
`
	cfg, err := ParsePipeline([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "test-pipeline", cfg.Name)
	assert.Len(t, cfg.Nodes, 2)
	assert.Equal(t, "setter", cfg.Nodes[0].Name)
	assert.Equal(t, "lead", cfg.Nodes[1].Name)
	assert.Equal(t, 2, cfg.Nodes[0].MaxRetries)
	assert.Equal(t, "file", cfg.Nodes[0].Output.Type)
	assert.Equal(t, ".belayer/output/plan.md", cfg.Nodes[0].Output.Path)
}

func TestParsePipeline_InvalidYAML(t *testing.T) {
	_, err := ParsePipeline([]byte("not: [valid: yaml"))
	assert.Error(t, err)
}

func TestParsePipelineFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	yaml := `
name: from-file
nodes:
  - name: setter
    description: "Plan"
    input: { type: file, key: design_doc }
    output: { type: file, path: plan.md }
    on_pass: next
    on_fail: stop
    max_retries: 1
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	cfg, err := ParsePipelineFile(path)
	require.NoError(t, err)
	assert.Equal(t, "from-file", cfg.Name)
}

func TestParsePipelineFile_NotFound(t *testing.T) {
	_, err := ParsePipelineFile("/nonexistent/file.yaml")
	assert.Error(t, err)
}

func TestNodeConfig_OutputKey(t *testing.T) {
	n := NodeConfig{Name: "setter", Output: OutputConfig{Key: "plan"}}
	assert.Equal(t, "plan", n.OutputKey())

	n2 := NodeConfig{Name: "setter", Output: OutputConfig{}}
	assert.Equal(t, "setter", n2.OutputKey())
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -run TestParse -v`
Expected: Compilation failure — ParsePipeline/ParsePipelineFile not defined.

- [ ] **Step 3: Implement parser**

```go
// internal/v3/pipeline/parser.go
package pipeline

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParsePipeline parses a PipelineConfig from YAML bytes.
func ParsePipeline(data []byte) (*PipelineConfig, error) {
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("pipeline parse: %w", err)
	}
	return &cfg, nil
}

// ParsePipelineFile reads and parses a PipelineConfig from a YAML file.
func ParsePipelineFile(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pipeline file: %w", err)
	}
	return ParsePipeline(data)
}
```

- [ ] **Step 4: Run parser tests to verify they pass**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -run TestParse -v`
Expected: All tests PASS.

- [ ] **Step 5: Write validator tests**

```go
// internal/v3/pipeline/validate_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate_ValidPipeline(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "valid",
		Nodes: []NodeConfig{
			{Name: "setter", Input: InputConfig{Type: "file", Key: "design_doc"},
				Output: OutputConfig{Type: "file", Path: "plan.md"}, OnPass: "next", OnFail: "stop", MaxRetries: 1},
			{Name: "lead", Input: InputConfig{Type: "file", Key: "setter"},
				Output: OutputConfig{Type: "code"}, OnPass: "next", OnFail: "stop", MaxRetries: 2},
		},
	}
	assert.NoError(t, Validate(cfg))
}

func TestValidate_EmptyName(t *testing.T) {
	cfg := &PipelineConfig{Nodes: []NodeConfig{{Name: "a"}}}
	assert.ErrorContains(t, Validate(cfg), "pipeline name")
}

func TestValidate_NoNodes(t *testing.T) {
	cfg := &PipelineConfig{Name: "empty"}
	assert.ErrorContains(t, Validate(cfg), "at least one node")
}

func TestValidate_DuplicateNodeNames(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "dupes",
		Nodes: []NodeConfig{
			{Name: "a", Output: OutputConfig{Type: "file"}, OnPass: "next", OnFail: "stop"},
			{Name: "a", Output: OutputConfig{Type: "file"}, OnPass: "next", OnFail: "stop"},
		},
	}
	assert.ErrorContains(t, Validate(cfg), "duplicate node name")
}

func TestValidate_InvalidOnRetryTarget(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "bad-retry",
		Nodes: []NodeConfig{
			{Name: "a", Output: OutputConfig{Type: "file"}, OnPass: "next", OnRetry: "nonexistent", OnFail: "stop"},
		},
	}
	assert.ErrorContains(t, Validate(cfg), "on_retry references unknown node")
}

func TestValidate_OnRetrySelf(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "self-retry",
		Nodes: []NodeConfig{
			{Name: "a", Output: OutputConfig{Type: "file"}, OnPass: "next", OnRetry: "self", OnFail: "stop"},
		},
	}
	assert.NoError(t, Validate(cfg))
}

func TestValidate_MissingOutputType(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "no-output",
		Nodes: []NodeConfig{
			{Name: "a", Output: OutputConfig{}, OnPass: "next", OnFail: "stop"},
		},
	}
	assert.ErrorContains(t, Validate(cfg), "output.type")
}
```

- [ ] **Step 6: Run validator tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -run TestValidate -v`
Expected: Compilation failure — Validate not defined.

- [ ] **Step 7: Implement validator**

```go
// internal/v3/pipeline/validate.go
package pipeline

import "fmt"

// Validate checks a PipelineConfig for structural correctness.
func Validate(cfg *PipelineConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}
	if len(cfg.Nodes) == 0 {
		return fmt.Errorf("pipeline must have at least one node")
	}

	names := make(map[string]bool, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if n.Name == "" {
			return fmt.Errorf("node name is required")
		}
		if names[n.Name] {
			return fmt.Errorf("duplicate node name: %q", n.Name)
		}
		names[n.Name] = true
	}

	for _, n := range cfg.Nodes {
		if n.Output.Type == "" {
			return fmt.Errorf("node %q: output.type is required", n.Name)
		}
		if n.Output.Type != "file" && n.Output.Type != "code" {
			return fmt.Errorf("node %q: output.type must be 'file' or 'code', got %q", n.Name, n.Output.Type)
		}
		if err := validateTarget(n.Name, "on_pass", n.OnPass, names); err != nil {
			return err
		}
		if err := validateTarget(n.Name, "on_retry", n.OnRetry, names); err != nil {
			return err
		}
		if err := validateTarget(n.Name, "on_fail", n.OnFail, names); err != nil {
			return err
		}
	}
	return nil
}

func validateTarget(nodeName, field, target string, validNames map[string]bool) error {
	if target == "" || target == "next" || target == "stop" || target == "self" {
		return nil
	}
	if !validNames[target] {
		return fmt.Errorf("node %q: %s references unknown node %q", nodeName, field, target)
	}
	return nil
}
```

- [ ] **Step 8: Run all pipeline tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -v`
Expected: All tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/v3/pipeline/
git commit -m "feat(v3): add pipeline YAML parser and validator"
```

---

### Task 4: Default Pipeline Config

**Files:**
- Create: `internal/v3/pipeline/defaults.go`
- Create: `internal/v3/pipeline/defaults_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/v3/pipeline/defaults_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPipeline_Parses(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	require.NoError(t, err)
	assert.Equal(t, "default-climb", cfg.Name)
	assert.Len(t, cfg.Nodes, 3)
	assert.Equal(t, "setter", cfg.Nodes[0].Name)
	assert.Equal(t, "lead", cfg.Nodes[1].Name)
	assert.Equal(t, "spotter", cfg.Nodes[2].Name)
}

func TestDefaultPipeline_Validates(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	require.NoError(t, err)
	assert.NoError(t, Validate(cfg))
}

func TestDefaultPipeline_SpotterRetriesToLead(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	require.NoError(t, err)
	spotter := cfg.FindNode("spotter")
	require.NotNil(t, spotter)
	assert.Equal(t, "lead", spotter.OnRetry)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -run TestDefault -v`
Expected: Compilation failure — DefaultPipelineYAML not defined.

- [ ] **Step 3: Implement default pipeline**

```go
// internal/v3/pipeline/defaults.go
package pipeline

// DefaultPipelineYAML is the built-in pipeline config: setter → lead → spotter.
// Node descriptions are natural language prompts that tell the Claude session what to do.
const DefaultPipelineYAML = `
name: default-climb
nodes:
  - name: setter
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
    description: |
      You are the spotter — an adversarial code reviewer. You receive a git
      diff and review it for quality, correctness, and adherence to the plan.

      Run /harness:review to review the changes.
      Write your verdict to .belayer/output/review.md.
      Format: start with PASS, RETRY, or FAIL on the first line.
      If RETRY, include specific feedback for the lead to address.
    input:
      type: code
    output:
      type: file
      path: .belayer/output/review.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/pipeline/ -run TestDefault -v`
Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/pipeline/defaults.go internal/v3/pipeline/defaults_test.go
git commit -m "feat(v3): add default pipeline config (setter → lead → spotter)"
```

---

### Task 5: Event Logger

**Files:**
- Create: `internal/v3/events/types.go`
- Create: `internal/v3/events/logger.go`
- Create: `internal/v3/events/logger_test.go`

- [ ] **Step 1: Write event types**

```go
// internal/v3/events/types.go
package events

import "time"

// Event is a single structured pipeline event.
type Event struct {
	Timestamp  time.Time         `json:"ts"`
	Type       string            `json:"event"`
	Node       string            `json:"node,omitempty"`
	Outcome    string            `json:"outcome,omitempty"`
	Target     string            `json:"target,omitempty"`
	Attempt    int               `json:"attempt,omitempty"`
	DurationS  float64           `json:"duration_s,omitempty"`
	WorkflowID string            `json:"workflow_id,omitempty"`
	Pipeline   string            `json:"pipeline,omitempty"`
	Input      string            `json:"input,omitempty"`
	Feedback   string            `json:"feedback,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Message    string            `json:"message,omitempty"`
}

func PipelineStarted(workflowID, pipeline, input string) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_started", WorkflowID: workflowID, Pipeline: pipeline, Input: input}
}

func NodeStarted(node string, attempt int) Event {
	return Event{Timestamp: time.Now(), Type: "node_started", Node: node, Attempt: attempt}
}

func NodeCompleted(node, outcome string, durationS float64) Event {
	return Event{Timestamp: time.Now(), Type: "node_completed", Node: node, Outcome: outcome, DurationS: durationS}
}

func NodeRetry(node, target, feedback string) Event {
	return Event{Timestamp: time.Now(), Type: "node_retry", Node: node, Target: target, Feedback: feedback}
}

func PipelineCompleted(outcome string, durationS float64) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_completed", Outcome: outcome, DurationS: durationS}
}

func PipelineFailed(node, reason string) Event {
	return Event{Timestamp: time.Now(), Type: "pipeline_failed", Node: node, Reason: reason}
}
```

- [ ] **Step 2: Write logger tests**

```go
// internal/v3/events/logger_test.go
package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")

	logger, err := NewLogger(logPath)
	require.NoError(t, err)
	defer logger.Close()

	require.NoError(t, logger.Log(PipelineStarted("wf-1", "default-climb", "design.md")))
	require.NoError(t, logger.Log(NodeStarted("setter", 1)))

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, 2)

	var evt Event
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &evt))
	assert.Equal(t, "pipeline_started", evt.Type)
	assert.Equal(t, "wf-1", evt.WorkflowID)
}

func TestLogger_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nested", "deep", "events.jsonl")

	logger, err := NewLogger(logPath)
	require.NoError(t, err)
	defer logger.Close()

	require.NoError(t, logger.Log(PipelineStarted("wf-1", "p", "i")))

	_, err = os.Stat(logPath)
	assert.NoError(t, err)
}
```

- [ ] **Step 3: Implement logger**

```go
// internal/v3/events/logger.go
package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Logger writes structured events to a JSONL file (one JSON object per line).
type Logger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewLogger creates a Logger that appends to the given file path.
// Creates parent directories if needed.
func NewLogger(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	return &Logger{file: f, enc: json.NewEncoder(f)}, nil
}

// Log writes a single event as a JSON line.
func (l *Logger) Log(evt Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enc.Encode(evt)
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	return l.file.Close()
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/events/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/events/
git commit -m "feat(v3): add JSONL event logger for pipeline observability"
```

---

### Task 6: Outcome Detection

**Files:**
- Create: `internal/v3/outcome/detect.go`
- Create: `internal/v3/outcome/detect_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/v3/outcome/detect_test.go
package outcome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func TestDetect_VerdictFile_Pass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".belayer/output/verdict.txt", "PASS\nLooks good.")
	result := Detect(fileNode("spotter"), dir, 1)
	assert.Equal(t, model.OutcomePass, result.Outcome)
}

func TestDetect_VerdictFile_Retry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".belayer/output/verdict.txt", "RETRY lead\nFix the error handling.")
	result := Detect(fileNode("spotter"), dir, 1)
	assert.Equal(t, model.OutcomeRetry, result.Outcome)
	assert.Equal(t, "lead", result.TargetNode)
}

func TestDetect_VerdictFile_Fail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".belayer/output/verdict.txt", "FAIL\nFundamental design problem.")
	result := Detect(fileNode("spotter"), dir, 1)
	assert.Equal(t, model.OutcomeFail, result.Outcome)
}

func TestDetect_OutputFile_FirstLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".belayer/output/review.md", "PASS\n## Review\nAll good.")
	node := pipeline.NodeConfig{
		Name:   "spotter",
		Output: pipeline.OutputConfig{Type: "file", Path: ".belayer/output/review.md"},
	}
	result := Detect(&node, dir, 1)
	assert.Equal(t, model.OutcomePass, result.Outcome)
}

func TestDetect_FileType_ExistsIsPass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".belayer/output/plan.md", "## Plan\nStep 1...")
	node := pipeline.NodeConfig{
		Name:   "setter",
		Output: pipeline.OutputConfig{Type: "file", Path: ".belayer/output/plan.md"},
	}
	result := Detect(&node, dir, 1)
	assert.Equal(t, model.OutcomePass, result.Outcome)
	assert.Equal(t, ".belayer/output/plan.md", result.OutputPath)
}

func TestDetect_FileType_MissingIsFail(t *testing.T) {
	dir := t.TempDir()
	node := pipeline.NodeConfig{
		Name:   "setter",
		Output: pipeline.OutputConfig{Type: "file", Path: ".belayer/output/plan.md"},
	}
	result := Detect(&node, dir, 1)
	assert.Equal(t, model.OutcomeFail, result.Outcome)
}

func TestDetect_CodeType_CommitsExistIsPass(t *testing.T) {
	// This test would need a real git repo — tested at integration level.
	// Unit test: verify that Detect with type=code and a startSHA delegates to git check.
	// For now, test the branch with a mock by testing DetectCodeOutcome directly.
}

func TestDetect_VerdictTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	// Both verdict.txt and output file exist — verdict wins.
	writeFile(t, dir, ".belayer/output/verdict.txt", "RETRY lead\nIssues found.")
	writeFile(t, dir, ".belayer/output/review.md", "PASS\nAll good.")
	node := pipeline.NodeConfig{
		Name:   "spotter",
		Output: pipeline.OutputConfig{Type: "file", Path: ".belayer/output/review.md"},
	}
	result := Detect(&node, dir, 1)
	assert.Equal(t, model.OutcomeRetry, result.Outcome)
	assert.Equal(t, "lead", result.TargetNode)
}

// Helpers

func fileNode(name string) *pipeline.NodeConfig {
	return &pipeline.NodeConfig{
		Name:   name,
		Output: pipeline.OutputConfig{Type: "file", Path: ".belayer/output/review.md"},
	}
}

func writeFile(t *testing.T, base, rel, content string) {
	t.Helper()
	path := filepath.Join(base, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/outcome/ -v`
Expected: Compilation failure.

- [ ] **Step 3: Implement outcome detection**

```go
// internal/v3/outcome/detect.go
package outcome

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// Detect determines the outcome of a node execution.
// Precedence: verdict.txt > output file first line > type-based default.
func Detect(node *pipeline.NodeConfig, workDir string, attempt int) model.CompletionResult {
	// 1. Check for explicit verdict file.
	verdictPath := filepath.Join(workDir, ".belayer", "output", "verdict.txt")
	if data, err := os.ReadFile(verdictPath); err == nil {
		return parseVerdict(data, node.Output.Path, attempt)
	}

	// 2. For file-type outputs, check if the file exists and parse first line.
	if node.Output.Type == "file" && node.Output.Path != "" {
		outputPath := filepath.Join(workDir, node.Output.Path)
		if data, err := os.ReadFile(outputPath); err == nil {
			// Check if first line is a verdict keyword.
			firstLine := firstLine(string(data))
			if outcome, target := parseFirstLine(firstLine); outcome.IsValid() {
				return model.CompletionResult{
					Outcome:    outcome,
					OutputPath: node.Output.Path,
					TargetNode: target,
					Attempt:    attempt,
				}
			}
			// File exists but no verdict keyword → PASS.
			return model.CompletionResult{
				Outcome:    model.OutcomePass,
				OutputPath: node.Output.Path,
				Attempt:    attempt,
			}
		}
		// File doesn't exist → FAIL.
		return model.CompletionResult{
			Outcome: model.OutcomeFail,
			Attempt: attempt,
		}
	}

	// 3. For code-type outputs, need git check (handled by caller with startSHA).
	// Default: PASS (caller should check commits separately for code type).
	return model.CompletionResult{
		Outcome: model.OutcomePass,
		Attempt: attempt,
	}
}

// DetectCodeOutcome checks if new commits exist since startSHA.
// Returns PASS if new commits exist, FAIL if HEAD unchanged.
func DetectCodeOutcome(workDir, startSHA string, attempt int) model.CompletionResult {
	// This will be implemented to run: git log <startSHA>..HEAD --oneline
	// For now, placeholder that the activity will call.
	return model.CompletionResult{
		Outcome: model.OutcomePass,
		Attempt: attempt,
	}
}

func parseVerdict(data []byte, outputPath string, attempt int) model.CompletionResult {
	line := firstLine(string(data))
	outcome, target := parseFirstLine(line)
	if !outcome.IsValid() {
		outcome = model.OutcomeFail
	}
	return model.CompletionResult{
		Outcome:    outcome,
		OutputPath: outputPath,
		TargetNode: target,
		Attempt:    attempt,
	}
}

func parseFirstLine(line string) (model.NodeOutcome, string) {
	line = strings.TrimSpace(line)
	upper := strings.ToUpper(line)

	if upper == "PASS" || strings.HasPrefix(upper, "PASS ") || strings.HasPrefix(upper, "PASS\t") {
		return model.OutcomePass, ""
	}
	if upper == "FAIL" || strings.HasPrefix(upper, "FAIL ") || strings.HasPrefix(upper, "FAIL\t") {
		return model.OutcomeFail, ""
	}
	if strings.HasPrefix(upper, "RETRY") {
		parts := strings.Fields(line)
		target := ""
		if len(parts) > 1 {
			target = parts[1]
		}
		return model.OutcomeRetry, target
	}
	return "", ""
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/outcome/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/outcome/
git commit -m "feat(v3): add outcome detection (verdict.txt > output file > type default)"
```

---

### Task 7: Hooks Config Generator

**Files:**
- Create: `internal/v3/session/hooks.go`
- Create: `internal/v3/session/hooks_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/v3/session/hooks_test.go
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteHooksConfig(t *testing.T) {
	dir := t.TempDir()
	err := WriteHooksConfig(dir, "wf-123", "setter", 1)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".belayer", "hooks.json"))
	require.NoError(t, err)

	var cfg map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &cfg))

	hooks, ok := cfg["hooks"].(map[string]interface{})
	require.True(t, ok)
	_, hasStop := hooks["Stop"]
	assert.True(t, hasStop, "hooks.json should have a Stop hook")
}

func TestWriteHooksConfig_CommandContainsNodeComplete(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteHooksConfig(dir, "wf-123", "setter", 1))

	data, err := os.ReadFile(filepath.Join(dir, ".belayer", "hooks.json"))
	require.NoError(t, err)

	assert.Contains(t, string(data), "node-complete")
	assert.Contains(t, string(data), "wf-123")
	assert.Contains(t, string(data), "setter")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/session/ -v`
Expected: Compilation failure.

- [ ] **Step 3: Implement hooks config generator**

```go
// internal/v3/session/hooks.go
package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteHooksConfig writes a .belayer/hooks.json file that configures the Stop hook
// to call `belayer node-complete` with the task ID, node name, and attempt number.
// The resulting file is passed to Claude via --settings.
func WriteHooksConfig(workDir, taskID, nodeName string, attempt int) error {
	hooksDir := filepath.Join(workDir, ".belayer")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create .belayer dir: %w", err)
	}

	hooksJSON := fmt.Sprintf(`{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "belayer node-complete --task-id %s --node %s --attempt %d"
          }
        ]
      }
    ]
  }
}`, taskID, nodeName, attempt)

	path := filepath.Join(hooksDir, "hooks.json")
	return os.WriteFile(path, []byte(hooksJSON), 0o644)
}

// HooksConfigPath returns the path to the hooks config file.
func HooksConfigPath(workDir string) string {
	return filepath.Join(workDir, ".belayer", "hooks.json")
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/session/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/session/
git commit -m "feat(v3): add hooks config generator for Stop hook → node-complete"
```

---

### Task 8: `belayer node-complete` Command

**Files:**
- Create: `internal/v3/cli/node_complete.go`
- Create: `internal/v3/cli/node_complete_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/v3/cli/node_complete_test.go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donovan-yohan/belayer/internal/v3/model"
)

func TestWriteCompletionFile(t *testing.T) {
	dir := t.TempDir()
	result := model.CompletionResult{
		Outcome:    model.OutcomePass,
		OutputPath: "plan.md",
		Attempt:    1,
	}
	path := completionFilePath(dir, "wf-1", "setter", 1)
	require.NoError(t, writeCompletionFile(dir, "wf-1", "setter", result))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var decoded model.CompletionResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, model.OutcomePass, decoded.Outcome)
}

func TestCompletionFilePath_AttemptScoped(t *testing.T) {
	path1 := completionFilePath("/work", "wf-1", "setter", 1)
	path2 := completionFilePath("/work", "wf-1", "setter", 2)
	assert.NotEqual(t, path1, path2)
	assert.Contains(t, path1, "attempt-1")
	assert.Contains(t, path2, "attempt-2")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/cli/ -v`
Expected: Compilation failure.

- [ ] **Step 3: Implement node-complete command**

```go
// internal/v3/cli/node_complete.go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/outcome"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func newNodeCompleteCmd() *cobra.Command {
	var taskID, nodeName string
	var attempt int

	cmd := &cobra.Command{
		Use:   "node-complete",
		Short: "Signal node completion (called by Stop hook)",
		Long:  "Called by the Claude Code Stop hook to determine outcome and write completion file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fall back to env vars if flags not provided.
			if taskID == "" {
				taskID = os.Getenv("BELAYER_TASK_ID")
			}
			if nodeName == "" {
				nodeName = os.Getenv("BELAYER_NODE")
			}
			if attempt == 0 {
				if a := os.Getenv("BELAYER_ATTEMPT"); a != "" {
					attempt, _ = strconv.Atoi(a)
				}
				if attempt == 0 {
					attempt = 1
				}
			}

			if taskID == "" || nodeName == "" {
				return fmt.Errorf("--task-id and --node are required (or set BELAYER_TASK_ID and BELAYER_NODE)")
			}

			workDir, _ := os.Getwd()

			// Load pipeline config to get node definition.
			node := resolveNode(workDir, nodeName)

			// Detect outcome.
			result := outcome.Detect(node, workDir, attempt)
			fmt.Printf("node-complete: %s → %s\n", nodeName, result.Outcome)

			// Write completion file.
			return writeCompletionFile(workDir, taskID, nodeName, result)
		},
	}

	cmd.Flags().StringVar(&taskID, "task-id", "", "Workflow task ID")
	cmd.Flags().StringVar(&nodeName, "node", "", "Node name")
	cmd.Flags().IntVar(&attempt, "attempt", 0, "Attempt number (default from env)")

	return cmd
}

// resolveNode finds the node config, falling back to a minimal default.
func resolveNode(workDir, nodeName string) *pipeline.NodeConfig {
	// Try to load from pipeline file.
	for _, path := range []string{
		filepath.Join(workDir, "belayer-pipeline.yaml"),
		filepath.Join(workDir, ".belayer", "pipeline.yaml"),
	} {
		if cfg, err := pipeline.ParsePipelineFile(path); err == nil {
			if n := cfg.FindNode(nodeName); n != nil {
				return n
			}
		}
	}
	// Fallback: try the default pipeline.
	if cfg, err := pipeline.ParsePipeline([]byte(pipeline.DefaultPipelineYAML)); err == nil {
		if n := cfg.FindNode(nodeName); n != nil {
			return n
		}
	}
	// Last resort: generic file node.
	return &pipeline.NodeConfig{
		Name:   nodeName,
		Output: pipeline.OutputConfig{Type: "file"},
	}
}

// completionFilePath returns the path to the attempt-scoped completion file.
func completionFilePath(workDir, taskID, nodeName string, attempt int) string {
	return filepath.Join(workDir, ".belayer", "completion",
		fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt))
}

// writeCompletionFile writes the CompletionResult JSON to the completion file.
func writeCompletionFile(workDir, taskID, nodeName string, result model.CompletionResult) error {
	path := completionFilePath(workDir, taskID, nodeName, result.Attempt)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create completion dir: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal completion: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/cli/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/cli/node_complete.go internal/v3/cli/node_complete_test.go
git commit -m "feat(v3): add belayer node-complete command (Stop hook handler)"
```

---

### Task 9: Session Spawner

**Files:**
- Create: `internal/v3/session/spawner.go`
- Create: `internal/v3/session/spawner_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/v3/session/spawner_test.go
package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildClaudeCommand(t *testing.T) {
	opts := SpawnOpts{
		NodeName:    "setter",
		TaskID:      "wf-123",
		Attempt:     1,
		WorkDir:     "/tmp/work",
		Description: "Create a plan from the design doc.",
		HooksPath:   "/tmp/work/.belayer/hooks.json",
		InputPrompt: "Read .belayer/input/design.md and create a plan.",
	}
	cmd := buildClaudeCommand(opts)
	assert.Contains(t, cmd, "claude")
	assert.Contains(t, cmd, "--dangerously-skip-permissions")
	assert.Contains(t, cmd, "--append-system-prompt")
	assert.Contains(t, cmd, "--settings")
	assert.Contains(t, cmd, opts.HooksPath)
	assert.Contains(t, cmd, "Create a plan from the design doc.")
}

func TestBuildEnvExports(t *testing.T) {
	opts := SpawnOpts{
		NodeName: "lead",
		TaskID:   "wf-456",
		Attempt:  2,
		WorkDir:  "/tmp/work",
	}
	exports := buildEnvExports(opts)
	assert.Contains(t, exports, "BELAYER_TASK_ID=wf-456")
	assert.Contains(t, exports, "BELAYER_NODE=lead")
	assert.Contains(t, exports, "BELAYER_ATTEMPT=2")
}

func TestSpawnOpts_WindowName(t *testing.T) {
	opts := SpawnOpts{NodeName: "setter", TaskID: "wf-12345678-long-id"}
	assert.Equal(t, "setter-wf-12345", opts.WindowName())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/session/ -run TestBuild -v`
Expected: Compilation failure.

- [ ] **Step 3: Implement session spawner**

```go
// internal/v3/session/spawner.go
package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

const tmuxSessionName = "belayer-v3"

// SpawnOpts contains everything needed to spawn a node session.
type SpawnOpts struct {
	NodeName    string
	TaskID      string
	Attempt     int
	WorkDir     string
	Description string // Node description (natural language role prompt)
	HooksPath   string // Path to hooks.json for --settings
	InputPrompt string // Initial prompt sent to Claude
}

// WindowName returns a short tmux window name for this session.
func (o SpawnOpts) WindowName() string {
	taskShort := o.TaskID
	if len(taskShort) > 8 {
		taskShort = taskShort[:8]
	}
	return fmt.Sprintf("%s-%s", o.NodeName, taskShort)
}

// Spawner launches interactive Claude sessions for pipeline nodes.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) error
}

// TmuxSpawner spawns sessions in tmux windows (provides PTY).
type TmuxSpawner struct {
	tmux tmux.TmuxManager
}

// NewTmuxSpawner creates a spawner backed by the given TmuxManager.
func NewTmuxSpawner(tm tmux.TmuxManager) *TmuxSpawner {
	return &TmuxSpawner{tmux: tm}
}

// Spawn launches a Claude Code session in a tmux window.
func (s *TmuxSpawner) Spawn(ctx context.Context, opts SpawnOpts) error {
	// Ensure tmux session exists.
	if !s.tmux.HasSession(tmuxSessionName) {
		if err := s.tmux.NewSession(tmuxSessionName); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
	}

	windowName := opts.WindowName()
	if err := s.tmux.NewWindow(tmuxSessionName, windowName); err != nil {
		return fmt.Errorf("create tmux window: %w", err)
	}

	cmd := fmt.Sprintf("%scd %s && %s",
		buildEnvExports(opts),
		shellQuote(opts.WorkDir),
		buildClaudeCommand(opts))

	return s.tmux.SendKeys(tmuxSessionName, windowName, cmd)
}

// buildClaudeCommand constructs the claude CLI invocation string.
func buildClaudeCommand(opts SpawnOpts) string {
	parts := []string{
		"claude",
		"--dangerously-skip-permissions",
	}

	if opts.Description != "" {
		parts = append(parts, "--append-system-prompt", shellQuote(opts.Description))
	}
	if opts.HooksPath != "" {
		parts = append(parts, "--settings", shellQuote(opts.HooksPath))
	}

	// Initial prompt as positional argument.
	if opts.InputPrompt != "" {
		parts = append(parts, shellQuote(opts.InputPrompt))
	}

	return strings.Join(parts, " ")
}

// buildEnvExports builds the env var export string for the session.
func buildEnvExports(opts SpawnOpts) string {
	exports := fmt.Sprintf("export BELAYER_TASK_ID=%s && export BELAYER_NODE=%s && export BELAYER_ATTEMPT=%d && ",
		shellQuote(opts.TaskID),
		shellQuote(opts.NodeName),
		opts.Attempt)
	return exports
}

func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/session/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/session/spawner.go internal/v3/session/spawner_test.go
git commit -m "feat(v3): add session spawner (tmux-backed Claude session launcher)"
```

---

### Task 10: Node Activity

**Files:**
- Create: `internal/v3/temporal/constants.go`
- Create: `internal/v3/temporal/activity.go`
- Create: `internal/v3/temporal/activity_test.go`

- [ ] **Step 1: Write constants**

```go
// internal/v3/temporal/constants.go
package temporal

// TaskQueueName is the Temporal task queue for v3 climb workers.
const TaskQueueName = "belayer-climb"
```

- [ ] **Step 2: Write activity tests**

```go
// internal/v3/temporal/activity_test.go
package temporal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func TestNodeActivity_DetectsCompletionFile(t *testing.T) {
	dir := t.TempDir()

	// Create the completion file that the Stop hook would write.
	completionDir := filepath.Join(dir, ".belayer", "completion")
	require.NoError(t, os.MkdirAll(completionDir, 0o755))

	result := model.CompletionResult{
		Outcome:    model.OutcomePass,
		OutputPath: ".belayer/output/plan.md",
		Attempt:    1,
	}
	data, _ := json.Marshal(result)
	completionPath := filepath.Join(completionDir, "wf-1-setter-attempt-1.json")
	require.NoError(t, os.WriteFile(completionPath, data, 0o644))

	// Test reading the completion file.
	got, err := readCompletionFile(dir, "wf-1", "setter", 1)
	require.NoError(t, err)
	assert.Equal(t, model.OutcomePass, got.Outcome)
}

func TestNodeActivity_CompletionFileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := readCompletionFile(dir, "wf-1", "setter", 1)
	assert.Error(t, err)
}

func TestNodeActivity_CleansStaleCompletionFiles(t *testing.T) {
	dir := t.TempDir()
	completionDir := filepath.Join(dir, ".belayer", "completion")
	require.NoError(t, os.MkdirAll(completionDir, 0o755))

	// Write a stale file from attempt 1.
	staleFile := filepath.Join(completionDir, "wf-1-setter-attempt-1.json")
	require.NoError(t, os.WriteFile(staleFile, []byte("{}"), 0o644))

	// Clean up stale files for attempt 2.
	cleanStaleCompletionFiles(dir, "wf-1", "setter", 2)

	// Stale file should be removed.
	_, err := os.Stat(staleFile)
	assert.True(t, os.IsNotExist(err))
}

func TestPrepareNodeInputs_DesignDocInput(t *testing.T) {
	dir := t.TempDir()
	artifacts := map[string]string{"design_doc": "/path/to/design.md"}

	node := pipeline.NodeConfig{
		Name:  "setter",
		Input: pipeline.InputConfig{Type: "file", Key: "design_doc"},
	}

	prompt := buildInputPrompt(&node, artifacts, dir)
	assert.Contains(t, prompt, "design.md")
}

func TestPrepareNodeInputs_CodeInput(t *testing.T) {
	dir := t.TempDir()
	artifacts := map[string]string{}

	node := pipeline.NodeConfig{
		Name:  "spotter",
		Input: pipeline.InputConfig{Type: "code"},
	}

	prompt := buildInputPrompt(&node, artifacts, dir)
	assert.Contains(t, prompt, "diff")
}

func TestPollForCompletion_Timeout(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pollForCompletion(ctx, dir, "wf-1", "setter", 1, 50*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/temporal/ -v`
Expected: Compilation failure.

- [ ] **Step 4: Implement node activity**

```go
// internal/v3/temporal/activity.go
package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
)

// NodeActivityInput is the input for a single node activity execution.
type NodeActivityInput struct {
	Node      pipeline.NodeConfig `json:"node"`
	TaskID    string              `json:"task_id"`
	WorkDir   string              `json:"work_dir"`
	Attempt   int                 `json:"attempt"`
	Artifacts map[string]string   `json:"artifacts"` // key → path
	StartSHA  string              `json:"start_sha,omitempty"` // For code-type: HEAD SHA at start
}

// NodeActivityOutput is the result of a node activity.
type NodeActivityOutput struct {
	Result model.CompletionResult `json:"result"`
}

// Activities holds the v3 activity implementations.
type Activities struct {
	Spawner session.Spawner
}

// NodeActivity spawns a Claude session for a pipeline node,
// heartbeats while it runs, and polls for the completion file.
func (a *Activities) NodeActivity(ctx context.Context, input NodeActivityInput) (*NodeActivityOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Node activity started", "node", input.Node.Name, "attempt", input.Attempt)

	// 1. Clean stale completion files from previous attempts.
	cleanStaleCompletionFiles(input.WorkDir, input.TaskID, input.Node.Name, input.Attempt)

	// 2. Write hooks config.
	if err := session.WriteHooksConfig(input.WorkDir, input.TaskID, input.Node.Name, input.Attempt); err != nil {
		return nil, fmt.Errorf("write hooks config: %w", err)
	}

	// 3. Materialize input artifacts.
	inputPrompt := buildInputPrompt(&input.Node, input.Artifacts, input.WorkDir)

	// 4. For code-type inputs, write the diff files.
	if input.Node.Input.Type == "code" {
		materializeCodeInput(input.WorkDir)
	}

	// 5. Spawn the session.
	spawnOpts := session.SpawnOpts{
		NodeName:    input.Node.Name,
		TaskID:      input.TaskID,
		Attempt:     input.Attempt,
		WorkDir:     input.WorkDir,
		Description: input.Node.Description,
		HooksPath:   session.HooksConfigPath(input.WorkDir),
		InputPrompt: inputPrompt,
	}

	if err := a.Spawner.Spawn(ctx, spawnOpts); err != nil {
		return nil, fmt.Errorf("spawn session: %w", err)
	}

	// 6. Poll for completion file with heartbeats.
	result, err := pollForCompletion(ctx, input.WorkDir, input.TaskID, input.Node.Name, input.Attempt, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("wait for completion: %w", err)
	}

	// 7. For code-type outputs, check for new commits if no explicit verdict.
	if input.Node.Output.Type == "code" && result.Outcome == model.OutcomePass && input.StartSHA != "" {
		// Verify commits actually exist.
		if !hasNewCommits(input.WorkDir, input.StartSHA) {
			result.Outcome = model.OutcomeFail
		}
	}

	logger.Info("Node activity completed", "node", input.Node.Name, "outcome", result.Outcome)
	return &NodeActivityOutput{Result: result}, nil
}

// pollForCompletion polls for the completion file at regular intervals,
// sending Temporal heartbeats while waiting.
func pollForCompletion(ctx context.Context, workDir, taskID, nodeName string, attempt int, pollInterval time.Duration) (model.CompletionResult, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return model.CompletionResult{}, ctx.Err()
		case <-ticker.C:
			// Heartbeat to keep the activity alive.
			activity.RecordHeartbeat(ctx, fmt.Sprintf("polling for %s completion", nodeName))

			result, err := readCompletionFile(workDir, taskID, nodeName, attempt)
			if err == nil {
				return result, nil
			}
			// File not found yet — keep polling.
		}
	}
}

// readCompletionFile reads and parses the attempt-scoped completion file.
func readCompletionFile(workDir, taskID, nodeName string, attempt int) (model.CompletionResult, error) {
	path := filepath.Join(workDir, ".belayer", "completion",
		fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt))

	data, err := os.ReadFile(path)
	if err != nil {
		return model.CompletionResult{}, err
	}

	var result model.CompletionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return model.CompletionResult{}, fmt.Errorf("parse completion file: %w", err)
	}
	return result, nil
}

// cleanStaleCompletionFiles removes completion files from previous attempts.
func cleanStaleCompletionFiles(workDir, taskID, nodeName string, currentAttempt int) {
	completionDir := filepath.Join(workDir, ".belayer", "completion")
	for i := 1; i < currentAttempt; i++ {
		path := filepath.Join(completionDir,
			fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, i))
		os.Remove(path) // Best effort.
	}
}

// buildInputPrompt constructs the initial prompt for a node session.
// Appends feedback reference if a previous attempt was reviewed (design doc Q2).
func buildInputPrompt(node *pipeline.NodeConfig, artifacts map[string]string, workDir string) string {
	var prompt string
	switch node.Input.Type {
	case "file":
		if path, ok := artifacts[node.Input.Key]; ok {
			prompt = fmt.Sprintf("Your input artifact is at: %s\nRead it and begin your work.", path)
		} else {
			prompt = "Begin your work. Check .belayer/input/ for any input artifacts."
		}
	case "code":
		prompt = "Review the changes. Full diff at .belayer/input/diff.txt, summary at .belayer/input/diff-summary.txt."
	default:
		prompt = "Begin your work."
	}

	// Append feedback if this is a retry (design doc Q2).
	if feedbackPath, ok := artifacts["feedback"]; ok {
		prompt += fmt.Sprintf("\n\nPrevious attempt was reviewed. Feedback at %s — address all issues listed.", feedbackPath)
	}
	return prompt
}

// materializeCodeInput writes git diff files for code-type inputs (design doc Q3).
// Diffs against the merge-base with the default branch to capture all changes on this climb branch.
func materializeCodeInput(workDir string) {
	inputDir := filepath.Join(workDir, ".belayer", "input")
	os.MkdirAll(inputDir, 0o755)

	// Find the merge-base with the default branch (main or master).
	defaultBranch := detectDefaultBranch(workDir)

	// Write full diff: git diff <merge-base>..HEAD
	diffCmd := exec.Command("git", "-C", workDir, "diff", defaultBranch+"..HEAD")
	if out, err := diffCmd.Output(); err == nil {
		os.WriteFile(filepath.Join(inputDir, "diff.txt"), out, 0o644)
	}

	// Write diff summary.
	statCmd := exec.Command("git", "-C", workDir, "diff", "--stat", defaultBranch+"..HEAD")
	if out, err := statCmd.Output(); err == nil {
		os.WriteFile(filepath.Join(inputDir, "diff-summary.txt"), out, 0o644)
	}
}

// detectDefaultBranch returns the default branch name (main or master).
func detectDefaultBranch(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if out, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		if parts := strings.Split(ref, "/"); len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	// Fallback: check for main, then master.
	for _, name := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", workDir, "rev-parse", "--verify", name)
		if cmd.Run() == nil {
			return name
		}
	}
	return "main"
}

// hasNewCommits checks if HEAD has advanced past startSHA.
func hasNewCommits(workDir, startSHA string) bool {
	cmd := exec.Command("git", "-C", workDir, "log", "--oneline", startSHA+"..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// getHeadSHA returns the current HEAD SHA in the working directory.
func GetHeadSHA(workDir string) (string, error) {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get HEAD SHA: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/temporal/ -v`
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/v3/temporal/
git commit -m "feat(v3): add NodeActivity — spawn session, heartbeat, poll completion"
```

---

### Task 11: Climb Workflow

**Files:**
- Create: `internal/v3/temporal/workflow.go`
- Create: `internal/v3/temporal/workflow_test.go`

- [ ] **Step 1: Write workflow tests**

```go
// internal/v3/temporal/workflow_test.go
package temporal

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// Ensure pipeline import is used.
var _ = pipeline.DefaultPipelineYAML

type ClimbWorkflowSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *ClimbWorkflowSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.env.RegisterActivity(&Activities{})
}

func (s *ClimbWorkflowSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

func TestClimbWorkflowSuite(t *testing.T) {
	suite.Run(t, new(ClimbWorkflowSuite))
}

func (s *ClimbWorkflowSuite) TestClimb_AllNodesPass() {
	var a *Activities

	// Setter passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "setter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, OutputPath: "plan.md", Attempt: 1,
	}}, nil)

	// Lead passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "lead"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, Attempt: 1,
	}}, nil)

	// Spotter passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "spotter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, OutputPath: "review.md", Attempt: 1,
	}}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, model.ClimbInput{
		Description:  "test climb",
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      "/tmp/work",
		Branch:       "belayer/climb-test",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

func (s *ClimbWorkflowSuite) TestClimb_SpotterRetriesToLead() {
	var a *Activities

	// Setter passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "setter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, OutputPath: "plan.md", Attempt: 1,
	}}, nil)

	// Lead: called twice (initial + retry after spotter).
	leadCall := 0
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "lead"
	})).Return(func(ctx interface{}, input NodeActivityInput) (*NodeActivityOutput, error) {
		leadCall++
		return &NodeActivityOutput{Result: model.CompletionResult{
			Outcome: model.OutcomePass, Attempt: leadCall,
		}}, nil
	})

	// Spotter: first call RETRY, second call PASS.
	spotterCall := 0
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "spotter"
	})).Return(func(ctx interface{}, input NodeActivityInput) (*NodeActivityOutput, error) {
		spotterCall++
		if spotterCall == 1 {
			return &NodeActivityOutput{Result: model.CompletionResult{
				Outcome:    model.OutcomeRetry,
				TargetNode: "lead",
				Feedback:   "Fix error handling",
				Attempt:    1,
			}}, nil
		}
		return &NodeActivityOutput{Result: model.CompletionResult{
			Outcome: model.OutcomePass, Attempt: 2,
		}}, nil
	})

	s.env.ExecuteWorkflow(ClimbWorkflow, model.ClimbInput{
		Description:  "test retry",
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      "/tmp/work",
		Branch:       "belayer/climb-test",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

func (s *ClimbWorkflowSuite) TestClimb_NodeFails() {
	var a *Activities

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "setter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomeFail, Attempt: 1,
	}}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, model.ClimbInput{
		Description:  "test fail",
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      "/tmp/work",
		Branch:       "belayer/climb-test",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
	s.Contains(result.Message, "setter")
}

func (s *ClimbWorkflowSuite) TestClimb_MaxRetriesExhausted() {
	var a *Activities

	// Setter passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "setter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, Attempt: 1,
	}}, nil)

	// Lead always passes.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "lead"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome: model.OutcomePass, Attempt: 1,
	}}, nil)

	// Spotter always retries (should exhaust max_retries=2).
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(input NodeActivityInput) bool {
		return input.Node.Name == "spotter"
	})).Return(&NodeActivityOutput{Result: model.CompletionResult{
		Outcome:    model.OutcomeRetry,
		TargetNode: "lead",
		Feedback:   "Still broken",
		Attempt:    1,
	}}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, model.ClimbInput{
		Description:  "test exhaustion",
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      "/tmp/work",
		Branch:       "belayer/climb-test",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
	s.Contains(result.Message, "max retries")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/temporal/ -run TestClimb -v`
Expected: Compilation failure — ClimbWorkflow not defined.

- [ ] **Step 3: Implement ClimbWorkflow**

```go
// internal/v3/temporal/workflow.go
package temporal

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// ClimbWorkflow is the main v3 pipeline workflow.
// It sequences pipeline nodes, handles PASS/RETRY/FAIL routing,
// and maintains an artifact map for output-to-input piping.
func ClimbWorkflow(ctx workflow.Context, input model.ClimbInput) (*model.ClimbOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Climb workflow started", "description", input.Description)

	// Parse pipeline config.
	cfg, err := pipeline.ParsePipeline(input.PipelineYAML)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	artifacts := make(map[string]string)
	nodeOutputs := make(map[string]string)

	// Seed initial artifact from design file.
	if input.DesignFile != "" {
		artifacts["design_doc"] = input.DesignFile
	}
	if input.InputPath != "" && input.FromNode != "" {
		// Resume mode: provide input for the starting node.
		artifacts[input.FromNode] = input.InputPath
	}

	retryCount := make(map[string]int) // node name → retry count

	// Find starting node index.
	startIdx := 0
	if input.FromNode != "" {
		for i, n := range cfg.Nodes {
			if n.Name == input.FromNode {
				startIdx = i
				break
			}
		}
	}

	nodeIdx := startIdx
	for nodeIdx < len(cfg.Nodes) {
		node := cfg.Nodes[nodeIdx]
		attempt := retryCount[node.Name] + 1

		logger.Info("Executing node", "node", node.Name, "attempt", attempt)

		// Execute the node activity.
		activityOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 2 * time.Hour,
			HeartbeatTimeout:    60 * time.Second,
		}
		actCtx := workflow.WithActivityOptions(ctx, activityOpts)

		actInput := NodeActivityInput{
			Node:      node,
			TaskID:    workflow.GetInfo(ctx).WorkflowExecution.ID,
			WorkDir:   input.WorkDir,
			Attempt:   attempt,
			Artifacts: artifacts,
		}

		var actOutput NodeActivityOutput
		var a *Activities
		if err := workflow.ExecuteActivity(actCtx, a.NodeActivity, actInput).Get(actCtx, &actOutput); err != nil {
			return &model.ClimbOutput{
				Status:      model.ClimbFailed,
				NodeOutputs: nodeOutputs,
				Message:     fmt.Sprintf("node %q activity error: %v", node.Name, err),
				Branch:      input.Branch,
			}, nil
		}

		result := actOutput.Result

		switch result.Outcome {
		case model.OutcomePass:
			// Store output artifact.
			outputKey := node.OutputKey()
			if result.OutputPath != "" {
				artifacts[outputKey] = result.OutputPath
			}
			nodeOutputs[node.Name] = result.OutputPath

			// Advance to next node.
			if node.OnPass == "stop" {
				logger.Info("Node passed with on_pass=stop", "node", node.Name)
				return &model.ClimbOutput{
					Status:      model.ClimbCompleted,
					NodeOutputs: nodeOutputs,
					Branch:      input.Branch,
				}, nil
			}
			nodeIdx++

		case model.OutcomeRetry:
			retryCount[node.Name]++
			if retryCount[node.Name] > node.MaxRetries {
				logger.Warn("Max retries exhausted", "node", node.Name, "retries", retryCount[node.Name])
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q: max retries (%d) exhausted", node.Name, node.MaxRetries),
					Branch:      input.Branch,
				}, nil
			}

			// Determine target node for retry.
			// Verdict file target takes precedence (design doc Q14),
			// then node config on_retry, then self.
			target := result.TargetNode
			if target == "" || target == "self" {
				if node.OnRetry != "" && node.OnRetry != "self" && node.OnRetry != "stop" {
					target = node.OnRetry
				} else {
					target = node.Name
				}
			}

			if target == "stop" || node.OnRetry == "stop" {
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q: retry with on_retry=stop", node.Name),
					Branch:      input.Branch,
				}, nil
			}

			// Write feedback file to disk so the target session can read it (design doc Q2).
			if result.Feedback != "" {
				feedbackPath := filepath.Join(input.WorkDir, ".belayer", "input", "feedback.md")
				os.MkdirAll(filepath.Dir(feedbackPath), 0o755)
				os.WriteFile(feedbackPath, []byte(result.Feedback), 0o644)
				artifacts["feedback"] = ".belayer/input/feedback.md"
			}

			// Find the target node index and jump back.
			found := false
			for i, n := range cfg.Nodes {
				if n.Name == target {
					nodeIdx = i
					found = true
					logger.Info("Retry loop", "from", node.Name, "to", target, "attempt", retryCount[node.Name])
					break
				}
			}
			if !found {
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q: retry target %q not found", node.Name, target),
					Branch:      input.Branch,
				}, nil
			}

		case model.OutcomeFail:
			logger.Warn("Node failed", "node", node.Name)
			if node.OnFail != "stop" && node.OnFail != "" {
				// on_fail references another node — jump to it.
				found := false
				for i, n := range cfg.Nodes {
					if n.Name == node.OnFail {
						nodeIdx = i
						found = true
						break
					}
				}
				if !found {
					return &model.ClimbOutput{
						Status:      model.ClimbFailed,
						NodeOutputs: nodeOutputs,
						Message:     fmt.Sprintf("node %q failed, on_fail target %q not found", node.Name, node.OnFail),
						Branch:      input.Branch,
					}, nil
				}
			} else {
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q failed", node.Name),
					Branch:      input.Branch,
				}, nil
			}

		default:
			return &model.ClimbOutput{
				Status:      model.ClimbFailed,
				NodeOutputs: nodeOutputs,
				Message:     fmt.Sprintf("node %q: unknown outcome %q", node.Name, result.Outcome),
				Branch:      input.Branch,
			}, nil
		}
	}

	logger.Info("Climb workflow completed")
	return &model.ClimbOutput{
		Status:      model.ClimbCompleted,
		NodeOutputs: nodeOutputs,
		Branch:      input.Branch,
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/temporal/ -v`
Expected: All workflow tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/temporal/workflow.go internal/v3/temporal/workflow_test.go
git commit -m "feat(v3): add ClimbWorkflow — node sequencing with PASS/RETRY/FAIL routing"
```

---

### Task 12: `belayer climb` CLI Command

**Files:**
- Create: `internal/v3/cli/climb.go`

- [ ] **Step 1: Implement the climb command**

```go
// internal/v3/cli/climb.go
package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/v3/events"
	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
	beltemporal "github.com/donovan-yohan/belayer/internal/v3/temporal"
)

func newClimbCmd() *cobra.Command {
	var fileFlag, promptFlag, nodeFlag, inputFlag string
	var detach bool

	cmd := &cobra.Command{
		Use:   "climb",
		Short: "Run a pipeline from a design doc",
		Long: `Start a Temporal workflow that sequences pipeline nodes (setter → lead → spotter).

Each node spawns an interactive Claude Code session with a defined role.
The Stop hook signals completion via belayer node-complete.

Examples:
  belayer climb --file design.md
  belayer climb --prompt "add auth to the API"
  belayer climb --file design.md --node lead --input plan.md
  belayer climb --detach --file design.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve input.
			designFile, description, err := resolveClimbInput(fileFlag, promptFlag, args)
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()

			// Parse pipeline.
			pipelineYAML, pipelineName, err := resolvePipelineYAML(cwd)
			if err != nil {
				return err
			}
			fmt.Printf("Pipeline: %s\n", pipelineName)

			// Connect to Temporal.
			c, err := client.Dial(client.Options{})
			if err != nil {
				return fmt.Errorf("cannot connect to Temporal. Ensure temporal server is running.\n\nError: %w", err)
			}
			defer c.Close()

			// Start in-process worker.
			tm := tmux.NewRealTmux()
			spawner := session.NewTmuxSpawner(tm)
			activities := &beltemporal.Activities{Spawner: spawner}

			w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})
			w.RegisterWorkflow(beltemporal.ClimbWorkflow)
			w.RegisterActivity(activities)

			if err := w.Start(); err != nil {
				return fmt.Errorf("start worker: %w", err)
			}
			defer w.Stop()

			// Create git branch for this climb (design doc Q15).
			workflowID := fmt.Sprintf("belayer-climb-%d", time.Now().UnixMilli())
			branch := fmt.Sprintf("belayer/climb-%s", workflowID)

			branchCmd := exec.Command("git", "-C", cwd, "checkout", "-b", branch)
			if out, err := branchCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("create branch %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
			}
			fmt.Printf("Branch: %s\n", branch)

			input := model.ClimbInput{
				Description:  description,
				DesignFile:   designFile,
				PipelineYAML: pipelineYAML,
				FromNode:     nodeFlag,
				InputPath:    inputFlag,
				WorkDir:      cwd,
				Branch:       branch,
			}

			opts := client.StartWorkflowOptions{
				ID:        workflowID,
				TaskQueue: beltemporal.TaskQueueName,
			}

			run, err := c.ExecuteWorkflow(cmd.Context(), opts, beltemporal.ClimbWorkflow, input)
			if err != nil {
				return fmt.Errorf("start pipeline: %w", err)
			}

			fmt.Printf("Climb started!\n")
			fmt.Printf("  Workflow: %s\n", run.GetID())
			fmt.Printf("  Branch:   %s\n", branch)

			if detach {
				fmt.Printf("\nDetached. Use 'belayer status' to check progress.\n")
				return nil
			}

			// Block and wait for completion.
			fmt.Printf("\nWaiting for pipeline to complete...\n")
			var result model.ClimbOutput
			if err := run.Get(cmd.Context(), &result); err != nil {
				return fmt.Errorf("pipeline error: %w", err)
			}

			// Print result.
			switch result.Status {
			case model.ClimbCompleted:
				fmt.Printf("\nClimb completed! Branch: %s\n", result.Branch)
			case model.ClimbFailed:
				fmt.Printf("\nClimb FAILED at: %s\n", result.Message)
				fmt.Printf("Branch preserved: %s\n", result.Branch)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&fileFlag, "file", "", "Design doc file path")
	cmd.Flags().StringVar(&promptFlag, "prompt", "", "Text prompt (written to temp file)")
	cmd.Flags().StringVar(&nodeFlag, "node", "", "Resume from this node")
	cmd.Flags().StringVar(&inputFlag, "input", "", "Input artifact path for --node")
	cmd.Flags().BoolVar(&detach, "detach", false, "Non-blocking mode (print workflow ID, exit)")

	return cmd
}

// resolveClimbInput determines the design file and description from CLI flags.
func resolveClimbInput(fileFlag, promptFlag string, args []string) (designFile, description string, err error) {
	if fileFlag != "" {
		data, err := os.ReadFile(fileFlag)
		if err != nil {
			return "", "", fmt.Errorf("read design file: %w", err)
		}
		return fileFlag, string(data[:min(len(data), 500)]), nil
	}
	if promptFlag != "" {
		return "", promptFlag, nil
	}
	if len(args) > 0 {
		return "", strings.Join(args, " "), nil
	}
	// Try stdin.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("read stdin: %w", err)
		}
		return "", string(data), nil
	}
	return "", "", fmt.Errorf("provide input via --file, --prompt, or stdin")
}

// resolvePipelineYAML finds and reads the pipeline YAML config.
func resolvePipelineYAML(cwd string) ([]byte, string, error) {
	// Check for pipeline file in CWD.
	for _, name := range []string{"belayer-pipeline.yaml", ".belayer/pipeline.yaml"} {
		path := name
		if data, err := os.ReadFile(path); err == nil {
			cfg, err := pipeline.ParsePipeline(data)
			if err != nil {
				return nil, "", fmt.Errorf("parse %s: %w", name, err)
			}
			return data, cfg.Name, nil
		}
	}
	// Use default.
	return []byte(pipeline.DefaultPipelineYAML), "default-climb", nil
}

			// Initialize event logger for this run.
			eventsDir := filepath.Join(cwd, ".belayer", "runs", workflowID)
			eventLogger, err := events.NewLogger(filepath.Join(eventsDir, "events.jsonl"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not create event log: %v\n", err)
			} else {
				defer eventLogger.Close()
				eventLogger.Log(events.PipelineStarted(workflowID, pipelineName, designFile))
			}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go build ./internal/v3/cli/`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/v3/cli/climb.go
git commit -m "feat(v3): add belayer climb command — pipeline entry point"
```

---

### Task 13: Status Command & CLI Wiring

**Files:**
- Create: `internal/v3/cli/status.go`
- Create: `internal/v3/cli/root.go`
- Modify: `internal/cli/root.go` (add v3 command registration)

- [ ] **Step 1: Implement status command**

```go
// internal/v3/cli/status.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active and recent pipeline runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.Dial(client.Options{})
			if err != nil {
				return fmt.Errorf("cannot connect to Temporal: %w", err)
			}
			defer c.Close()

			// List recent workflows via the correct Temporal API.
			resp, err := c.ListWorkflow(cmd.Context(), &workflowservice.ListWorkflowExecutionsRequest{
				Namespace: "default",
				Query:     `WorkflowType = "ClimbWorkflow"`,
			})
			if err != nil {
				return fmt.Errorf("list workflows: %w", err)
			}

			if len(resp.Executions) == 0 {
				fmt.Println("No pipeline runs found.")
				return nil
			}

			fmt.Println("Pipeline Runs:")
			fmt.Println("  ID                              Status     Start Time")
			fmt.Println("  ─────────────────────────────── ────────── ──────────")

			for _, wf := range resp.Executions {
				wfID := wf.GetExecution().GetWorkflowId()
				status := wf.GetStatus().String()
				startTime := wf.GetStartTime().AsTime().Format("15:04:05")
				_ = enums.WORKFLOW_EXECUTION_STATUS_RUNNING // ensure import used
				fmt.Printf("  %-35s %-10s %s\n", wfID, status, startTime)
			}

			return nil
		},
	}
}
```

- [ ] **Step 2: Implement CLI registration**

```go
// internal/v3/cli/root.go
package cli

import "github.com/spf13/cobra"

// RegisterV3Commands adds v3 commands to the root cobra command.
func RegisterV3Commands(root *cobra.Command) {
	root.AddCommand(
		newClimbCmd(),
		newNodeCompleteCmd(),
		newStatusCmd(),
	)
}
```

- [ ] **Step 3: Wire into CLI root**

Read `internal/cli/root.go` (where `v2cli.RegisterCommands` is called) and add `v3cli.RegisterV3Commands(cmd)` alongside the existing v2 registration. Import `v3cli "github.com/donovan-yohan/belayer/internal/v3/cli"`.

- [ ] **Step 4: Verify full build**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go build -o belayer ./cmd/belayer && ./belayer climb --help`
Expected: Build succeeds, `climb` help text is displayed.

- [ ] **Step 5: Run all tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/... -v`
Expected: All v3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/v3/cli/root.go internal/v3/cli/status.go internal/cli/root.go
git commit -m "feat(v3): add status command and wire v3 CLI into main binary"
```

---

### Task 14: Integration Test

**Files:**
- Create: `internal/v3/temporal/integration_test.go`

- [ ] **Step 1: Write integration test with mock spawner**

```go
// internal/v3/temporal/integration_test.go
package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
)

// fakeSpawner simulates a Claude session by immediately writing completion files.
type fakeSpawner struct {
	workDir string
	results map[string]model.CompletionResult
}

func (f *fakeSpawner) Spawn(ctx context.Context, opts session.SpawnOpts) error {
	// Simulate the session completing by writing the completion file.
	result, ok := f.results[opts.NodeName]
	if !ok {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}
	result.Attempt = opts.Attempt

	completionDir := filepath.Join(f.workDir, ".belayer", "completion")
	os.MkdirAll(completionDir, 0o755)

	path := filepath.Join(completionDir, fmt.Sprintf("%s-%s-attempt-%d.json",
		opts.TaskID, opts.NodeName, opts.Attempt))

	data, _ := json.Marshal(result)
	return os.WriteFile(path, data, 0o644)
}

type IntegrationSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *IntegrationSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *IntegrationSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (s *IntegrationSuite) TestEndToEnd_AllPass() {
	dir := s.T().TempDir()

	// Create a fake design doc.
	designPath := filepath.Join(dir, "design.md")
	require.NoError(s.T(), os.WriteFile(designPath, []byte("# Design\nBuild auth."), 0o644))

	// Create output files that the fake spawner's sessions would produce.
	outputDir := filepath.Join(dir, ".belayer", "output")
	require.NoError(s.T(), os.MkdirAll(outputDir, 0o755))
	require.NoError(s.T(), os.WriteFile(filepath.Join(outputDir, "plan.md"), []byte("# Plan\n1. Do stuff"), 0o644))
	require.NoError(s.T(), os.WriteFile(filepath.Join(outputDir, "review.md"), []byte("PASS\nLooks great."), 0o644))

	spawner := &fakeSpawner{
		workDir: dir,
		results: map[string]model.CompletionResult{
			"setter":  {Outcome: model.OutcomePass, OutputPath: ".belayer/output/plan.md"},
			"lead":    {Outcome: model.OutcomePass},
			"spotter": {Outcome: model.OutcomePass, OutputPath: ".belayer/output/review.md"},
		},
	}

	activities := &Activities{Spawner: spawner}
	s.env.RegisterActivity(activities)

	input := model.ClimbInput{
		Description:  "build auth",
		DesignFile:   designPath,
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      dir,
		Branch:       "belayer/climb-test",
	}

	s.env.ExecuteWorkflow(ClimbWorkflow, input)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

func (s *IntegrationSuite) TestEndToEnd_RetryLoop() {
	dir := s.T().TempDir()

	outputDir := filepath.Join(dir, ".belayer", "output")
	require.NoError(s.T(), os.MkdirAll(outputDir, 0o755))
	require.NoError(s.T(), os.WriteFile(filepath.Join(outputDir, "plan.md"), []byte("# Plan"), 0o644))

	spotterCallCount := 0
	spawner := &retryFakeSpawner{
		workDir:     dir,
		spotterCall: &spotterCallCount,
	}

	activities := &Activities{Spawner: spawner}
	s.env.RegisterActivity(activities)

	input := model.ClimbInput{
		Description:  "test retry",
		DesignFile:   filepath.Join(dir, "design.md"),
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      dir,
		Branch:       "belayer/climb-test",
	}

	// Create design file.
	os.WriteFile(filepath.Join(dir, "design.md"), []byte("# Design"), 0o644)

	s.env.ExecuteWorkflow(ClimbWorkflow, input)
	s.True(s.env.IsWorkflowCompleted())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// retryFakeSpawner returns RETRY on first spotter call, PASS on second.
type retryFakeSpawner struct {
	workDir     string
	spotterCall *int
}

func (f *retryFakeSpawner) Spawn(ctx context.Context, opts session.SpawnOpts) error {
	var result model.CompletionResult

	switch opts.NodeName {
	case "spotter":
		*f.spotterCall++
		if *f.spotterCall == 1 {
			result = model.CompletionResult{
				Outcome:    model.OutcomeRetry,
				TargetNode: "lead",
				Feedback:   "Fix tests",
			}
		} else {
			result = model.CompletionResult{Outcome: model.OutcomePass}
		}
	default:
		result = model.CompletionResult{Outcome: model.OutcomePass}
	}

	result.Attempt = opts.Attempt

	completionDir := filepath.Join(f.workDir, ".belayer", "completion")
	os.MkdirAll(completionDir, 0o755)

	path := filepath.Join(completionDir, fmt.Sprintf("%s-%s-attempt-%d.json",
		opts.TaskID, opts.NodeName, opts.Attempt))

	data, _ := json.Marshal(result)
	return os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/temporal/ -run TestIntegration -v`
Expected: All integration tests PASS.

- [ ] **Step 3: Run ALL v3 tests**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./internal/v3/... -v`
Expected: All v3 tests PASS.

- [ ] **Step 4: Run full project test suite**

Run: `cd /Users/donovanyohan/Documents/Programs/personal/belayer && go test ./... 2>&1 | tail -30`
Expected: All tests pass (existing v1/v2 tests remain green, v3 tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/v3/temporal/integration_test.go
git commit -m "feat(v3): add integration tests with fake spawner — full pipeline flow"
```

---

## Outcomes & Retrospective

**What worked:**
- Bottom-up task decomposition: foundation types → parsers → activities → workflow → CLI kept dependencies clean
- 4 parallel workers during peak: Tasks 1/2/5/7 and later 12/14 ran concurrently, saving wall clock time
- Plan review caught 9 real issues before any code was written (feedback file not persisted, wrong diff base, broken Temporal API, etc.)
- TDD approach: every package has tests, integration tests with fakeSpawner validate the full pipeline flow

**What didn't:**
- Several workers produced minor code quality issues (broken itoa helper, unused imports, double "climb-" in branch name) that required a fix cycle
- The `materializeCodeInput` error was silently swallowed — silent failures should always be caught in review
- Command injection in hooks.json was missed during planning — security review should be explicit in the plan review checklist

**Learnings to codify:**
- L-001: Temporal RecordHeartbeat panics outside worker context — use recover() wrapper
- L-002: Temporal test env is synchronous — add pre-tick checks in polling loops
- L-003: File-based rendezvous needs attempt scoping
- L-004: JSON-interpolated shell commands need json.Marshal escaping
