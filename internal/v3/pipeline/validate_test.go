package pipeline

import (
	"strings"
	"testing"
)

func validPipeline() *PipelineConfig {
	return &PipelineConfig{
		Name: "my-pipeline",
		Nodes: []NodeConfig{
			{
				Name:    "build",
				Output:  OutputConfig{Type: "file"},
				OnPass:  "test",
				OnFail:  "stop",
			},
			{
				Name:    "test",
				Output:  OutputConfig{Type: "code"},
				OnPass:  "stop",
				OnRetry: "self",
				OnFail:  "stop",
			},
		},
	}
}

func TestValidateValidPipeline(t *testing.T) {
	if err := Validate(validPipeline()); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateEmptyName(t *testing.T) {
	cfg := validPipeline()
	cfg.Name = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention name, got: %v", err)
	}
}

func TestValidateNoNodes(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes = nil
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for no nodes")
	}
	if !strings.Contains(err.Error(), "node") {
		t.Errorf("error should mention node, got: %v", err)
	}
}

func TestValidateDuplicateNames(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes = append(cfg.Nodes, NodeConfig{
		Name:   "build",
		Output: OutputConfig{Type: "file"},
	})
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate node name")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate, got: %v", err)
	}
}

func TestValidateInvalidOnRetryTarget(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].OnRetry = "nonexistent-node"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid on_retry target")
	}
	if !strings.Contains(err.Error(), "on_retry") {
		t.Errorf("error should mention on_retry, got: %v", err)
	}
}

func TestValidateOnRetrySelfIsValid(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].OnRetry = "self"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected 'self' to be valid for on_retry, got: %v", err)
	}
}

func TestValidateMissingOutputType(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing output type")
	}
	if !strings.Contains(err.Error(), "output.type") {
		t.Errorf("error should mention output.type, got: %v", err)
	}
}
