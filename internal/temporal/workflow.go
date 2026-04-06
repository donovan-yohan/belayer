package temporal

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/pipeline"
	"github.com/donovan-yohan/belayer/internal/route"
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
	if err := workflow.SetQueryHandler(ctx, "status", func() (string, error) {
		return fmt.Sprintf("node=%s attempt=%d", currentNode, currentAttempt), nil
	}); err != nil {
		workflow.GetLogger(ctx).Warn("failed to register status query handler", "error", err)
	}

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

			// Router dispatch: spawn child workflow for the chosen route.
			if node.IsRouter() && result.OutputPath != "" {
				childOut, routeErr := dispatchRoute(ctx, input, node, result, artifacts, nodeOutputs, ao)
				if routeErr != nil {
					return &model.ClimbOutput{
						Status:      model.ClimbFailed,
						NodeOutputs: nodeOutputs,
						Message:     fmt.Sprintf("router %q dispatch failed: %v", node.Name, routeErr),
						Branch:      input.Branch,
					}, nil
				}
				if childOut.Status == model.ClimbFailed {
					// Child failed — propagate as router node failure.
					if node.OnFail != "" && node.OnFail != "stop" {
						failJumps++
						if failJumps > maxFailJumps {
							return &model.ClimbOutput{
								Status:      model.ClimbFailed,
								NodeOutputs: nodeOutputs,
								Message:     fmt.Sprintf("router %q: child failed, exceeded max fail jumps", node.Name),
								Branch:      input.Branch,
							}, nil
						}
						if i := findNodeIndex(cfg.Nodes, node.OnFail); i != -1 {
							nodeIdx = i
							continue
						}
					}
					return &model.ClimbOutput{
						Status:      model.ClimbFailed,
						NodeOutputs: nodeOutputs,
						Message:     fmt.Sprintf("router %q: child workflow failed: %s", node.Name, childOut.Message),
						Branch:      input.Branch,
					}, nil
				}
				// Merge child outputs: namespaced for auditability, plus un-namespaced alias.
				var lastChildOutput string
				for key, path := range childOut.NodeOutputs {
					namespacedKey := fmt.Sprintf("%s/%s", node.Name, key)
					artifacts[namespacedKey] = path
					nodeOutputs[namespacedKey] = path
					lastChildOutput = path
				}
				// Un-namespaced alias: router's output key = child's terminal output.
				// Lets downstream parent nodes reference the router by name.
				if lastChildOutput != "" {
					artifacts[node.OutputKey()] = lastChildOutput
					nodeOutputs[node.Name] = lastChildOutput
				}
			}

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

// dispatchRoute reads the route decision, resolves the subpipeline, spawns a child
// workflow for the chosen route, and returns the child's output.
func dispatchRoute(
	ctx workflow.Context,
	input model.ClimbInput,
	node pipeline.NodeConfig,
	result model.CompletionResult,
	artifacts map[string]string,
	nodeOutputs map[string]string,
	ao workflow.ActivityOptions,
) (*model.ClimbOutput, error) {
	// 1. Read route-result.json content via a side-effect-free activity.
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})
	a := &Activities{}
	var routeResultBytes []byte
	if err := workflow.ExecuteActivity(actx, a.ReadFileActivity, ReadFileInput{
		FilePath: result.OutputPath,
		WorkDir:  input.WorkDir,
	}).Get(ctx, &routeResultBytes); err != nil {
		return nil, fmt.Errorf("read route result: %w", err)
	}

	// 2. Parse and validate route choice.
	validRoutes := make([]string, 0, len(node.Routes.Options))
	for name := range node.Routes.Options {
		validRoutes = append(validRoutes, name)
	}
	routeResult, err := route.ParseBytes(routeResultBytes, validRoutes)
	if err != nil {
		return nil, fmt.Errorf("invalid route result: %w", err)
	}

	chosenRoute := routeResult.Route
	option, ok := node.Routes.Options[chosenRoute]
	if !ok {
		return nil, fmt.Errorf("route %q not found in declared options", chosenRoute)
	}

	// 3. Resolve subpipeline YAML from pre-loaded map (deterministic, no file I/O).
	subYAML, ok := input.SubpipelineYAMLs[chosenRoute]
	if !ok {
		// Fallback: try the option pipeline path as key.
		subYAML, ok = input.SubpipelineYAMLs[option.Pipeline]
		if !ok {
			return nil, fmt.Errorf("subpipeline YAML for route %q not pre-loaded (key: %q)", chosenRoute, option.Pipeline)
		}
	}

	// 4. Parse and validate subpipeline.
	subCfg, err := pipeline.ParsePipeline(subYAML)
	if err != nil {
		return nil, fmt.Errorf("parse subpipeline for route %q: %w", chosenRoute, err)
	}
	if err := pipeline.Validate(subCfg); err != nil {
		return nil, fmt.Errorf("validate subpipeline for route %q: %w", chosenRoute, err)
	}

	// 5. Spawn child workflow.
	parentID := workflow.GetInfo(ctx).WorkflowExecution.ID
	childID := fmt.Sprintf("%s/route/%s", parentID, chosenRoute)

	childInput := model.ClimbInput{
		Description:  fmt.Sprintf("Subpipeline: %s (routed from %s)", chosenRoute, node.Name),
		PipelineYAML: subYAML,
		WorkDir:      input.WorkDir,
		Branch:       input.Branch,
		BaseRef:      input.BaseRef,
	}

	cwo := workflow.ChildWorkflowOptions{
		WorkflowID: childID,
	}
	childCtx := workflow.WithChildOptions(ctx, cwo)

	var childOut model.ClimbOutput
	if err := workflow.ExecuteChildWorkflow(childCtx, ClimbWorkflow, childInput).Get(ctx, &childOut); err != nil {
		return nil, fmt.Errorf("child workflow for route %q failed: %w", chosenRoute, err)
	}

	// 6. Log route decision for observability.
	workflow.GetLogger(ctx).Info("Route dispatched",
		"router", node.Name,
		"route", chosenRoute,
		"confidence", routeResult.Confidence,
		"child_status", childOut.Status,
	)

	return &childOut, nil
}

