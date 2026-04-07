package pipeline

import (
	"strings"
	"testing"
)

func validPollPipeline() *PipelineConfig {
	return &PipelineConfig{
		Name: "poll-pipeline",
		Nodes: []NodeConfig{{
			Name:    "wait-for-ci",
			Command: "echo ready",
			Output:  OutputConfig{Type: "file"},
			Poll: &PollConfig{
				Command:     "echo ready",
				Interval:    "5s",
				OnDuplicate: "skip",
			},
		}},
	}
}

func TestValidatePoll_Valid(t *testing.T) {
	if err := Validate(validPollPipeline()); err != nil {
		t.Fatalf("expected valid poll config, got: %v", err)
	}
}

func TestValidatePoll_MissingCommand(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Poll.Command = ""
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "poll.command") {
		t.Fatalf("expected poll.command error, got: %v", err)
	}
}

func TestValidatePoll_InvalidInterval(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Poll.Interval = "nope"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "poll.interval") {
		t.Fatalf("expected poll.interval error, got: %v", err)
	}
}

func TestValidatePoll_InvalidOnDuplicate(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Poll.OnDuplicate = "maybe"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "poll.on_duplicate") {
		t.Fatalf("expected poll.on_duplicate error, got: %v", err)
	}
}

func TestValidatePoll_PollWithDimensions(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Dimensions = []DimensionConfig{{Name: "x", Weight: 1}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "poll and dimensions") {
		t.Fatalf("expected poll/dimensions error, got: %v", err)
	}
}

func TestValidatePoll_PollWithRoutes(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Type = NodeTypeAgent
	cfg.Nodes[0].Command = ""
	cfg.Nodes[0].Vendor = "claude"
	cfg.Nodes[0].Prompt = "Test prompt with %{INPUT}"
	cfg.Nodes[0].Output.Type = "route_result"
	cfg.Nodes[0].Routes = &RouteConfig{
		Mode: "choose_one",
		Options: map[string]RouteOption{
			"route-a": {Pipeline: "a.yaml"},
			"route-b": {Pipeline: "b.yaml"},
		},
	}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "poll and routes") {
		t.Fatalf("expected poll/routes error, got: %v", err)
	}
}

func TestValidatePoll_NoCommandNonFileOutput(t *testing.T) {
	cfg := validPollPipeline()
	cfg.Nodes[0].Command = ""
	cfg.Nodes[0].Vendor = ""
	cfg.Nodes[0].Output.Type = "commit"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "output.type \"file\"") {
		t.Fatalf("expected poll/file output error, got: %v", err)
	}
}
