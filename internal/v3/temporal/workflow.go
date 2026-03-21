package temporal

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

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
		for i, n := range cfg.Nodes {
			if n.Name == input.FromNode {
				startIdx = i
				break
			}
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

			// Write feedback to disk.
			if result.Feedback != "" {
				feedbackPath := filepath.Join(input.WorkDir, ".belayer", "input", "feedback.md")
				_ = os.MkdirAll(filepath.Dir(feedbackPath), 0o755)
				_ = os.WriteFile(feedbackPath, []byte(result.Feedback), 0o644)
				artifacts["feedback"] = result.Feedback
			}

			// Determine retry target: verdict > node.OnRetry > self.
			targetName := result.TargetNode
			if targetName == "" {
				targetName = node.OnRetry
			}
			if targetName == "" || targetName == "self" {
				targetName = node.Name
			}

			// Find target node index.
			found := false
			for i, n := range cfg.Nodes {
				if n.Name == targetName {
					nodeIdx = i
					found = true
					break
				}
			}
			if !found {
				// Target not found, retry self.
				// nodeIdx stays the same.
			}

		case model.OutcomeFail:
			if node.OnFail != "" && node.OnFail != "stop" {
				// Jump to named node.
				for i, n := range cfg.Nodes {
					if n.Name == node.OnFail {
						nodeIdx = i
						break
					}
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
