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
