package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const validYAML = `
name: test-pipeline
nodes:
  - name: build
    description: Build the project
    output:
      type: file
      path: /tmp/build.out
    on_pass: test
    on_fail: stop
  - name: test
    description: Run tests
    output:
      type: commit
    on_pass: stop
    on_fail: stop
`

func TestParsePipelineValid(t *testing.T) {
	cfg, err := ParsePipeline([]byte(validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "test-pipeline" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "test-pipeline")
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("Nodes: got %d, want 2", len(cfg.Nodes))
	}
	if cfg.Nodes[0].Name != "build" {
		t.Errorf("Nodes[0].Name: got %q, want %q", cfg.Nodes[0].Name, "build")
	}
	if cfg.Nodes[1].Name != "test" {
		t.Errorf("Nodes[1].Name: got %q, want %q", cfg.Nodes[1].Name, "test")
	}
}

func TestParsePipelineInvalidYAML(t *testing.T) {
	_, err := ParsePipeline([]byte(":\tinvalid: yaml: :::"))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestParsePipelineFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cfg, err := ParsePipelineFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "test-pipeline" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "test-pipeline")
	}
}

func TestParsePipelineFileNotFound(t *testing.T) {
	_, err := ParsePipelineFile("/nonexistent/path/pipeline.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

const gateYAML = `
name: gate-pipeline
nodes:
  - name: lead
    type: node
    description: Write code
    output:
      type: commit
    on_pass: review
    on_fail: stop
  - name: review
    type: gate
    description: Review the code
    input:
      type: commit
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

func TestParsePipeline_CommandField(t *testing.T) {
	yaml := `
name: test
nodes:
  - name: worker
    type: node
    command: ./scripts/run.sh
    description: do work
    input: { type: description }
    output: { type: file }
    on_pass: stop
`
	cfg, err := ParsePipeline([]byte(yaml))
	require.NoError(t, err)
	require.Equal(t, "./scripts/run.sh", cfg.Nodes[0].Command)
}

func TestOutputKey(t *testing.T) {
	// No key set: falls back to node name.
	n := &NodeConfig{Name: "build", Output: OutputConfig{Type: "file"}}
	if got := n.OutputKey(); got != "build" {
		t.Errorf("OutputKey (no key): got %q, want %q", got, "build")
	}

	// Key explicitly set.
	n.Output.Key = "build-artifact"
	if got := n.OutputKey(); got != "build-artifact" {
		t.Errorf("OutputKey (with key): got %q, want %q", got, "build-artifact")
	}
}
