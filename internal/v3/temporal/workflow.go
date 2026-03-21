package temporal

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// findNodeIndex returns the index of the node with the given name, or -1 if not found.
func findNodeIndex(nodes []pipeline.NodeConfig, name string) int {
	for i, n := range nodes {
		if n.Name == name {
			return i
		}
	}
	return -1
}

// ClimbWorkflow is the core Temporal workflow that sequences pipeline nodes.
func ClimbWorkflow(ctx workflow.Context, input model.ClimbInput) (*model.ClimbOutput, error) {
	// 1. Parse pipeline.
	var pipelineYAML []byte
	if len(input.PipelineYAML) > 0 {
		pipelineYAML = input.PipelineYAML
	} else {
		pipelineYAML = []byte(pipeline.DefaultPipelineYAML)
	}

	cfg, err := pipeline.ParsePipeline(pipelineYAML)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	// 2. Initialize state.
	artifacts := make(map[string]string)
	nodeOutputs := make(map[string]string)
	retryCount := make(map[string]int)

	// 3. Seed design_doc artifact.
	if input.DesignFile != "" {
		artifacts["design_doc"] = input.DesignFile
	}

	// 4. Find start index.
	startIdx := 0
	if input.FromNode != "" {
		startIdx = findNodeIndex(cfg.Nodes, input.FromNode)
		if startIdx == -1 {
			return nil, fmt.Errorf("from-node %q not found in pipeline %q", input.FromNode, cfg.Name)
		}
	}

	// 5. Activity options.
	ao := workflow.ActivityOptions{
		StartToCloseTimeout:    2 * time.Hour,
		HeartbeatTimeout:       60 * time.Second,
		ScheduleToCloseTimeout: 24 * time.Hour,
	}
	actx := workflow.WithActivityOptions(ctx, ao)

	a := &Activities{}
	nodeIdx := startIdx
	failJumps := 0 // Cycle detection for OnFail jumps
	const maxFailJumps = 5

	for nodeIdx < len(cfg.Nodes) {
		node := cfg.Nodes[nodeIdx]

		actInput := NodeActivityInput{
			Node:      node,
			TaskID:    workflow.GetInfo(ctx).WorkflowExecution.ID,
			WorkDir:   input.WorkDir,
			Attempt:   retryCount[node.Name],
			Artifacts: artifacts,
		}

		var out NodeActivityOutput
		if err := workflow.ExecuteActivity(actx, a.NodeActivity, actInput).Get(ctx, &out); err != nil {
			return &model.ClimbOutput{
				Status:      model.ClimbFailed,
				NodeOutputs: nodeOutputs,
				Message:     fmt.Sprintf("node %q activity error: %v", node.Name, err),
				Branch:      input.Branch,
			}, nil
		}

		result := out.Result

		switch result.Outcome {
		case model.OutcomePass:
			// Store output artifact.
			if result.OutputPath != "" {
				artifacts[node.OutputKey()] = result.OutputPath
			}
			nodeOutputs[node.Name] = result.OutputPath

			if node.OnPass == "stop" {
				return &model.ClimbOutput{
					Status:      model.ClimbCompleted,
					NodeOutputs: nodeOutputs,
					Branch:      input.Branch,
				}, nil
			}
			nodeIdx++

		case model.OutcomeRetry:
			retryCount[node.Name]++
			if node.MaxRetries > 0 && retryCount[node.Name] > node.MaxRetries {
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q exceeded max retries (%d)", node.Name, node.MaxRetries),
					Branch:      input.Branch,
				}, nil
			}

			// Write feedback to disk so the target session can read it.
			if result.Feedback != "" {
				fbao := workflow.ActivityOptions{
					StartToCloseTimeout: 30 * time.Second,
				}
				fbactx := workflow.WithActivityOptions(ctx, fbao)
				var feedbackRelPath string
				if err := workflow.ExecuteActivity(fbactx, a.WriteFeedbackActivity, WriteFeedbackInput{
					WorkDir:      input.WorkDir,
					FeedbackText: result.Feedback,
				}).Get(ctx, &feedbackRelPath); err != nil {
					workflow.GetLogger(ctx).Warn("Failed to write feedback file", "error", err)
				} else {
					artifacts["feedback"] = feedbackRelPath
				}
			}

			// Determine retry target: verdict > node.OnRetry > self.
			targetName := result.TargetNode
			if targetName == "" {
				targetName = node.OnRetry
			}
			if targetName == "" || targetName == "self" {
				targetName = node.Name
			}

			// Find target node index; stay on current node if not found.
			if i := findNodeIndex(cfg.Nodes, targetName); i != -1 {
				nodeIdx = i
			}

		case model.OutcomeFail:
			if node.OnFail != "" && node.OnFail != "stop" {
				failJumps++
				if failJumps > maxFailJumps {
					return &model.ClimbOutput{
						Status:      model.ClimbFailed,
						NodeOutputs: nodeOutputs,
						Message:     fmt.Sprintf("node %q: exceeded max fail jumps (%d), possible cycle", node.Name, maxFailJumps),
						Branch:      input.Branch,
					}, nil
				}
				if i := findNodeIndex(cfg.Nodes, node.OnFail); i != -1 {
					nodeIdx = i
				}
			} else {
				return &model.ClimbOutput{
					Status:      model.ClimbFailed,
					NodeOutputs: nodeOutputs,
					Message:     fmt.Sprintf("node %q failed", node.Name),
					Branch:      input.Branch,
				}, nil
			}
		}
	}

	// Exhausted all nodes.
	return &model.ClimbOutput{
		Status:      model.ClimbCompleted,
		NodeOutputs: nodeOutputs,
		Branch:      input.Branch,
	}, nil
}
