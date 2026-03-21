package pipeline

import (
	"os"
	"path/filepath"
	"testing"
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
      type: code
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
