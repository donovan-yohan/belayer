package pipeline

import "testing"

func TestDefaultPipelineParses(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.Name != "default-climb" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "default-climb")
	}
	if len(cfg.Nodes) != 3 {
		t.Errorf("Nodes: got %d, want 3", len(cfg.Nodes))
	}
}

func TestDefaultPipelineValidates(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("validation error: %v", err)
	}
}

func TestDefaultPipelineSpotterOnRetry(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	spotter := cfg.FindNode("spotter")
	if spotter == nil {
		t.Fatal("spotter node not found")
	}
	if spotter.OnRetry != "lead" {
		t.Errorf("spotter.OnRetry: got %q, want %q", spotter.OnRetry, "lead")
	}
}

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
