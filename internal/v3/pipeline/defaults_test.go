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
	if len(cfg.Nodes) != 4 {
		t.Errorf("Nodes: got %d, want 4", len(cfg.Nodes))
	}
	if len(cfg.Intake) != 1 {
		t.Errorf("Intake: got %d, want 1", len(cfg.Intake))
	} else if cfg.Intake[0].Type != "interactive" {
		t.Errorf("Intake[0].Type: got %q, want %q", cfg.Intake[0].Type, "interactive")
	}
	lead := cfg.FindNode("lead")
	if lead == nil {
		t.Fatal("lead node not found")
	}
	if lead.Output.Type != "commit" {
		t.Errorf("lead output type: got %q, want %q", lead.Output.Type, "commit")
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

func TestDefaultPipeline_SummitNode(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse default pipeline: %v", err)
	}

	summit := cfg.FindNode("summit")
	if summit == nil {
		t.Fatal("expected summit node in default pipeline")
	}
	if summit.Type != NodeTypeNode {
		t.Errorf("summit type: got %q, want %q", summit.Type, NodeTypeNode)
	}
	if summit.Output.Type != "pr" {
		t.Errorf("summit output type: got %q, want %q", summit.Output.Type, "pr")
	}
	if summit.OnPass != "stop" {
		t.Errorf("summit on_pass: got %q, want %q", summit.OnPass, "stop")
	}
	if summit.OnRetry != "self" {
		t.Errorf("summit on_retry: got %q, want %q", summit.OnRetry, "self")
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
