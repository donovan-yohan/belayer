package temporal

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/pipeline"
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
	if len(input.PipelineYAML) == 0 {
		return nil, fmt.Errorf("pipeline YAML is required (run 'belayer setup --framework' to install one)")
	}

	cfg, err := pipeline.ParsePipeline(input.PipelineYAML)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}
	if err := pipeline.Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate pipeline: %w", err)
	}

	// 2. Initialize state.
	artifacts := make(map[string]string)
	nodeOutputs := make(map[string]string)
	retryCount := make(map[string]int)

	// Register workflow query handler for observability.
	var currentNode string
	var currentAttempt int
	_ = workflow.SetQueryHandler(ctx, "status", func() (string, error) {
		return fmt.Sprintf("node=%s attempt=%d", currentNode, currentAttempt), nil
	})

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

		// Update observability state.
		currentNode = node.Name
		currentAttempt = retryCount[node.Name]

		// Set search attributes for Temporal Web UI visibility.
		if err := workflow.UpsertSearchAttributes(ctx, map[string]interface{}{
			"CurrentNode": node.Name,
		}); err != nil {
			workflow.GetLogger(ctx).Warn("search attributes not registered (non-fatal)", "error", err)
		}

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
			// Resolve named on_pass target; default to next sequential node.
			if node.OnPass != "" && node.OnPass != "next" {
				if i := findNodeIndex(cfg.Nodes, node.OnPass); i != -1 {
					nodeIdx = i
				} else {
					workflow.GetLogger(ctx).Warn("on_pass target not found, advancing sequentially",
						"node", node.Name, "target", node.OnPass)
					nodeIdx++
				}
			} else {
				nodeIdx++
			}

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

			// Find target node index; warn and stay on current node if not found.
			if i := findNodeIndex(cfg.Nodes, targetName); i != -1 {
				nodeIdx = i
			} else {
				workflow.GetLogger(ctx).Warn("retry target not found, retrying current node",
					"node", node.Name, "target", targetName)
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
				} else {
					workflow.GetLogger(ctx).Warn("on_fail target not found, treating as terminal failure",
						"node", node.Name, "target", node.OnFail)
					return &model.ClimbOutput{
						Status:      model.ClimbFailed,
						NodeOutputs: nodeOutputs,
						Message:     fmt.Sprintf("node %q failed: on_fail target %q not found", node.Name, node.OnFail),
						Branch:      input.Branch,
					}, nil
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
