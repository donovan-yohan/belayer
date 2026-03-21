package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/donovan-yohan/belayer/internal/v3/gate"
	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
)

// NodeActivityInput is the input to the NodeActivity.
type NodeActivityInput struct {
	Node      pipeline.NodeConfig
	TaskID    string
	WorkDir   string
	Attempt   int
	Artifacts map[string]string
	StartSHA  string
}

// NodeActivityOutput is the output of the NodeActivity.
type NodeActivityOutput struct {
	Result model.CompletionResult
}

// Activities holds dependencies for Temporal activity implementations.
type Activities struct {
	Spawner session.Spawner
}

// NodeActivity is the core Temporal activity that spawns a Claude session for a pipeline node,
// polls for its completion file, and returns the result.
func (a *Activities) NodeActivity(ctx context.Context, input NodeActivityInput) (*NodeActivityOutput, error) {
	// 1. Clean stale completion files from previous attempts.
	if err := cleanStaleCompletionFiles(input.WorkDir, input.TaskID, input.Node.Name, input.Attempt); err != nil {
		return nil, fmt.Errorf("clean stale completion files: %w", err)
	}

	// 1b. For gate nodes, clean stale gate output files from previous attempts.
	if input.Node.IsGate() {
		cleanStaleGateOutputs(input.WorkDir, input.Node, input.Attempt)
	}

	// 2. Write hooks config.
	if err := session.WriteHooksConfig(input.WorkDir, input.TaskID, input.Node.Name, input.Attempt); err != nil {
		return nil, fmt.Errorf("write hooks config: %w", err)
	}

	// 3. Build input prompt.
	inputPrompt := buildInputPrompt(input.Node, input.Artifacts, input.WorkDir, input.Attempt)

	// 4. For code-type inputs, materialize diff files.
	if input.Node.Input.Type == "code" {
		if err := materializeCodeInput(input.WorkDir); err != nil {
			activity.GetLogger(ctx).Warn("Failed to materialize code input", "error", err)
		}
	}

	// 5. Spawn session.
	hooksPath := session.HooksConfigPath(input.WorkDir)
	opts := session.SpawnOpts{
		NodeName:    input.Node.Name,
		TaskID:      input.TaskID,
		Attempt:     input.Attempt,
		WorkDir:     input.WorkDir,
		Description: input.Node.Description,
		HooksPath:   hooksPath,
		InputPrompt: inputPrompt,
	}
	if err := a.Spawner.Spawn(ctx, opts); err != nil {
		return nil, fmt.Errorf("spawn session: %w", err)
	}

	// 6. Poll for completion file with heartbeats.
	result, err := pollForCompletion(ctx, input.WorkDir, input.TaskID, input.Node.Name, input.Attempt, 5*time.Second)
	if err != nil {
		return nil, err
	}

	// 7. For code-type outputs, verify commits if startSHA is set.
	if input.Node.Output.Type == "code" && input.StartSHA != "" {
		if !hasNewCommits(input.WorkDir, input.StartSHA) {
			result.Outcome = model.OutcomeRetry
			result.Feedback = "no new commits detected since start"
		}
	}

	// For gate nodes, post-process: read gate-result.json, score, apply thresholds.
	if input.Node.IsGate() {
		gateResult, err := processGateResult(input.WorkDir, input.Node, input.Attempt)
		if err != nil {
			return &NodeActivityOutput{
				Result: model.CompletionResult{
					Outcome:  model.OutcomeFail,
					Feedback: fmt.Sprintf("gate processing failed: %v", err),
					Attempt:  input.Attempt,
				},
			}, nil
		}
		gateResult.Attempt = input.Attempt
		return &NodeActivityOutput{Result: gateResult}, nil
	}

	return &NodeActivityOutput{Result: result}, nil
}

// processGateResult reads gate-result.json, validates, computes weighted score,
// and applies thresholds to determine the gate outcome. This is the score-then-route
// pattern: the activity decides outcome, not the Claude session.
func processGateResult(workDir string, node pipeline.NodeConfig, attempt int) (model.CompletionResult, error) {
	// Resolve attempt-scoped gate-result.json path.
	resultBase := node.Output.Path
	if resultBase == "" {
		resultBase = ".belayer/output/gate-result.json"
	}
	resultPath := gate.ScopedPath(resultBase, attempt)
	absResultPath := filepath.Join(workDir, resultPath)

	// Read gate-result.json
	data, err := os.ReadFile(absResultPath)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("read gate result: %w", err)
	}

	// Parse
	gateResult, err := gate.ParseGateResult(data)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("parse gate result: %w", err)
	}

	// Validate all expected dimensions are present
	expectedDims := make([]string, len(node.Dimensions))
	for i, d := range node.Dimensions {
		expectedDims[i] = d.Name
	}
	// Verify gate name matches to prevent stale/mismatched data.
	if gateResult.Gate != "" && gateResult.Gate != node.Name {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: fmt.Sprintf("gate result mismatch: expected gate %q, got %q", node.Name, gateResult.Gate),
		}, nil
	}

	if err := gate.ValidateGateResult(gateResult, expectedDims); err != nil {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: fmt.Sprintf("gate produced incomplete output: %v", err),
		}, nil
	}

	// Check rationale exists (anti-gaming: rationale is mandatory)
	rationaleBase := node.Output.RationalePath
	if rationaleBase == "" {
		rationaleBase = ".belayer/output/rationale.md"
	}
	rationalePath := gate.ScopedPath(rationaleBase, attempt)
	absRationalePath := filepath.Join(workDir, rationalePath)
	if _, err := os.Stat(absRationalePath); err != nil {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: fmt.Sprintf("gate failed: rationale.md is mandatory but was not accessible: %v", err),
		}, nil
	}

	// Compute weighted score (score-then-route: we compute, not the session)
	weightedScore := gate.ComputeWeightedScore(gateResult, node.Dimensions)

	// Apply thresholds
	outcome := gate.ApplyThresholds(weightedScore, node.Thresholds)

	result := model.CompletionResult{
		Outcome:    outcome,
		OutputPath: resultPath,
	}

	// On RETRY, read rationale as feedback
	if outcome == model.OutcomeRetry {
		rationaleData, err := os.ReadFile(absRationalePath)
		if err == nil {
			result.Feedback = string(rationaleData)
		}
		result.TargetNode = node.OnRetry
	}

	return result, nil
}

// pollForCompletion checks immediately, then ticks at interval, sends heartbeats, and reads
// the completion file when it appears.
func pollForCompletion(ctx context.Context, workDir, taskID, nodeName string, attempt int, interval time.Duration) (model.CompletionResult, error) {
	// Check immediately before the first tick (handles fast/fake spawners in tests).
	if result, err := readCompletionFile(workDir, taskID, nodeName, attempt); err == nil {
		return result, nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return model.CompletionResult{}, ctx.Err()
		case <-ticker.C:
			recordHeartbeat(ctx, fmt.Sprintf("polling for %s attempt %d", nodeName, attempt))
			result, err := readCompletionFile(workDir, taskID, nodeName, attempt)
			if err == nil {
				return result, nil
			}
		}
	}
}

// recordHeartbeat calls activity.RecordHeartbeat safely, ignoring panics when called outside a Temporal worker.
func recordHeartbeat(ctx context.Context, details ...interface{}) {
	defer func() { recover() }() //nolint:errcheck
	activity.RecordHeartbeat(ctx, details...)
}

// readCompletionFile reads .belayer/completion/<taskID>-<nodeName>-attempt-<N>.json.
func readCompletionFile(workDir, taskID, nodeName string, attempt int) (model.CompletionResult, error) {
	filename := fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt)
	path := filepath.Join(workDir, ".belayer", "completion", filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("completion file not found: %w", err)
	}

	var result model.CompletionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return model.CompletionResult{}, fmt.Errorf("parse completion file: %w", err)
	}
	return result, nil
}

// cleanStaleCompletionFiles removes completion files from attempts < currentAttempt.
func cleanStaleCompletionFiles(workDir, taskID, nodeName string, currentAttempt int) error {
	dir := filepath.Join(workDir, ".belayer", "completion")
	for i := 0; i < currentAttempt; i++ {
		filename := fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, i)
		path := filepath.Join(dir, filename)
		_ = os.Remove(path) // ignore not-found errors
	}
	return nil
}

// cleanStaleGateOutputs removes gate-result and rationale files from previous attempts.
func cleanStaleGateOutputs(workDir string, node pipeline.NodeConfig, currentAttempt int) {
	resultBase := node.Output.Path
	if resultBase == "" {
		resultBase = ".belayer/output/gate-result.json"
	}
	rationaleBase := node.Output.RationalePath
	if rationaleBase == "" {
		rationaleBase = ".belayer/output/rationale.md"
	}
	for i := 0; i < currentAttempt; i++ {
		_ = os.Remove(filepath.Join(workDir, gate.ScopedPath(resultBase, i)))
		_ = os.Remove(filepath.Join(workDir, gate.ScopedPath(rationaleBase, i)))
	}
}

// buildInputPrompt constructs the input prompt for a node based on its input type and artifacts.
func buildInputPrompt(node pipeline.NodeConfig, artifacts map[string]string, workDir string, attempt ...int) string {
	if node.IsGate() {
		a := 0
		if len(attempt) > 0 {
			a = attempt[0]
		}
		var sb strings.Builder
		sb.WriteString(gate.BuildGatePrompt(node, a))
		sb.WriteString("\n")
		switch node.Input.Type {
		case "code":
			sb.WriteString("\nInput: Review the changes. Full diff at .belayer/input/diff.txt\n")
		case "file":
			key := node.Input.Key
			if key == "" {
				key = node.Name
			}
			if path, ok := artifacts[key]; ok {
				sb.WriteString(fmt.Sprintf("\nInput artifact at: %s\n", path))
			}
		}
		if feedback, ok := artifacts["feedback"]; ok && feedback != "" {
			sb.WriteString(fmt.Sprintf("\nFeedback from previous attempt: %s\n", feedback))
		}
		return sb.String()
	}

	var sb strings.Builder

	switch node.Input.Type {
	case "file":
		key := node.Input.Key
		if key == "" {
			key = node.Name
		}
		if path, ok := artifacts[key]; ok {
			sb.WriteString(fmt.Sprintf("Your input artifact is at: %s", path))
		}
	case "code":
		sb.WriteString("Review the changes. Full diff at .belayer/input/diff.txt")
	default:
		sb.WriteString(node.Description)
	}

	if feedback, ok := artifacts["feedback"]; ok && feedback != "" {
		sb.WriteString(fmt.Sprintf("\n\nFeedback from previous attempt: %s", feedback))
	}

	return sb.String()
}

// materializeCodeInput runs git diff against the default branch and writes results to .belayer/input/.
func materializeCodeInput(workDir string) error {
	inputDir := filepath.Join(workDir, ".belayer", "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return fmt.Errorf("create input dir: %w", err)
	}

	branch := detectDefaultBranch(workDir)

	diffOut, err := runGit(workDir, "diff", branch+"..HEAD")
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "diff.txt"), []byte(diffOut), 0o644); err != nil {
		return fmt.Errorf("write diff.txt: %w", err)
	}

	statOut, err := runGit(workDir, "diff", "--stat", branch+"..HEAD")
	if err != nil {
		return fmt.Errorf("git diff --stat: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "diff-stat.txt"), []byte(statOut), 0o644); err != nil {
		return fmt.Errorf("write diff-stat.txt: %w", err)
	}

	return nil
}

// detectDefaultBranch tries git symbolic-ref, then falls back to main or master.
func detectDefaultBranch(workDir string) string {
	out, err := runGit(workDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		// ref looks like refs/remotes/origin/main
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	for _, candidate := range []string{"main", "master"} {
		if _, err := runGit(workDir, "rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}
	return "main"
}

// hasNewCommits returns true if there are commits in workDir since startSHA.
func hasNewCommits(workDir, startSHA string) bool {
	out, err := runGit(workDir, "log", startSHA+"..HEAD", "--oneline")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// GetHeadSHA returns the current HEAD SHA for the given workDir.
func GetHeadSHA(workDir string) (string, error) {
	out, err := runGit(workDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// runGit runs a git command in workDir and returns combined output.
func runGit(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
