# Summit Node & Explorer Plugin Implementation Plan

> **Status**: Active | **Created**: 2026-03-23 | **Last Updated**: 2026-03-23
> **Design Doc**: `docs/design-docs/2026-03-23-summit-node-explorer-plugin-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-23 | Design | Summit is a `node` not a `gate` | PR creation is pass/fail, no scoring needed |
| 2026-03-23 | Design | New `pr` output type | Semantically distinct from file/commit/gate_result |
| 2026-03-23 | Design | `on_pass: stop` for summit | Terminal node — pipeline ends after successful PR |
| 2026-03-23 | Design | Explorer uses `commands/` not `skills/` | Matches pr plugin convention |

## Progress

- [ ] Task 1: Add `pr` output type to pipeline validation
- [ ] Task 2: Add summit node to default pipeline
- [ ] Task 3: Update agentassets for explorer plugin
- [ ] Task 4: Create explorer plugin
- [ ] Task 5: Register explorer in marketplace and init
- [ ] Task 6: Update existing tests for new node count

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Add `pr` output type to pipeline validation

**Goal:** Make `pr` a valid output type for non-gate nodes.

**Files:**
- Modify: `internal/v3/pipeline/validate.go`
- Modify: `internal/v3/pipeline/validate_test.go`

- [ ] **Step 1: Write failing test — `pr` output type accepted on node**

Add to `validate_test.go`:

```go
func TestValidate_PROutputType(t *testing.T) {
	cfg := validPipeline()
	cfg.Nodes[0].Output.Type = "pr"
	if err := Validate(cfg); err != nil {
		t.Errorf("expected pr output type to be valid, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/pipeline/ -run TestValidate_PROutputType -v`
Expected: FAIL — `pr` not in `validOutputTypes`

- [ ] **Step 3: Write failing test — `pr` output type rejected on gate**

Add to `validate_test.go`:

```go
func TestValidate_GateWithPROutputRejected(t *testing.T) {
	cfg := validGatePipeline()
	cfg.Nodes[1].Output.Type = "pr"
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for gate with pr output type")
	}
	if !strings.Contains(err.Error(), "gate_result") {
		t.Errorf("error should mention gate_result, got: %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/v3/pipeline/ -run TestValidate_GateWithPROutputRejected -v`
Expected: FAIL — error message won't mention `gate_result` (hits the unknown output type check first, since `pr` isn't in `validOutputTypes` yet)

- [ ] **Step 5: Implement — add `pr` to validOutputTypes and update error message**

In `validate.go`, update `validOutputTypes`:

```go
validOutputTypes := map[string]bool{
	"file":        true,
	"gate_result": true,
	"commit":      true,
	"pr":          true,
}
```

Update the error message on line 51:

```go
return fmt.Errorf("node %q: output.type must be \"file\", \"gate_result\", \"commit\", or \"pr\", got %q", n.Name, n.Output.Type)
```

The existing gate consistency check on line 54 already handles the gate rejection: gates must use `gate_result`, so `pr` on a gate will hit that error. The non-gate check on line 57 rejects `gate_result` on non-gates — `pr` won't trigger that since it's not `gate_result`. Both tests should pass without additional code.

- [ ] **Step 6: Run all validation tests**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/v3/pipeline/validate.go internal/v3/pipeline/validate_test.go
git commit -m "feat(pipeline): add pr output type to validation"
```

---

### Task 2: Add summit node to default pipeline

**Goal:** Append the summit node after spotter in `DefaultPipelineYAML`.

**Files:**
- Modify: `internal/v3/pipeline/defaults.go`
- Modify: `internal/v3/pipeline/defaults_test.go`

- [ ] **Step 1: Write failing test — summit node exists in default pipeline**

Add to `defaults_test.go`:

```go
func TestDefaultPipeline_SummitNode(t *testing.T) {
	cfg, err := ParsePipeline([]byte(DefaultPipelineYAML))
	if err != nil {
		t.Fatalf("parse default pipeline: %v", err)
	}

	summit := cfg.FindNode("summit")
	if summit == nil {
		t.Fatal("expected summit node in default pipeline")
	}
	if summit.Type != NodeTypeNode {
		t.Errorf("summit type: got %q, want %q", summit.Type, NodeTypeNode)
	}
	if summit.Output.Type != "pr" {
		t.Errorf("summit output type: got %q, want %q", summit.Output.Type, "pr")
	}
	if summit.OnPass != "stop" {
		t.Errorf("summit on_pass: got %q, want %q", summit.OnPass, "stop")
	}
	if summit.OnRetry != "self" {
		t.Errorf("summit on_retry: got %q, want %q", summit.OnRetry, "self")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/v3/pipeline/ -run TestDefaultPipeline_SummitNode -v`
Expected: FAIL — summit node not found

- [ ] **Step 3: Implement — append summit node to DefaultPipelineYAML**

In `defaults.go`, add the summit node after the spotter node (before the `safety:` block). Also update the comment to reflect the new pipeline shape:

```go
// DefaultPipelineYAML is the built-in setter → lead → spotter → summit pipeline.
```

The summit YAML block:

```yaml
  - name: summit
    type: node
    description: |
      You are the summit. The code has passed review. Your job is to create
      a pull request for the completed work.

      Run /pr:author to create the PR.
      After /pr:author completes, verify the PR exists with `gh pr view`.
      If the PR was created successfully, write the PR URL and number to
      .belayer/output/pr.json.

      On retry: check `gh pr view` first — if a PR already exists, skip
      /pr:author and write the output directly (idempotency).
    input:
      type: gate_result
      key: spotter
    output:
      type: pr
      path: .belayer/output/pr.json
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2
```

- [ ] **Step 4: Update node count test**

In `defaults_test.go`, update `TestDefaultPipelineParses`:

```go
if len(cfg.Nodes) != 4 {
    t.Errorf("Nodes: got %d, want 4", len(cfg.Nodes))
}
```

- [ ] **Step 5: Run all defaults + validation tests**

Run: `go test ./internal/v3/pipeline/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/v3/pipeline/defaults.go internal/v3/pipeline/defaults_test.go
git commit -m "feat(pipeline): add summit PR node to default pipeline"
```

---

### Task 3: Update agentassets for explorer plugin

**Goal:** Wire the explorer plugin into the Go embed FS and plugin loader.

**Files:**
- Modify: `agentassets.go`
- Modify: `agentassets_test.go`

- [ ] **Step 1: Write failing test — explorer plugin version discoverable**

Add to `agentassets_test.go`:

```go
func TestPluginVersion_Explorer(t *testing.T) {
	if got := MustPluginVersion("explorer"); got != "0.1.0" {
		t.Fatalf("unexpected explorer version: %s", got)
	}
}
```

- [ ] **Step 2: Write failing test — explorer send command in codex skills**

Add to `agentassets_test.go` inside `TestCodexSkillFiles_GeneratesCommandSkillsAndCopiesStaticSkills`, add to the `required` slice:

```go
"explorer-send/SKILL.md",
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test . -run TestPluginVersion_Explorer -v && go test . -run TestCodexSkillFiles -v`
Expected: FAIL — explorer not embedded

- [ ] **Step 4: Implement — update embed directive**

In `agentassets.go` line 15, add explorer paths to the embed directive:

```go
//go:embed plugins/harness/commands/*.md plugins/pr/commands/*.md plugins/explorer/commands/*.md plugins/harness/skills/strangler-fig all:plugins/harness/.claude-plugin/plugin.json all:plugins/pr/.claude-plugin/plugin.json all:plugins/explorer/.claude-plugin/plugin.json
```

- [ ] **Step 5: Implement — add explorer to plugin loader**

In `agentassets.go`, update `loadPlugins`:

```go
func loadPlugins() ([]PluginSpec, error) {
	pluginNames := []string{"harness", "pr", "explorer"}
```

- [ ] **Step 6: Implement — add explorer to CodexPackVersion**

In `agentassets.go`, update `CodexPackVersion`:

```go
func CodexPackVersion() string {
	return fmt.Sprintf("harness-%s_pr-%s_explorer-%s", MustPluginVersion("harness"), MustPluginVersion("pr"), MustPluginVersion("explorer"))
}
```

- [ ] **Step 7: Implement — add explorer command ref regex**

In `agentassets.go`, add regex and apply in `rewriteForCodex`:

```go
commandRefExplorer = regexp.MustCompile(`/explorer:([a-z-]+)`)
```

And in `rewriteForCodex`:

```go
rewritten = commandRefExplorer.ReplaceAllString(rewritten, "explorer-$1")
```

- [ ] **Step 8: Write test — explorer command rewrite in codex**

Add to `agentassets_test.go` in `TestCodexSkillFiles_RewritesRuntimeReferences`:

```go
send := string(files["explorer-send/SKILL.md"])
if strings.Contains(send, "/explorer:send") {
	t.Fatalf("expected /explorer:send to be rewritten for Codex")
}
```

**Note:** These tests will fail until Task 4 creates the actual plugin files. Run tests after Task 4 is complete.

- [ ] **Step 9: Commit (combined with Task 4)**

Commit happens after Task 4.

---

### Task 4: Create explorer plugin

**Goal:** Create the plugin directory, manifest, and send command.

**Files:**
- Create: `plugins/explorer/.claude-plugin/plugin.json`
- Create: `plugins/explorer/commands/send.md`

- [ ] **Step 1: Create plugin directory structure**

```bash
mkdir -p plugins/explorer/.claude-plugin plugins/explorer/commands
```

- [ ] **Step 2: Write plugin.json**

Create `plugins/explorer/.claude-plugin/plugin.json`:

```json
{
  "name": "explorer",
  "version": "0.1.0",
  "description": "Submit specs to the belayer pipeline from any interactive coding session.",
  "author": { "name": "donovanyohan" },
  "keywords": ["explorer", "submit", "spec", "pipeline", "intake"],
  "agents": [],
  "commands": ["./commands/send.md"],
  "skills": []
}
```

- [ ] **Step 3: Write send.md command**

Create `plugins/explorer/commands/send.md`:

```markdown
---
description: Use when a spec.md is ready for implementation and needs to be submitted to the belayer pipeline
---

# Send Spec to Pipeline

Submit a spec document to the belayer pipeline for autonomous implementation.

## Usage

` ` `
/explorer:send path/to/spec.md
` ` `

## Invocation

**IMMEDIATELY execute this workflow:**

1. **Parse arguments:** Extract the file path from the command arguments. If no path is provided, ask the user which spec file to submit.

2. **Validate the spec file:**
   - Check that the file exists using the Read tool
   - Check that the file is non-empty
   - If validation fails, report the error and stop

3. **Read the spec contents:** Read the full contents of the spec file.

4. **Submit to pipeline:** Call the belayer-channel MCP `submit` tool with:
   ` ` `json
   { "spec": "<full contents of the spec file>" }
   ` ` `

5. **Report result:** Relay the channel's response verbatim to the user. The response contains the workflow ID and pipeline name on success, or an error message if the worker is not reachable.

## Notes

- This command is a thin wrapper around the belayer-channel MCP `submit` tool
- No spec validation or linting is performed — belayer is pipes, the spec goes in as-is
- The belayer worker must be running (`belayer worker`) for submission to succeed
- If the worker is not running, the channel server returns a descriptive error
```

- [ ] **Step 4: Run agentassets tests**

Run: `go test . -v`
Expected: All PASS (including the new explorer tests from Task 3)

- [ ] **Step 5: Commit tasks 3 and 4 together**

```bash
git add agentassets.go agentassets_test.go plugins/explorer/
git commit -m "feat: add explorer plugin with /explorer:send command"
```

---

### Task 5: Register explorer in marketplace and init

**Goal:** Add explorer to marketplace.json and the init registration flow.

**Files:**
- Modify: `.claude-plugin/marketplace.json`
- Modify: `internal/plugins/registry.go`
- Modify: `internal/cli/init.go`

- [ ] **Step 1: Update marketplace.json**

Add explorer entry to the `plugins` array in `.claude-plugin/marketplace.json`:

```json
{
  "name": "explorer",
  "source": "./plugins/explorer",
  "description": "Submit specs to the belayer pipeline from any interactive session"
}
```

- [ ] **Step 2: Add ExplorerVersion constant**

In `internal/plugins/registry.go`, add to the constants:

```go
ExplorerVersion = "0.1.0"
```

- [ ] **Step 3: Register explorer in init**

In `internal/cli/init.go`, add to the `specs` slice:

```go
specs := []pluginSpec{
    {"harness", plugins.HarnessVersion},
    {"pr", plugins.PRVersion},
    {"explorer", plugins.ExplorerVersion},
}
```

Update the success message:

```go
fmt.Fprintf(cmd.OutOrStdout(), "Registered belayer marketplace. Installed plugins: harness, pr, explorer\n")
```

Update the warning message (line 46):

```go
"  The 'harness', 'pr', and 'explorer' Claude Code plugins were not installed.\n"+
```

- [ ] **Step 4: Run init tests**

Run: `go test ./internal/cli/ -v && go test ./internal/plugins/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add .claude-plugin/marketplace.json internal/plugins/registry.go internal/cli/init.go
git commit -m "feat: register explorer plugin in marketplace and init"
```

---

### Task 6: Final verification

**Goal:** Run full test suite and verify everything works together.

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 2: Build binary**

Run: `go build -o belayer ./cmd/belayer`
Expected: Build succeeds

- [ ] **Step 3: Commit (if any fixups needed)**

Only if test failures required changes.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
