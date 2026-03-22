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
				Output:  OutputConfig{Type: "commit"},
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

func validGatePipeline() *PipelineConfig {
	return &PipelineConfig{
		Name: "gate-pipeline",
		Nodes: []NodeConfig{
			{
				Name:   "lead",
				Type:   NodeTypeNode,
				Output: OutputConfig{Type: "commit"},
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

func TestValidateGateWithNonGateResultOutput(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Output.Type = "file" // gate must use gate_result
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for gate with non-gate_result output")
	}
	if !strings.Contains(err.Error(), "gate_result") {
		t.Errorf("error should mention gate_result, got: %v", err)
	}
}

func TestValidateNonGateWithGateResultOutput(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = "gate_result" // non-gate must not use gate_result
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for non-gate with gate_result output")
	}
	if !strings.Contains(err.Error(), "gate_result") {
		t.Errorf("error should mention gate_result, got: %v", err)
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

func TestValidate_CommitOutputType(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = "commit"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected commit output type to be valid, got: %v", err)
	}
}

func TestValidate_IntakeValid(t *testing.T) {
	cfg := validPipeline()
	cfg.Intake = []IntakeConfig{
		{Name: "tickets", Type: "jira"},
		{Name: "manual", Type: "interactive"},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid intake to pass, got: %v", err)
	}
}

func TestValidate_IntakeUnknownType(t *testing.T) {
	cfg := validPipeline()
	cfg.Intake = []IntakeConfig{
		{Name: "bad", Type: "bogus"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unknown intake type")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("error should mention unknown type, got: %v", err)
	}
}

func TestValidate_IntakeDuplicateNames(t *testing.T) {
	cfg := validPipeline()
	cfg.Intake = []IntakeConfig{
		{Name: "src", Type: "jira"},
		{Name: "src", Type: "jira"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate intake names")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate, got: %v", err)
	}
}

func TestValidate_IntakeDuplicateInteractive(t *testing.T) {
	cfg := validPipeline()
	cfg.Intake = []IntakeConfig{
		{Name: "a", Type: "interactive"},
		{Name: "b", Type: "interactive"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for more than one interactive intake")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("error should mention interactive, got: %v", err)
	}
}

func TestValidate_FanOutValid(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].FanOut = "repos"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected fan_out repos to be valid, got: %v", err)
	}
}

func TestValidate_FanOutInvalid(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].FanOut = "unknown"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unknown fan_out value")
	}
	if !strings.Contains(err.Error(), "fan_out") {
		t.Errorf("error should mention fan_out, got: %v", err)
	}
}
