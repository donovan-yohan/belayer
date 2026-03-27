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

// NodeContext is the typed contract between belayer core and framework implementations.
type NodeContext struct {
	TaskID      string                     `json:"task_id"`
	NodeName    string                     `json:"node_name"`
	NodeType    string                     `json:"node_type"`
	Attempt     int                        `json:"attempt"`
	WorkDir     string                     `json:"work_dir"`
	Description string                     `json:"description"`
	InputPrompt string                     `json:"input_prompt"`
	Artifacts   map[string]string          `json:"artifacts"`
	Dimensions  []pipeline.DimensionConfig `json:"dimensions,omitempty"`
	Thresholds  *pipeline.ThresholdConfig  `json:"thresholds,omitempty"`
}

// writeNodeContext writes node-context.json to the internal input directory.
func writeNodeContext(workDir string, nc NodeContext) error {
	dir := session.InputDir(workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create input dir: %w", err)
	}
	data, err := json.MarshalIndent(nc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal node context: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "node-context.json"), data, 0o644)
}

// Activities holds dependencies for Temporal activity implementations.
type Activities struct {
	Spawner session.Spawner
}

// WriteFeedbackInput is the input to WriteFeedbackActivity.
type WriteFeedbackInput struct {
	WorkDir      string
	FeedbackText string
}

// WriteFeedbackActivity writes feedback text to disk so the target session can read it on retry.
func (a *Activities) WriteFeedbackActivity(ctx context.Context, input WriteFeedbackInput) (string, error) {
	feedbackPath := filepath.Join(session.InputDir(input.WorkDir), "feedback.md")
	if err := os.MkdirAll(filepath.Dir(feedbackPath), 0o755); err != nil {
		return "", fmt.Errorf("create feedback dir: %w", err)
	}
	if err := os.WriteFile(feedbackPath, []byte(input.FeedbackText), 0o644); err != nil {
		return "", fmt.Errorf("write feedback: %w", err)
	}
	return ".belayer/.internal/input/feedback.md", nil
}

// NodeActivity is the core Temporal activity that spawns a process for a pipeline node,
// polls for its completion file, and returns the result.
func (a *Activities) NodeActivity(ctx context.Context, input NodeActivityInput) (*NodeActivityOutput, error) {
	// 1. Clean stale completion files from previous attempts.
	cleanStaleCompletionFiles(input.WorkDir, input.TaskID, input.Node.Name, input.Attempt)

	// 2. Build input prompt.
	inputPrompt := buildInputPrompt(input.Node, input.Artifacts)

	// 3. For code/commit-type inputs, materialize diff files.
	if input.Node.Input.Type == "code" || input.Node.Input.Type == "commit" {
		if err := materializeCodeInput(input.WorkDir); err != nil {
			if input.Node.IsGate() {
				return nil, fmt.Errorf("materialize code input for gate %q: %w", input.Node.Name, err)
			}
			activity.GetLogger(ctx).Warn("Failed to materialize code input", "error", err)
		}
	}

	// 4. Write node-context.json.
	var thresholds *pipeline.ThresholdConfig
	if input.Node.IsGate() {
		thresholds = &input.Node.Thresholds
	}
	nc := NodeContext{
		TaskID:      input.TaskID,
		NodeName:    input.Node.Name,
		NodeType:    string(input.Node.EffectiveType()),
		Attempt:     input.Attempt,
		WorkDir:     input.WorkDir,
		Description: input.Node.Description,
		InputPrompt: inputPrompt,
		Artifacts:   input.Artifacts,
		Dimensions:  input.Node.Dimensions,
		Thresholds:  thresholds,
	}
	if err := writeNodeContext(input.WorkDir, nc); err != nil {
		return nil, fmt.Errorf("write node context: %w", err)
	}

	// 5. Spawn session.
	opts := session.SpawnOpts{
		NodeName:    input.Node.Name,
		TaskID:      input.TaskID,
		Attempt:     input.Attempt,
		WorkDir:     input.WorkDir,
		Description: input.Node.Description,
		Command:     input.Node.Command,
		InputPrompt: inputPrompt,
	}
	exitCh, err := a.Spawner.Spawn(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("spawn session: %w", err)
	}

	// 6. Poll for completion file with heartbeats, checking exit channel.
	result, err := pollForCompletion(ctx, input.WorkDir, input.TaskID, input.Node.Name, input.Attempt, 5*time.Second, exitCh)
	if err != nil {
		return nil, err
	}

	// 7. For commit-type outputs, verify commits if startSHA is set.
	if input.Node.Output.Type == "commit" && input.StartSHA != "" {
		hasCommits, gitErr := hasNewCommits(input.WorkDir, input.StartSHA)
		if gitErr != nil {
			activity.GetLogger(ctx).Warn("Failed to check for new commits", "error", gitErr)
		} else if !hasCommits {
			result.Outcome = model.OutcomeRetry
			result.Feedback = "no new commits detected since start"
		}
	}

	// For gate nodes, post-process: read gate-result.json, score, apply thresholds.
	if input.Node.IsGate() {
		gateResult, err := processGateResult(input.WorkDir, input.Node)
		if err != nil {
			return nil, fmt.Errorf("gate %q processing failed: %w", input.Node.Name, err)
		}
		gateResult.Attempt = input.Attempt
		return &NodeActivityOutput{Result: gateResult}, nil
	}

	return &NodeActivityOutput{Result: result}, nil
}

// processGateResult reads gate-result.json, validates, computes weighted score,
// and applies thresholds to determine the gate outcome. This is the score-then-route
// pattern: the activity decides outcome, not the Claude session.
func processGateResult(workDir string, node pipeline.NodeConfig) (model.CompletionResult, error) {
	// Resolve gate-result.json path
	resultPath := node.Output.Path
	if resultPath == "" {
		resultPath = ".belayer/.internal/output/gate-result.json"
	}
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
	rationalePath := node.Output.RationalePath
	if rationalePath == "" {
		rationalePath = ".belayer/.internal/output/rationale.md"
	}
	absRationalePath := filepath.Join(workDir, rationalePath)
	if _, err := os.Stat(absRationalePath); err != nil {
		return model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: fmt.Sprintf("gate failed: rationale.md is mandatory but was not accessible: %v", err),
		}, nil
	}

	// Compute weighted score (score-then-route: we compute, not the session)
	weightedScore, err := gate.ComputeWeightedScore(gateResult, node.Dimensions)
	if err != nil {
		return model.CompletionResult{}, fmt.Errorf("gate %q score computation: %w", node.Name, err)
	}

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
// the completion file when it appears. If exitCh fires (process exited), checks one more
// time for the completion file before returning an error.
func pollForCompletion(ctx context.Context, workDir, taskID, nodeName string, attempt int, interval time.Duration, exitCh <-chan error) (model.CompletionResult, error) {
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
		case exitErr, ok := <-exitCh: // nil exitCh blocks forever (Go spec), so only ticker and ctx.Done fire when spawner returns nil — intentional
			// Process exited (cleanly or with error). Check one last time for completion file.
			if result, readErr := readCompletionFile(workDir, taskID, nodeName, attempt); readErr == nil {
				return result, nil
			}
			if ok && exitErr != nil {
				return model.CompletionResult{}, fmt.Errorf("node %q process exited without completion file: %w", nodeName, exitErr)
			}
			return model.CompletionResult{}, fmt.Errorf("node %q process exited without writing completion file", nodeName)
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
func recordHeartbeat(ctx context.Context, details ...any) {
	defer func() {
		if r := recover(); r != nil {
			// Only swallow the expected "not in activity context" panic from Temporal SDK.
			// Re-panic on unexpected errors to avoid hiding real bugs.
			if msg, ok := r.(string); ok && strings.Contains(msg, "Not an activity context") {
				return
			}
			panic(r)
		}
	}()
	activity.RecordHeartbeat(ctx, details...)
}

// readCompletionFile reads .belayer/.internal/completion/<taskID>-<nodeName>-attempt-<N>.json.
func readCompletionFile(workDir, taskID, nodeName string, attempt int) (model.CompletionResult, error) {
	path := session.CompletionFilePath(workDir, taskID, nodeName, attempt)

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
func cleanStaleCompletionFiles(workDir, taskID, nodeName string, currentAttempt int) {
	for i := 0; i < currentAttempt; i++ {
		path := session.CompletionFilePath(workDir, taskID, nodeName, i)
		_ = os.Remove(path) // ignore not-found errors
	}
}

// resolveInputKey returns the artifact key for a node's input, defaulting to node name.
func resolveInputKey(node pipeline.NodeConfig) string {
	if node.Input.Key != "" {
		return node.Input.Key
	}
	return node.Name
}

// buildInputPrompt constructs the input prompt for a node based on its input type and artifacts.
func buildInputPrompt(node pipeline.NodeConfig, artifacts map[string]string) string {
	var sb strings.Builder

	if node.IsGate() {
		sb.WriteString(gate.BuildGatePrompt(node))
		sb.WriteString("\n")
		switch node.Input.Type {
		case "code", "commit":
			sb.WriteString("\nInput: Review the changes. Full diff at .belayer/.internal/input/diff.txt\n")
		case "file":
			if path, ok := artifacts[resolveInputKey(node)]; ok {
				fmt.Fprintf(&sb, "\nInput artifact at: %s\n", path)
			}
		}
	} else {
		switch node.Input.Type {
		case "file":
			if path, ok := artifacts[resolveInputKey(node)]; ok {
				fmt.Fprintf(&sb, "Your input artifact is at: %s", path)
			}
		case "code", "commit":
			sb.WriteString("Review the changes. Full diff at .belayer/.internal/input/diff.txt")
		default:
			sb.WriteString(node.Description)
		}
	}

	if feedback, ok := artifacts["feedback"]; ok && feedback != "" {
		fmt.Fprintf(&sb, "\n\nFeedback from previous attempt: %s", feedback)
	}

	return sb.String()
}

// materializeCodeInput runs git diff against the default branch and writes results to .belayer/.internal/input/.
func materializeCodeInput(workDir string) error {
	inputDir := session.InputDir(workDir)
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
func hasNewCommits(workDir, startSHA string) (bool, error) {
	out, err := runGit(workDir, "log", startSHA+"..HEAD", "--oneline")
	if err != nil {
		return false, fmt.Errorf("git log %s..HEAD: %w", startSHA, err)
	}
	return strings.TrimSpace(out) != "", nil
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
