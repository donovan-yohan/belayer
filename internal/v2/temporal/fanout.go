package temporal

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"go.temporal.io/sdk/workflow"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// DecomposerOutput is the structured output from a fan_out decomposer role.
type DecomposerOutput struct {
	Repos map[string]RepoTask `json:"repos"`
}

// RepoTask is the decomposer's per-repo decision.
type RepoTask struct {
	Needed    bool     `json:"needed"`
	Spec      string   `json:"spec,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// FanOutResult collects per-repo results after parallel execution.
type FanOutResult struct {
	mu      sync.Mutex
	Results map[string]model.RoleResult // repoName → final result (from spotter)
	Skipped map[string]string           // repoName → reason for skipping
}

// executeFanOut runs lead → spotter per repo in parallel, respecting dependencies.
// Returns the collected results for fan-in to the anchor.
//
//	┌─── repo-a (no deps) ──► lead ──► spotter ───┐
//	├─── repo-b (depends a) ──► lead ──► spotter ──┤──► collected results
//	└─── repo-c (skipped) ─────────────────────────┘
func executeFanOut(
	ctx workflow.Context,
	decomposerOutput DecomposerOutput,
	perRoles []role.RoleDef, // Roles annotated with per: repo
	route RouteInfo,
) (*FanOutResult, error) {
	logger := workflow.GetLogger(ctx)

	result := &FanOutResult{
		Results: make(map[string]model.RoleResult),
		Skipped: make(map[string]string),
	}

	// Separate needed from skipped repos.
	needed := make(map[string]RepoTask)
	for name, task := range decomposerOutput.Repos {
		if task.Needed {
			needed[name] = task
		} else {
			result.Skipped[name] = task.Reason
			logger.Info("Repo skipped by decomposer", "repo", name, "reason", task.Reason)
		}
	}

	if len(needed) == 0 {
		logger.Info("Decomposer marked all repos as unneeded")
		return result, nil
	}

	// Topological sort by depends_on.
	levels, err := topoSort(needed)
	if err != nil {
		return nil, fmt.Errorf("dependency ordering: %w", err)
	}

	// Execute each dependency level in parallel.
	for levelIdx, level := range levels {
		logger.Info("Fan-out level", "level", levelIdx, "repos", level)

		var wg sync.WaitGroup

		for _, repoName := range level {
			repoName := repoName
			task := needed[repoName]
			wg.Add(1)

			workflow.Go(ctx, func(gCtx workflow.Context) {
				defer wg.Done()

				repoInput, _ := json.Marshal(map[string]string{
					"repo": repoName,
					"spec": task.Spec,
				})

				// Execute each per-repo role sequentially (lead → spotter).
				var lastResult model.RoleResult
				for _, roleDef := range perRoles {
					var res model.RoleResult
					var execErr error

					switch roleDef.ContractType {
					case role.TypeA:
						res, execErr = executeTypeAForRepo(gCtx, roleDef, repoName, repoInput, route)
					case role.TypeB:
						res, execErr = executeTypeBForRepo(gCtx, roleDef, repoName, repoInput, route)
					}

					if execErr != nil {
						res = model.RoleResult{Role: roleDef.Name, Status: "failed", Message: execErr.Error()}
					}

					lastResult = res

					// If role flared, stop this repo's pipeline but don't block others.
					if res.Status == "flared" || res.Status == "failed" {
						break
					}
					repoInput = res.Output // Chain output to next role.
				}

				result.mu.Lock()
				result.Results[repoName] = lastResult
				result.mu.Unlock()
			})
		}

		// Wait for all goroutines in this level to finish.
		wg.Wait()
	}

	return result, nil
}

// RouteInfo carries context needed during fan-out execution.
type RouteInfo struct {
	TaskID    string
	WorkDir   string // Base crag directory
	Worktrees map[string]string // repoName → worktree path (populated by activity)
}

// executeTypeAForRepo runs a Type A role for a specific repo.
func executeTypeAForRepo(ctx workflow.Context, roleDef role.RoleDef, repoName string, input json.RawMessage, route RouteInfo) (model.RoleResult, error) {
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * 60_000_000_000, // 10 minutes in nanoseconds
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	actInput := TypeAInput{
		Role:   roleDef,
		Input:  input,
		TaskID: route.TaskID,
	}

	var output TypeAOutput
	var a *Activities
	err := workflow.ExecuteActivity(ctx, a.TypeAPitchActivity, actInput).Get(ctx, &output)
	if err != nil {
		return model.RoleResult{Role: roleDef.Name, Status: "failed", Message: err.Error()}, nil
	}
	return model.RoleResult{Role: roleDef.Name, Status: output.Status, Output: output.Output}, nil
}

// executeTypeBForRepo runs a Type B role for a specific repo, waiting for a Repo-scoped signal.
func executeTypeBForRepo(ctx workflow.Context, roleDef role.RoleDef, repoName string, input json.RawMessage, route RouteInfo) (model.RoleResult, error) {
	spawnOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * 60_000_000_000,
		HeartbeatTimeout:    90_000_000_000,
	}
	spawnCtx := workflow.WithActivityOptions(ctx, spawnOpts)

	workDir := route.WorkDir
	if wt, ok := route.Worktrees[repoName]; ok {
		workDir = wt
	}

	spawnInput := TypeBSpawnInput{
		Role:    roleDef,
		Input:   input,
		TaskID:  route.TaskID,
		WorkDir: workDir,
	}
	// TODO: pass repo name to spawn activity for window naming + system prompt

	var spawnOutput TypeBSpawnOutput
	var a *Activities
	err := workflow.ExecuteActivity(spawnCtx, a.TypeBSpawnActivity, spawnInput).Get(spawnCtx, &spawnOutput)
	if err != nil {
		return model.RoleResult{Role: roleDef.Name, Status: "failed", Message: "spawn failed: " + err.Error()}, nil
	}

	// Wait for signal matching this Role+Repo.
	signalCh := workflow.GetSignalChannel(ctx, SignalChannelName)
	var signal model.RoleSignal

	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	timerFuture := workflow.NewTimer(timerCtx, 4*3600_000_000_000) // 4 hours

	selector := workflow.NewSelector(ctx)
	var result model.RoleResult
	signalReceived := false

	selector.AddReceive(signalCh, func(ch workflow.ReceiveChannel, more bool) {
		ch.Receive(ctx, &signal)
		if signal.Role == roleDef.Name && (signal.Repo == repoName || signal.Repo == "") {
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
				Message: fmt.Sprintf("session for %s/%s timed out", roleDef.Name, repoName),
			}
		}
	})

	selector.Select(ctx)
	for !signalReceived && result.Status == "" {
		selector.Select(ctx)
	}

	return result, nil
}

// topoSort performs topological sort on repos by depends_on.
// Returns levels: repos at level 0 have no deps, level 1 depends on level 0, etc.
func topoSort(repos map[string]RepoTask) ([][]string, error) {
	// Build in-degree map.
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep → repos that depend on it

	for name, task := range repos {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
		for _, dep := range task.DependsOn {
			if _, exists := repos[dep]; !exists {
				// Dependency on a repo not in the needed set — skip.
				continue
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	var levels [][]string
	for len(inDegree) > 0 {
		// Collect repos with zero in-degree.
		var level []string
		for name, deg := range inDegree {
			if deg == 0 {
				level = append(level, name)
			}
		}
		if len(level) == 0 {
			// Circular dependency.
			var remaining []string
			for name := range inDegree {
				remaining = append(remaining, name)
			}
			return nil, fmt.Errorf("circular dependency among repos: %v", remaining)
		}

		sort.Strings(level) // Deterministic ordering within a level.
		levels = append(levels, level)

		// Remove processed repos and decrement dependents.
		for _, name := range level {
			delete(inDegree, name)
			for _, dep := range dependents[name] {
				if _, ok := inDegree[dep]; ok {
					inDegree[dep]--
				}
			}
		}
	}

	return levels, nil
}
