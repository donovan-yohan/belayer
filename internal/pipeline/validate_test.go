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


func TestValidate_PROutputType(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = "pr"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected pr output type to be valid, got: %v", err)
	}
}

func TestValidate_GateWithPROutputRejected(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Output.Type = "pr"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for gate with pr output type")
	}
	if !strings.Contains(err.Error(), "gate_result") {
		t.Errorf("error should mention gate_result, got: %v", err)
	}
}

func TestValidate_GateWithVendor(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Vendor = "codex"
	cfg.Nodes[1].Prompt = "Review the code.\n\n%{INPUT}"
	cfg.Nodes[1].Command = "" // vendor and command are mutually exclusive
	if err := Validate(cfg); err != nil {
		t.Errorf("gate with vendor+prompt should be valid, got: %v", err)
	}
}

func TestValidate_GateWithVendorNoPrompt(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Vendor = "codex"
	cfg.Nodes[1].Prompt = ""
	cfg.Nodes[1].Command = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for gate with vendor but no prompt")
	}
	if !strings.Contains(err.Error(), "no prompt") {
		t.Errorf("error should mention missing prompt, got: %v", err)
	}
}

// --- Router validation tests ---

func validRouterPipeline() *PipelineConfig {
	return &PipelineConfig{
		Name: "router-pipeline",
		Nodes: []NodeConfig{
			{
				Name:   "lead",
				Type:   NodeTypeNode,
				Output: OutputConfig{Type: "commit"},
				OnPass: "router",
				OnFail: "stop",
			},
			{
				Name:   "router",
				Type:   NodeTypeAgent,
				Vendor: "claude",
				Prompt: "Classify this change",
				Output: OutputConfig{Type: "route_result"},
				Routes: &RouteConfig{
					Mode: "choose_one",
					Options: map[string]RouteOption{
						"full-review":  {Pipeline: ".belayer/pipelines/full.yaml", Description: "Full"},
						"quick-review": {Pipeline: ".belayer/pipelines/quick.yaml", Description: "Quick"},
					},
				},
				OnPass:     "stop",
				OnRetry:    "self",
				OnFail:     "stop",
				MaxRetries: 2,
			},
		},
	}
}

func TestValidate_RouterValid(t *testing.T) {
	if err := Validate(validRouterPipeline()); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidate_RouterInvalidMode(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Routes.Mode = "fan_out"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid route mode")
	}
	if !strings.Contains(err.Error(), "choose_one") {
		t.Errorf("error should mention choose_one, got: %v", err)
	}
}

func TestValidate_RouterSingleOption(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Routes.Options = map[string]RouteOption{
		"only-option": {Pipeline: ".belayer/pipelines/only.yaml"},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for single route option")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("error should mention at least 2, got: %v", err)
	}
}

func TestValidate_RouterInvalidOptionName(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Routes.Options["bad name spaces"] = RouteOption{Pipeline: "p.yaml"}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid option name")
	}
	if !strings.Contains(err.Error(), "must match pattern") {
		t.Errorf("error should mention pattern, got: %v", err)
	}
}

func TestValidate_RouterEmptyPipeline(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Routes.Options["full-review"] = RouteOption{Pipeline: "", Description: "Full"}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for empty pipeline path")
	}
	if !strings.Contains(err.Error(), "non-empty pipeline") {
		t.Errorf("error should mention non-empty pipeline, got: %v", err)
	}
}

func TestValidate_RouterWrongOutputType(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Output.Type = "file"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for wrong output type on router")
	}
	if !strings.Contains(err.Error(), "route_result") {
		t.Errorf("error should mention route_result, got: %v", err)
	}
}

func TestValidate_RouterWithDimensions(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Dimensions = []DimensionConfig{
		{Name: "quality", Weight: 1.0},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for router with dimensions")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive, got: %v", err)
	}
}

func TestValidate_RouterNonAgentType(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].Type = NodeTypeNode
	cfg.Nodes[1].Command = "echo test"
	cfg.Nodes[1].Vendor = ""
	cfg.Nodes[1].Prompt = ""
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for non-agent router")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("error should mention agent, got: %v", err)
	}
}

func TestValidate_RouterInvalidOnRetry(t *testing.T) {
	cfg := validRouterPipeline()
	cfg.Nodes[1].OnRetry = "lead"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for router on_retry pointing to another node")
	}
	if !strings.Contains(err.Error(), "on_retry") {
		t.Errorf("error should mention on_retry, got: %v", err)
	}
}

func TestValidate_RouteResultOnNonRouter(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = "route_result"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for route_result on non-router")
	}
	if !strings.Contains(err.Error(), "route_result") {
		t.Errorf("error should mention route_result, got: %v", err)
	}
}
