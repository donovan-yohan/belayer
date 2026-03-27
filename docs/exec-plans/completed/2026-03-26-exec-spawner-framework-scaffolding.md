# ExecSpawner + Framework Scaffolding Implementation Plan

> **Status**: Active | **Created**: 2026-03-26 | **Last Updated**: 2026-03-26
> **Design Doc**: `docs/design-docs/2026-03-26-exec-spawner-framework-scaffolding-design.md`
> **Consulted Learnings**: L-20260321-score-then-route, L-20260320-rendezvous-attempt-scope, L-20260321-workflow-no-file-io
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-26 | Design | Approach B/A: ExecSpawner + framework scaffolding | Belayer stays useful out of box while being pluggable |
| 2026-03-26 | Design | Context passing: env vars + node-context.json | Env vars for basics, JSON for rich context; language-agnostic |
| 2026-03-26 | Design | Async spawn model: exec background, poll completion | Same model as TmuxSpawner; fire and forget |
| 2026-03-26 | Design | Missing command: validation error | Explicit over magic defaults |
| 2026-03-26 | Design | SDK deferred to P2 TODO | Depends on this refactor proving the contract |
| 2026-03-26 | CEO Review | HOLD SCOPE, 0 critical gaps | Clean refactor, no scope expansion needed |
| 2026-03-26 | Eng Review | Spawner interface returns exit channel (1A) | Cleaner long-term, no users for backward compat |

## Progress

- [x] Task 1: Add `Command` field to pipeline model + validation _(completed 2026-03-26)_
- [x] Task 2: Introduce `internalDir` helper and migrate paths _(completed 2026-03-26)_
- [x] Task 3: Update Spawner interface and implement ExecSpawner _(completed 2026-03-26)_
- [x] Task 4: Update NodeActivity to write node-context.json and use ExecSpawner _(completed 2026-03-26)_
- [x] Task 5: Update node-complete to prefer BELAYER_WORK_DIR _(completed 2026-03-26 via Task 2)_
- [x] Task 6: Wire ExecSpawner into CLI commands _(completed 2026-03-26)_
- [x] Task 7: Create framework embed infrastructure and `belayer setup` command _(completed 2026-03-26)_
- [x] Task 8: Create claude-tmux framework _(completed 2026-03-26)_
- [x] Task 9: Update integration tests for new interface _(completed 2026-03-26)_

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Add `Command` field to pipeline model + validation

**Files:**
- Modify: `internal/v3/pipeline/model.go:46-61`
- Modify: `internal/v3/pipeline/validate.go:46-117`
- Modify: `internal/v3/pipeline/validate_test.go`
- Modify: `internal/v3/pipeline/parser_test.go`

- [ ] **Step 1: Add Command field to NodeConfig**

In `internal/v3/pipeline/model.go`, add `Command` to the `NodeConfig` struct:

```go
// NodeConfig defines a single pipeline node.
type NodeConfig struct {
	Name        string            `yaml:"name" json:"name"`
	Type        NodeType          `yaml:"type,omitempty" json:"type,omitempty"`
	Command     string            `yaml:"command" json:"command"`
	Description string            `yaml:"description" json:"description"`
	Input       InputConfig       `yaml:"input" json:"input"`
	Output      OutputConfig      `yaml:"output" json:"output"`
	// ... rest unchanged
}
```

- [ ] **Step 2: Write failing validation test**

In `internal/v3/pipeline/validate_test.go`, add a test that a node without `command:` fails validation:

```go
func TestValidate_MissingCommand(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "test",
		Nodes: []NodeConfig{{
			Name:        "worker",
			Type:        NodeTypeNode,
			Description: "do work",
			Input:       InputConfig{Type: "description"},
			Output:      OutputConfig{Type: "file"},
			OnPass:      "stop",
		}},
	}
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "command")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/v3/pipeline/ -run TestValidate_MissingCommand -v`
Expected: FAIL — no validation for command yet.

- [ ] **Step 4: Add command validation to Validate()**

In `internal/v3/pipeline/validate.go`, inside the node validation loop (around line 46), add after the output type check:

```go
if n.Command == "" {
	return fmt.Errorf("node %q: command is required", n.Name)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/v3/pipeline/ -run TestValidate_MissingCommand -v`
Expected: PASS

- [ ] **Step 6: Write parser test for command field**

In `internal/v3/pipeline/parser_test.go`, add a test that `command:` parses correctly:

```go
func TestParsePipeline_CommandField(t *testing.T) {
	yaml := `
name: test
nodes:
  - name: worker
    type: node
    command: ./scripts/run.sh
    description: do work
    input: { type: description }
    output: { type: file }
    on_pass: stop
`
	cfg, err := ParsePipeline([]byte(yaml))
	require.NoError(t, err)
	require.Equal(t, "./scripts/run.sh", cfg.Nodes[0].Command)
}
```

- [ ] **Step 7: Run all pipeline tests**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: PASS (parser test should already work since Command is a YAML field)

- [ ] **Step 8: Fix existing tests that now fail due to missing command**

Existing validation tests use node configs without `command:`. Update all test fixtures in `validate_test.go` to include `Command: "echo test"` where needed. The `DefaultPipelineYAML` validation will also fail — add a special case: validation skips command check for nodes when used with `DefaultPipelineYAML` (backward compat), OR update the default pipeline to include commands. For now, make `command` required only when not using the default pipeline — add the check only in `Validate()` when explicitly called, not in `ClimbWorkflow`.

Actually, simpler: make `command` optional in validation for now. The `ExecSpawner` itself will error if command is empty at spawn time. This avoids breaking the default pipeline while we migrate.

```go
// In validate.go — do NOT add command-required validation yet.
// ExecSpawner validates at spawn time instead.
```

Revert the validation addition from step 4. The command-required check lives in ExecSpawner.Spawn(), not in Validate().

- [ ] **Step 9: Commit**

```bash
git add internal/v3/pipeline/model.go internal/v3/pipeline/parser_test.go
git commit -m "feat(pipeline): add Command field to NodeConfig"
```

---

### Task 2: Introduce `internalDir` helper and migrate paths

**Files:**
- Create: `internal/v3/session/paths.go`
- Modify: `internal/v3/temporal/activity.go`
- Modify: `internal/v3/cli/node_complete.go`

- [ ] **Step 1: Write the paths helper**

Create `internal/v3/session/paths.go`:

```go
package session

import (
	"fmt"
	"path/filepath"
)

// InternalDir returns the path to the gitignored runtime state directory.
func InternalDir(workDir string) string {
	return filepath.Join(workDir, ".belayer", ".internal")
}

// CompletionDir returns the path to the completion files directory.
func CompletionDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "completion")
}

// CompletionFilePath returns the attempt-scoped completion file path.
func CompletionFilePath(workDir, taskID, nodeName string, attempt int) string {
	filename := fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt)
	return filepath.Join(CompletionDir(workDir), filename)
}

// InputDir returns the path to the node input directory.
func InputDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "input")
}

// OutputDir returns the path to the node output directory.
func OutputDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "output")
}
```

- [ ] **Step 2: Write tests for paths helper**

Create `internal/v3/session/paths_test.go`:

```go
package session

import (
	"testing"
)

func TestInternalDir(t *testing.T) {
	got := InternalDir("/tmp/work")
	want := "/tmp/work/.belayer/.internal"
	if got != want {
		t.Errorf("InternalDir = %q, want %q", got, want)
	}
}

func TestCompletionFilePath(t *testing.T) {
	got := CompletionFilePath("/tmp/work", "climb-123", "lead", 2)
	want := "/tmp/work/.belayer/.internal/completion/climb-123-lead-attempt-2.json"
	if got != want {
		t.Errorf("CompletionFilePath = %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: Run path tests**

Run: `go test ./internal/v3/session/ -run TestInternalDir -v && go test ./internal/v3/session/ -run TestCompletionFilePath -v`
Expected: PASS

- [ ] **Step 4: Migrate activity.go paths**

In `internal/v3/temporal/activity.go`, update `readCompletionFile`:

```go
func readCompletionFile(workDir, taskID, nodeName string, attempt int) (model.CompletionResult, error) {
	path := session.CompletionFilePath(workDir, taskID, nodeName, attempt)
	// ... rest unchanged
}
```

Update `cleanStaleCompletionFiles`:

```go
func cleanStaleCompletionFiles(workDir, taskID, nodeName string, currentAttempt int) {
	for i := 0; i < currentAttempt; i++ {
		path := session.CompletionFilePath(workDir, taskID, nodeName, i)
		_ = os.Remove(path)
	}
}
```

Update `materializeCodeInput`:

```go
func materializeCodeInput(workDir string) error {
	inputDir := session.InputDir(workDir)
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return fmt.Errorf("create input dir: %w", err)
	}
	// ... rest unchanged, but write to inputDir instead of filepath.Join(workDir, ".belayer", "input")
}
```

Update `WriteFeedbackActivity`:

```go
func (a *Activities) WriteFeedbackActivity(ctx context.Context, input WriteFeedbackInput) (string, error) {
	feedbackPath := filepath.Join(session.InputDir(input.WorkDir), "feedback.md")
	// ... rest unchanged
	return ".belayer/.internal/input/feedback.md", nil
}
```

Update `processGateResult` default paths:

```go
if resultPath == "" {
	resultPath = ".belayer/.internal/output/gate-result.json"
}
// ...
if rationalePath == "" {
	rationalePath = ".belayer/.internal/output/rationale.md"
}
```

- [ ] **Step 5: Migrate node_complete.go paths**

In `internal/v3/cli/node_complete.go`:

Replace `completionFilePath`:

```go
func completionFilePath(workDir, taskID, nodeName string, attempt int) string {
	return session.CompletionFilePath(workDir, taskID, nodeName, attempt)
}
```

Add `BELAYER_WORK_DIR` preference:

```go
// Inside RunE, replace:
//   workDir, err := os.Getwd()
// with:
workDir := os.Getenv("BELAYER_WORK_DIR")
if workDir == "" {
	var err error
	workDir, err = os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/v3/... -v`
Expected: Some tests may fail due to path changes in integration tests (completion files now under `.internal/`). Fix in Task 9.

- [ ] **Step 7: Commit**

```bash
git add internal/v3/session/paths.go internal/v3/session/paths_test.go internal/v3/temporal/activity.go internal/v3/cli/node_complete.go
git commit -m "refactor: introduce internalDir helper, migrate runtime paths to .belayer/.internal/"
```

---

### Task 3: Update Spawner interface and implement ExecSpawner

**Files:**
- Modify: `internal/v3/session/spawner.go`
- Delete: `internal/v3/session/hooks.go`
- Delete: `internal/v3/session/hooks_test.go`
- Create: `internal/v3/session/exec_spawner.go`
- Create: `internal/v3/session/exec_spawner_test.go`
- Modify: `internal/v3/session/spawner_test.go`

- [ ] **Step 1: Update SpawnOpts and Spawner interface**

In `internal/v3/session/spawner.go`, replace the entire file:

```go
package session

import (
	"context"
	"fmt"
)

// SpawnOpts holds the parameters needed to spawn a session for a pipeline node.
type SpawnOpts struct {
	NodeName    string
	TaskID      string
	Attempt     int
	WorkDir     string
	Description string
	Command     string
	InputPrompt string
}

// WindowName returns a display name: "{NodeName}-{TaskID[:8]}".
func (o SpawnOpts) WindowName() string {
	id := o.TaskID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("%s-%s", o.NodeName, id)
}

// Spawner launches sessions for pipeline nodes.
// Returns a channel that receives an error if the spawned process exits non-zero
// before writing a completion file. Returns nil channel if exit monitoring is not supported.
type Spawner interface {
	Spawn(ctx context.Context, opts SpawnOpts) (<-chan error, error)
}
```

- [ ] **Step 2: Delete hooks.go and hooks_test.go**

```bash
rm internal/v3/session/hooks.go internal/v3/session/hooks_test.go
```

These move to the claude-tmux framework (Task 8).

- [ ] **Step 3: Update spawner_test.go**

Replace `internal/v3/session/spawner_test.go` — remove all `buildClaudeCommand`, `buildEnvExports`, and `HooksPath` tests. Keep only `WindowName` tests:

```go
package session

import "testing"

func TestWindowName_TruncatesTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "reviewer", TaskID: "abcdef1234567890"}
	got := opts.WindowName()
	want := "reviewer-abcdef12"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}

func TestWindowName_ShortTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "planner", TaskID: "abc"}
	got := opts.WindowName()
	want := "planner-abc"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}
```

- [ ] **Step 4: Write ExecSpawner with tests**

Create `internal/v3/session/exec_spawner.go`:

```go
package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// ExecSpawner implements Spawner by executing a shell command.
type ExecSpawner struct{}

// Spawn executes opts.Command via sh -c in the background. It sets BELAYER_*
// environment variables and returns a channel that fires if the process exits
// non-zero before a completion file is written.
func (e *ExecSpawner) Spawn(_ context.Context, opts SpawnOpts) (<-chan error, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("node %q: command is empty", opts.NodeName)
	}

	cmd := exec.Command("sh", "-c", opts.Command)
	cmd.Dir = opts.WorkDir
	cmd.Env = append(os.Environ(),
		"BELAYER_TASK_ID="+opts.TaskID,
		"BELAYER_NODE="+opts.NodeName,
		"BELAYER_ATTEMPT="+strconv.Itoa(opts.Attempt),
		"BELAYER_WORK_DIR="+opts.WorkDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command for node %q: %w", opts.NodeName, err)
	}

	exitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			exitCh <- fmt.Errorf("node %q command exited: %w", opts.NodeName, err)
		}
		close(exitCh)
	}()

	return exitCh, nil
}
```

Create `internal/v3/session/exec_spawner_test.go`:

```go
package session

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestExecSpawner_SpawnSuccess(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "true",
		WorkDir:  os.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	if exitCh == nil {
		t.Fatal("expected non-nil exit channel")
	}
	// Wait for process to exit cleanly.
	select {
	case err := <-exitCh:
		if err != nil {
			t.Fatalf("expected nil error from exit channel, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_EmptyCommand(t *testing.T) {
	spawner := &ExecSpawner{}
	_, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "",
		WorkDir:  os.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecSpawner_CommandNotFound(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "nonexistent_command_belayer_test_12345",
		WorkDir:  os.TempDir(),
	})
	// sh -c starts but the command fails — error comes via exitCh
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case err := <-exitCh:
		if err == nil {
			t.Fatal("expected error from exit channel for bad command")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_NonZeroExit(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "exit 1",
		WorkDir:  os.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case err := <-exitCh:
		if err == nil {
			t.Fatal("expected error from exit channel for exit 1")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_EnvVars(t *testing.T) {
	dir := t.TempDir()
	outFile := dir + "/env.txt"
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "mynode",
		TaskID:   "task-42",
		Attempt:  3,
		Command:  "env > " + outFile,
		WorkDir:  dir,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	<-exitCh
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read env output: %v", err)
	}
	env := string(data)
	for _, want := range []string{"BELAYER_TASK_ID=task-42", "BELAYER_NODE=mynode", "BELAYER_ATTEMPT=3", "BELAYER_WORK_DIR=" + dir} {
		if !contains(env, want) {
			t.Errorf("missing env var %q in output", want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/v3/session/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/v3/session/
git commit -m "feat(session): replace TmuxSpawner with ExecSpawner, update Spawner interface"
```

---

### Task 4: Update NodeActivity to write node-context.json and use new Spawner

**Files:**
- Modify: `internal/v3/temporal/activity.go`

- [ ] **Step 1: Add NodeContext type and writeNodeContext function**

In `internal/v3/temporal/activity.go`, add:

```go
// NodeContext is the typed contract between belayer core and framework implementations.
type NodeContext struct {
	TaskID      string            `json:"task_id"`
	NodeName    string            `json:"node_name"`
	NodeType    string            `json:"node_type"`
	Attempt     int               `json:"attempt"`
	WorkDir     string            `json:"work_dir"`
	Description string            `json:"description"`
	InputPrompt string            `json:"input_prompt"`
	Artifacts   map[string]string `json:"artifacts"`
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
```

- [ ] **Step 2: Update NodeActivity to use new flow**

Replace the NodeActivity method body. Remove the `WriteHooksConfig` call, add `writeNodeContext`, update `Spawn` to handle the exit channel:

```go
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
```

- [ ] **Step 3: Update pollForCompletion to accept exit channel**

```go
func pollForCompletion(ctx context.Context, workDir, taskID, nodeName string, attempt int, interval time.Duration, exitCh <-chan error) (model.CompletionResult, error) {
	// Check immediately before the first tick.
	if result, err := readCompletionFile(workDir, taskID, nodeName, attempt); err == nil {
		return result, nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return model.CompletionResult{}, ctx.Err()
		case err := <-exitCh:
			// Process exited. Check one more time for completion file
			// (process may have written it just before exiting).
			if result, readErr := readCompletionFile(workDir, taskID, nodeName, attempt); readErr == nil {
				return result, nil
			}
			if err != nil {
				return model.CompletionResult{}, fmt.Errorf("node %q process exited without completion file: %w", nodeName, err)
			}
			return model.CompletionResult{}, fmt.Errorf("node %q process exited without writing completion file", nodeName)
		case <-ticker.C:
			recordHeartbeat(ctx, fmt.Sprintf("polling for %s attempt %d", nodeName, attempt))
			result, err := readCompletionFile(workDir, taskID, nodeName, attempt)
			if err == nil {
				return result, nil
			}
		}
		// If exitCh is nil (e.g., fakeSpawner), don't block on it.
		// The nil channel case is never selected, so polling continues normally.
	}
}
```

Note: a nil channel never fires in a select, so fakeSpawner returning `nil` for exitCh means the select only considers ctx.Done and ticker.C — identical to the old behavior.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/v3/temporal/ -v`
Expected: Compilation errors in integration tests — fakeSpawner doesn't match new Spawner interface. Fix in Task 9.

- [ ] **Step 5: Commit**

```bash
git add internal/v3/temporal/activity.go
git commit -m "feat(activity): write node-context.json, integrate exit channel in polling"
```

---

### Task 5: Update node-complete to prefer BELAYER_WORK_DIR

**Files:**
- Modify: `internal/v3/cli/node_complete.go`

Already done in Task 2 Step 5. This task is a no-op if Task 2 was completed. Verify the change is present:

- [ ] **Step 1: Verify BELAYER_WORK_DIR preference is in node_complete.go**

The code should read `BELAYER_WORK_DIR` first, fall back to `os.Getwd()`.

- [ ] **Step 2: Commit if not already committed**

Already committed in Task 2.

---

### Task 6: Wire ExecSpawner into CLI commands

**Files:**
- Modify: `internal/v3/cli/climb.go:70-73`
- Modify: `internal/v3/cli/worker.go:60-63`

- [ ] **Step 1: Update climb.go**

Replace the TmuxSpawner wiring:

```go
// Remove:
//   tm := tmux.NewRealTmux()
//   spawner := session.NewTmuxSpawner(tm)
// Replace with:
spawner := &session.ExecSpawner{}
```

Remove the `tmux` import.

- [ ] **Step 2: Update worker.go**

Same change:

```go
// Remove:
//   tm := tmux.NewRealTmux()
//   spawner := session.NewTmuxSpawner(tm)
// Replace with:
spawner := &session.ExecSpawner{}
```

Remove the `tmux` import.

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/belayer`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/v3/cli/climb.go internal/v3/cli/worker.go
git commit -m "refactor(cli): wire ExecSpawner instead of TmuxSpawner"
```

---

### Task 7: Create framework embed infrastructure and `belayer setup` command

**Files:**
- Create: `frameworks/claude-tmux/` (placeholder — filled in Task 8)
- Create: `internal/v3/frameworks/embed.go`
- Create: `internal/v3/cli/setup.go`
- Create: `internal/v3/cli/setup_test.go`
- Modify: `internal/v3/cli/root.go`

- [ ] **Step 1: Create framework embed package**

Create `internal/v3/frameworks/embed.go`:

```go
package frameworks

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:claude-tmux
var builtinFS embed.FS

// List returns the names of all built-in frameworks.
func List() ([]string, error) {
	entries, err := fs.ReadDir(builtinFS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Install copies a framework into the target directory.
// source is either a built-in name or a filesystem path.
func Install(source, targetDir string, force bool) error {
	var srcFS fs.FS

	// Check if source is a local path.
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		srcFS = os.DirFS(source)
	} else {
		// Try as built-in name.
		sub, err := fs.Sub(builtinFS, source)
		if err != nil {
			return fmt.Errorf("unknown framework %q (not a path or built-in name)", source)
		}
		srcFS = sub
	}

	// Validate: source must contain pipeline.yaml.
	if _, err := fs.Stat(srcFS, "pipeline.yaml"); err != nil {
		return fmt.Errorf("framework %q missing required pipeline.yaml", source)
	}

	// Check for existing pipeline.yaml in target.
	targetPipeline := filepath.Join(targetDir, "pipeline.yaml")
	if _, err := os.Stat(targetPipeline); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", targetPipeline)
	}

	// Copy all files from source to target.
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(srcFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// EnsureInternalDir creates .belayer/.internal/ with a .gitignore.
func EnsureInternalDir(workDir string) error {
	internalDir := filepath.Join(workDir, ".belayer", ".internal")
	if err := os.MkdirAll(internalDir, 0o755); err != nil {
		return err
	}
	gitignorePath := filepath.Join(internalDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		return os.WriteFile(gitignorePath, []byte("*\n"), 0o644)
	}
	return nil
}
```

Note: the `//go:embed` directive requires the `frameworks/claude-tmux/` directory to exist with at least one file. We'll create the placeholder in Task 8 before this compiles.

- [ ] **Step 2: Create setup command**

Create `internal/v3/cli/setup.go`:

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/v3/frameworks"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var frameworkFlag string
	var force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Scaffold a belayer framework into the current repo",
		Long: `Install a belayer framework into .belayer/ of the current repository.

Frameworks provide pipeline.yaml and node runner scripts that define
how belayer executes pipeline nodes.

Examples:
  belayer setup --framework claude-tmux           # built-in framework
  belayer setup --framework ./my-custom-framework # local path`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if frameworkFlag == "" {
				return fmt.Errorf("--framework is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			targetDir := filepath.Join(cwd, ".belayer")
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("create .belayer directory: %w", err)
			}

			if err := frameworks.Install(frameworkFlag, targetDir, force); err != nil {
				return err
			}

			if err := frameworks.EnsureInternalDir(cwd); err != nil {
				return fmt.Errorf("create .internal directory: %w", err)
			}

			// Make scripts executable.
			scriptsDir := filepath.Join(targetDir, "scripts")
			if entries, err := os.ReadDir(scriptsDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						os.Chmod(filepath.Join(scriptsDir, e.Name()), 0o755)
					}
				}
			}

			fmt.Printf("Framework %q installed to .belayer/\n", frameworkFlag)
			fmt.Println("Customize .belayer/pipeline.yaml, then run: belayer climb")
			return nil
		},
	}

	cmd.Flags().StringVar(&frameworkFlag, "framework", "", "Framework name (built-in) or path (local directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .belayer/pipeline.yaml")

	return cmd
}
```

- [ ] **Step 3: Register setup command in root.go**

In `internal/v3/cli/root.go`:

```go
func RegisterV3Commands(root *cobra.Command) {
	root.AddCommand(
		NewClimbCmd(),
		NewNodeCompleteCmd(),
		newStatusCmd(),
		newWorkerCmd(),
		newStartCmd(),
		newSetupCmd(),
	)
}
```

- [ ] **Step 4: Write setup test**

Create `internal/v3/cli/setup_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/frameworks"
)

func TestSetup_BuiltinFramework(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)

	err := frameworks.Install("claude-tmux", targetDir, false)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	// Verify pipeline.yaml was copied.
	if _, err := os.Stat(filepath.Join(targetDir, "pipeline.yaml")); err != nil {
		t.Error("pipeline.yaml not found after install")
	}
}

func TestSetup_ExistingPipelineNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)
	os.WriteFile(filepath.Join(targetDir, "pipeline.yaml"), []byte("existing"), 0o644)

	err := frameworks.Install("claude-tmux", targetDir, false)
	if err == nil {
		t.Fatal("expected error for existing pipeline.yaml without --force")
	}
}

func TestSetup_ExistingPipelineWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)
	os.WriteFile(filepath.Join(targetDir, "pipeline.yaml"), []byte("old"), 0o644)

	err := frameworks.Install("claude-tmux", targetDir, true)
	if err != nil {
		t.Fatalf("Install with force returned error: %v", err)
	}
}

func TestSetup_UnknownFramework(t *testing.T) {
	tmpDir := t.TempDir()
	err := frameworks.Install("nonexistent", tmpDir, false)
	if err == nil {
		t.Fatal("expected error for unknown framework")
	}
}
```

- [ ] **Step 5: Commit** (after Task 8 creates the framework files so embed compiles)

This task and Task 8 should be committed together since `//go:embed` requires the directory.

---

### Task 8: Create claude-tmux framework

**Files:**
- Create: `frameworks/claude-tmux/framework.yaml`
- Create: `frameworks/claude-tmux/pipeline.yaml`
- Create: `frameworks/claude-tmux/scripts/run-node.sh`
- Create: `frameworks/claude-tmux/scripts/run-gate.sh`
- Create: `frameworks/claude-tmux/README.md`

- [ ] **Step 1: Create framework.yaml**

Create `frameworks/claude-tmux/framework.yaml`:

```yaml
name: claude-tmux
description: Interactive Claude Code sessions in tmux windows
version: 0.1.0
```

- [ ] **Step 2: Create pipeline.yaml**

Create `frameworks/claude-tmux/pipeline.yaml`:

```yaml
name: claude-tmux
intake:
  - name: user-session
    type: interactive
nodes:
  - name: implement
    type: node
    command: .belayer/scripts/run-node.sh
    description: |
      You are the implementer. You receive a design document and implement
      the feature. Write tests. Commit your changes when done.
    input:
      type: file
      key: design_doc
    output:
      type: commit
    on_pass: review
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: review
    type: gate
    command: .belayer/scripts/run-gate.sh
    description: |
      You are an adversarial code reviewer. Review the code changes for
      spec compliance, test coverage, and runtime correctness.

      For each dimension, provide a score from 0-10 with rationale.
      Be honest. Gaming the score helps no one.
    input:
      type: commit
    dimensions:
      - name: spec_compliance
        description: "Do the changes match what was specified?"
        weight: 0.35
      - name: test_contracts
        description: "Are tests meaningful and sufficient?"
        weight: 0.3
      - name: runtime_correctness
        description: "Would this work in production?"
        weight: 0.35
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
    on_pass: stop
    on_retry: implement
    on_fail: stop
    max_retries: 2

safety:
  max_concurrent_runs: 3
```

- [ ] **Step 3: Create run-node.sh**

Create `frameworks/claude-tmux/scripts/run-node.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Belayer claude-tmux framework: node runner
# Reads context from env vars + node-context.json, opens Claude in tmux.

TASK_ID="${BELAYER_TASK_ID:?}"
NODE="${BELAYER_NODE:?}"
ATTEMPT="${BELAYER_ATTEMPT:?}"
WORK_DIR="${BELAYER_WORK_DIR:?}"

CONTEXT_FILE="$WORK_DIR/.belayer/.internal/input/node-context.json"
DESCRIPTION=$(jq -r '.description' "$CONTEXT_FILE")
INPUT_PROMPT=$(jq -r '.input_prompt' "$CONTEXT_FILE")

# Write Claude Code Stop hook to call belayer node-complete.
HOOKS_DIR="$WORK_DIR/.belayer/.internal"
mkdir -p "$HOOKS_DIR"
HOOK_CMD="belayer node-complete --task-id ${TASK_ID} --node ${NODE} --attempt ${ATTEMPT}"
jq -n --arg cmd "$HOOK_CMD" '{
  hooks: {
    Stop: [{ hooks: [{ type: "command", command: $cmd }] }]
  }
}' > "$HOOKS_DIR/hooks.json"

# Ensure tmux session exists.
SESSION="belayer-v3"
tmux has-session -t "$SESSION" 2>/dev/null || tmux new-session -d -s "$SESSION"

# Create window and launch Claude.
WINDOW="${NODE}-${TASK_ID:0:8}"
tmux new-window -t "$SESSION" -n "$WINDOW"
tmux send-keys -t "$SESSION:$WINDOW" \
  "cd $(printf '%q' "$WORK_DIR") && claude --dangerously-skip-permissions --settings $(printf '%q' "$HOOKS_DIR/hooks.json") $(printf '%q' "$DESCRIPTION") $(printf '%q' "$INPUT_PROMPT")" Enter
```

- [ ] **Step 4: Create run-gate.sh**

Create `frameworks/claude-tmux/scripts/run-gate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Belayer claude-tmux framework: gate runner
# Same as run-node.sh but ensures gate output directory exists.

TASK_ID="${BELAYER_TASK_ID:?}"
NODE="${BELAYER_NODE:?}"
ATTEMPT="${BELAYER_ATTEMPT:?}"
WORK_DIR="${BELAYER_WORK_DIR:?}"

CONTEXT_FILE="$WORK_DIR/.belayer/.internal/input/node-context.json"
DESCRIPTION=$(jq -r '.description' "$CONTEXT_FILE")
INPUT_PROMPT=$(jq -r '.input_prompt' "$CONTEXT_FILE")

# Ensure output directory exists for gate results.
mkdir -p "$WORK_DIR/.belayer/.internal/output"

# Write Claude Code Stop hook.
HOOKS_DIR="$WORK_DIR/.belayer/.internal"
mkdir -p "$HOOKS_DIR"
HOOK_CMD="belayer node-complete --task-id ${TASK_ID} --node ${NODE} --attempt ${ATTEMPT}"
jq -n --arg cmd "$HOOK_CMD" '{
  hooks: {
    Stop: [{ hooks: [{ type: "command", command: $cmd }] }]
  }
}' > "$HOOKS_DIR/hooks.json"

SESSION="belayer-v3"
tmux has-session -t "$SESSION" 2>/dev/null || tmux new-session -d -s "$SESSION"

WINDOW="${NODE}-${TASK_ID:0:8}"
tmux new-window -t "$SESSION" -n "$WINDOW"
tmux send-keys -t "$SESSION:$WINDOW" \
  "cd $(printf '%q' "$WORK_DIR") && claude --dangerously-skip-permissions --settings $(printf '%q' "$HOOKS_DIR/hooks.json") $(printf '%q' "$DESCRIPTION") $(printf '%q' "$INPUT_PROMPT")" Enter
```

- [ ] **Step 5: Create README.md**

Create `frameworks/claude-tmux/README.md`:

```markdown
# claude-tmux Framework

Interactive Claude Code sessions in tmux windows. Each pipeline node opens a new tmux window running Claude Code with the node's description as a system prompt.

## Prerequisites

- `tmux` — terminal multiplexer
- `claude` — Claude Code CLI (authenticated)
- `jq` — JSON processor
- `belayer` — on PATH (for the Stop hook)

## How it works

1. `belayer climb` starts a pipeline run
2. For each node, belayer writes `node-context.json` and execs `run-node.sh` (or `run-gate.sh`)
3. The script reads context, configures a Claude Code Stop hook, and opens a tmux window
4. Claude does its work and exits
5. The Stop hook calls `belayer node-complete`, which writes a completion file
6. Belayer detects the completion file and routes to the next node

## Customization

- Edit `pipeline.yaml` to change node descriptions, add/remove nodes, adjust gate thresholds
- Edit scripts to change how Claude is invoked (different flags, different agents)
- Create new scripts for different node types
```

- [ ] **Step 6: Make scripts executable and commit with Task 7**

```bash
chmod +x frameworks/claude-tmux/scripts/run-node.sh frameworks/claude-tmux/scripts/run-gate.sh
git add frameworks/ internal/v3/frameworks/ internal/v3/cli/setup.go internal/v3/cli/setup_test.go internal/v3/cli/root.go
git commit -m "feat: add framework model, claude-tmux framework, and belayer setup command"
```

---

### Task 9: Update integration tests for new interface

**Files:**
- Modify: `internal/v3/temporal/integration_test.go`

- [ ] **Step 1: Update fakeSpawner to match new Spawner interface**

```go
type fakeSpawner struct {
	workDir string
	results map[string]model.CompletionResult
}

func (f *fakeSpawner) Spawn(_ context.Context, opts session.SpawnOpts) (<-chan error, error) {
	result, ok := f.results[opts.NodeName]
	if !ok {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}
	result.Attempt = opts.Attempt
	completionDir := session.CompletionDir(f.workDir)
	os.MkdirAll(completionDir, 0o755)
	path := session.CompletionFilePath(f.workDir, opts.TaskID, opts.NodeName, opts.Attempt)
	data, _ := json.Marshal(result)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return nil, nil // nil channel — polling works as before
}
```

- [ ] **Step 2: Update retryThenPassSpawner**

```go
type retryThenPassSpawner struct {
	workDir      string
	spotterCalls *int
}

func (r *retryThenPassSpawner) Spawn(_ context.Context, opts session.SpawnOpts) (<-chan error, error) {
	// ... same body as before but:
	// - use session.CompletionDir / session.CompletionFilePath for completion paths
	// - use session.OutputDir for gate output paths
	// - return nil, nil instead of just nil
	// Gate output files go to .belayer/.internal/output/ instead of .belayer/output/
	var result model.CompletionResult

	if opts.NodeName == "spotter" {
		*r.spotterCalls++
		outputDir := session.OutputDir(r.workDir)
		os.MkdirAll(outputDir, 0o755)
		// ... same gate file writing but to outputDir ...
	}

	result.Attempt = opts.Attempt
	completionDir := session.CompletionDir(r.workDir)
	os.MkdirAll(completionDir, 0o755)
	path := session.CompletionFilePath(r.workDir, opts.TaskID, opts.NodeName, opts.Attempt)
	data, _ := json.Marshal(result)
	os.WriteFile(path, data, 0o644)
	return nil, nil
}
```

- [ ] **Step 3: Update test pipeline YAML to include command fields**

The integration tests provide pipeline YAML via `defaultInput()`. Add `command:` fields to the test YAML. Since `fakeSpawner` ignores the command (it writes completion files directly), the value doesn't matter:

```go
func defaultInput() model.ClimbInput {
	return model.ClimbInput{
		PipelineYAML: []byte(`name: test-pipeline
nodes:
  - name: setter
    type: node
    command: echo test
    description: plan
    input: { type: file, key: design_doc }
    output: { type: file, path: .belayer/.internal/output/plan.md }
    on_pass: next
    on_retry: setter
    on_fail: stop
    max_retries: 2
  - name: lead
    type: node
    command: echo test
    description: implement
    input: { type: file, key: setter }
    output: { type: commit }
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3
  - name: spotter
    type: gate
    command: echo test
    description: review
    input: { type: commit }
    dimensions:
      - { name: spec_compliance, weight: 0.35, description: match }
      - { name: test_contracts, weight: 0.3, description: tests }
      - { name: runtime_correctness, weight: 0.35, description: works }
    thresholds: { pass: 7.0, retry: 4.0 }
    output: { type: gate_result }
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
  - name: summit
    type: node
    command: echo test
    description: PR
    input: { type: gate_result, key: spotter }
    output: { type: pr }
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2
safety:
  max_concurrent_runs: 3
`),
		// ... rest unchanged
	}
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/v3/... -v`
Expected: ALL PASS

Run: `go test ./... -v` (full test suite)
Expected: ALL PASS (may have some v1 tests unrelated to this change)

- [ ] **Step 5: Commit**

```bash
git add internal/v3/temporal/integration_test.go
git commit -m "test: update integration tests for ExecSpawner interface and .internal/ paths"
```

---

## Deliverable Traceability

| Design Doc Deliverable | Plan Task |
|----------------------|-----------|
| ExecSpawner implementation | Task 3 |
| Command field in pipeline YAML | Task 1 |
| node-context.json protocol | Task 4 |
| Framework model + embed infrastructure | Task 7 |
| belayer setup command | Task 7 |
| .belayer/ directory structure (.internal/ split) | Task 2 |
| claude-tmux framework (pipeline, scripts, README) | Task 8 |
| Path migration (.belayer/ → .belayer/.internal/) | Task 2 |
| NodeActivity updates (remove hooks, add context) | Task 4 |
| node-complete BELAYER_WORK_DIR preference | Task 5 (via Task 2) |
| CLI wiring (climb.go, worker.go) | Task 6 |
| Integration test updates | Task 9 |
| Spawner interface exit channel | Task 3 |

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
