package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/pipeline"
	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// RouteWorkflow is the main pipeline workflow — a climbing route.
// It sequences roles across phases, dispatching Type A activities synchronously
// and Type B activities with CLI-callback signals.
//
//	┌──────────┐     ┌──────────┐     ┌──────────┐
//	│ APPROACH  │ ──► │  ASCENT   │ ──► │   SEND    │
//	│ setter    │     │ decomposer│     │ pr-creator│
//	└──────────┘     │ lead      │     └──────────┘
//	                  │ spotter   │
//	                  │ ◄── loop  │
//	                  └──────────┘
func RouteWorkflow(ctx workflow.Context, input model.RouteInput) (*model.RouteOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Route workflow started", "description", input.Description)

	// Parse the pipeline definition from input, or use default.
	route, err := resolveRoute(input)
	if err != nil {
		return nil, fmt.Errorf("resolve pipeline route: %w", err)
	}

	roleOutputs := make(map[string]json.RawMessage)
	// Seed the first role's input with the pipeline description.
	initialInput, _ := json.Marshal(map[string]string{"description": input.Description})
	lastOutput := json.RawMessage(initialInput)

	// Sequence through phases and roles.
	for _, phase := range route.Phases {
		logger.Info("Entering phase", "phase", phase.Phase)

		loopIterations := make(map[string]int) // track loop counts per "from" role

		roleIdx := 0
		for roleIdx < len(phase.Roles) {
			roleDef := phase.Roles[roleIdx]
			logger.Info("Executing role", "role", roleDef.Name, "type", roleDef.ContractType)

			var result model.RoleResult
			var err error

			switch roleDef.ContractType {
			case role.TypeA:
				result, err = executeTypeA(ctx, roleDef, input.Description, lastOutput)
			case role.TypeB:
				result, err = executeTypeB(ctx, roleDef, input.Description, lastOutput)
			default:
				return nil, fmt.Errorf("unknown contract type %q for role %q", roleDef.ContractType, roleDef.Name)
			}

			if err != nil {
				return &model.RouteOutput{
					Status:      model.RunStatusFailed,
					RoleOutputs: roleOutputs,
					Message:     fmt.Sprintf("role %q failed: %v", roleDef.Name, err),
				}, nil
			}

			if result.Status == "flared" {
				return &model.RouteOutput{
					Status:      model.RunStatusFlared,
					RoleOutputs: roleOutputs,
					Message:     fmt.Sprintf("role %q flared: %s", roleDef.Name, result.Message),
				}, nil
			}

			if result.Status == "failed" {
				// Check if there's a loop that can retry from an earlier role.
				looped := false
				for _, loop := range phase.Loops {
					if loop.From == roleDef.Name {
						count := loopIterations[loop.From]
						maxIter := loop.MaxIterations
						if maxIter == 0 {
							maxIter = route.Safety.MaxLoopIterations
						}
						if count < maxIter {
							loopIterations[loop.From] = count + 1
							logger.Info("Loop triggered (fall + re-attempt)",
								"from", loop.From, "to", loop.To, "iteration", count+1)
							// Find the target role index and jump back.
							for i, r := range phase.Roles {
								if r.Name == loop.To {
									roleIdx = i
									looped = true
									break
								}
							}
							break
						}
						logger.Warn("Loop exhausted", "from", loop.From, "iterations", count)
					}
				}
				if looped {
					continue
				}
				return &model.RouteOutput{
					Status:      model.RunStatusFailed,
					RoleOutputs: roleOutputs,
					Message:     fmt.Sprintf("role %q failed: %s", roleDef.Name, result.Message),
				}, nil
			}

			// Role completed successfully.
			roleOutputs[roleDef.Name] = result.Output
			lastOutput = result.Output
			roleIdx++
		}
	}

	logger.Info("Route workflow completed")
	return &model.RouteOutput{
		Status:      model.RunStatusCompleted,
		RoleOutputs: roleOutputs,
	}, nil
}

// executeTypeA runs a Type A (pitch) role synchronously via activity.
func executeTypeA(ctx workflow.Context, roleDef role.RoleDef, description string, prevOutput json.RawMessage) (model.RoleResult, error) {
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	input := TypeAInput{
		Role:   roleDef,
		Input:  prevOutput,
		TaskID: workflow.GetInfo(ctx).WorkflowExecution.RunID,
	}

	var output TypeAOutput
	var a *Activities
	err := workflow.ExecuteActivity(ctx, a.TypeAPitchActivity, input).Get(ctx, &output)
	if err != nil {
		return model.RoleResult{Role: roleDef.Name, Status: "failed", Message: err.Error()}, nil
	}

	return model.RoleResult{
		Role:   roleDef.Name,
		Status: output.Status,
		Output: output.Output,
	}, nil
}

// executeTypeB spawns a Type B (ascent) interactive session and waits for CLI callback.
func executeTypeB(ctx workflow.Context, roleDef role.RoleDef, description string, prevOutput json.RawMessage) (model.RoleResult, error) {
	// Step 1: Spawn the interactive session via activity.
	spawnOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute, // Session spawn should be fast
		HeartbeatTimeout:    90 * time.Second,
	}
	spawnCtx := workflow.WithActivityOptions(ctx, spawnOpts)

	spawnInput := TypeBSpawnInput{
		Role:    roleDef,
		Input:   prevOutput,
		TaskID:  workflow.GetInfo(ctx).WorkflowExecution.RunID,
		WorkDir: "", // Set by activity based on crag config
	}

	var spawnOutput TypeBSpawnOutput
	var a *Activities
	err := workflow.ExecuteActivity(spawnCtx, a.TypeBSpawnActivity, spawnInput).Get(spawnCtx, &spawnOutput)
	if err != nil {
		return model.RoleResult{Role: roleDef.Name, Status: "failed", Message: "spawn failed: " + err.Error()}, nil
	}

	// Step 2: Wait for CLI callback signal (belayer <role> finish/flare/fail).
	signalCh := workflow.GetSignalChannel(ctx, SignalChannelName)
	var signal model.RoleSignal

	// Wait with a generous timeout for interactive sessions.
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, 4*time.Hour) // Max session duration

	selector := workflow.NewSelector(ctx)

	var result model.RoleResult
	signalReceived := false

	selector.AddReceive(signalCh, func(ch workflow.ReceiveChannel, more bool) {
		ch.Receive(ctx, &signal)
		// Only process signals for this specific role.
		if signal.Role == roleDef.Name {
			result = HandleRoleSignal(signal)
			signalReceived = true
			cancelTimer()
		}
	})

	selector.AddFuture(timerFuture, func(f workflow.Future) {
		if !signalReceived {
			result = model.RoleResult{
				Role:    roleDef.Name,
				Status:  "flared",
				Message: "session timed out without calling finish/flare/fail",
			}
		}
	})

	selector.Select(ctx)

	// If we got a signal for a different role, keep waiting.
	// This handles out-of-order signals.
	for !signalReceived && result.Status == "" {
		selector.Select(ctx)
	}

	return result, nil
}

// resolveRoute extracts the Route from workflow input, falling back to the default.
func resolveRoute(input model.RouteInput) (*pipeline.Route, error) {
	if len(input.RouteJSON) > 0 {
		var route pipeline.Route
		if err := json.Unmarshal(input.RouteJSON, &route); err != nil {
			return nil, fmt.Errorf("unmarshal route: %w", err)
		}
		return &route, nil
	}
	return defaultMVPRoute(), nil
}

// defaultMVPRoute returns the hardcoded MVP pipeline: setter → lead.
// This will be replaced by DSL parsing in Task 7.
func defaultMVPRoute() *pipeline.Route {
	return &pipeline.Route{
		Name: "mvp",
		Phases: []pipeline.PhaseConfig{
			{
				Phase: role.PhaseApproach,
				Roles: []role.RoleDef{
					{
						Name:         "setter",
						Phase:        role.PhaseApproach,
						ContractType: role.TypeB,
						Provider:     role.ProviderConfig{Type: "builtin"},
					},
				},
			},
			{
				Phase: role.PhaseAscent,
				Roles: []role.RoleDef{
					{
						Name:         "lead",
						Phase:        role.PhaseAscent,
						ContractType: role.TypeB,
						Provider:     role.ProviderConfig{Type: "builtin"},
					},
				},
			},
		},
		Safety: role.DefaultSafetyConfig(),
	}
}
