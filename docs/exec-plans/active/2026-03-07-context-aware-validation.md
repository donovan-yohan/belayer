# Context-Aware Validation Pipeline Implementation Plan

> **Status**: Active | **Created**: 2026-03-07 | **Last Updated**: 2026-03-07
> **Design Doc**: `docs/plans/2026-03-07-context-aware-validation-design.md`
> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a per-goal spotter validation layer with LLM-driven project detection and browser-based checks, rename the current spotter to anchor, and extract all prompts/config into an editable config directory.

**Architecture:** The setter gains a new "spotting" state between lead completion and anchor review. A fresh Claude session reuses the lead's tmux window to run context-aware validation checks. All hardcoded prompts move to embedded `.md` files written to `~/.belayer/config/` on init. Validation profiles are human-readable TOML checklists the LLM interprets.

**Tech Stack:** Go 1.24, embed FS, BurntSushi/toml, existing tmux/spawner abstractions.

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-07 | Design | Context-aware validation (option D) | Different project types need different checks |
| 2026-03-07 | Design | Hybrid validation (option C) | Lead does basic self-check, spotter does runtime |
| 2026-03-07 | Design | Chrome DevTools MCP for browser checks | Already in toolchain, no deps in target project |
| 2026-03-07 | Design | LLM detects project type, not code | Rigid signal matching is worse than LLM judgment |
| 2026-03-07 | Design | Spotter reuses lead's tmux window | Simpler, fresh context via new agent session |
| 2026-03-07 | Design | Rename: spotter→anchor, validator→spotter | Climbing metaphors: spotter watches you, anchor ties lines together |
| 2026-03-07 | Design | Config: embedded defaults + editable overrides | Works out of box, customizable per instance |

## Progress

- [ ] Task 1: Embed default config files
- [ ] Task 2: Config loader with resolution chain
- [ ] Task 3: Rename spotter → anchor (model, store, events)
- [ ] Task 4: New goal status "spotting" and SPOT.json types
- [ ] Task 5: Spotter prompt template
- [ ] Task 6: Spotter validation in TaskRunner
- [ ] Task 7: Setter loop: spotting state handling
- [ ] Task 8: Lead prompt: inject spotter feedback on retry
- [ ] Task 9: `belayer init` writes config to disk
- [ ] Task 10: Wire config into setter/spawner

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Embed Default Config Files

Create the embedded config directory with default prompt templates and validation profiles.

**Files:**
- Create: `internal/defaults/defaults.go`
- Create: `internal/defaults/prompts/lead.md`
- Create: `internal/defaults/prompts/spotter.md`
- Create: `internal/defaults/prompts/anchor.md`
- Create: `internal/defaults/profiles/frontend.toml`
- Create: `internal/defaults/profiles/backend.toml`
- Create: `internal/defaults/profiles/cli.toml`
- Create: `internal/defaults/profiles/library.toml`
- Create: `internal/defaults/belayer.toml`
- Test: `internal/defaults/defaults_test.go`

**Step 1: Create default belayer.toml**

```toml
# internal/defaults/belayer.toml
[agents]
provider = "claude"
lead_model = "opus"
review_model = "sonnet"
permissions = "dangerously-skip"

[execution]
max_leads = 8
poll_interval = "5s"
stale_timeout = "30m"
max_retries = 3

[validation]
enabled = true
auto_detect_project = true
fallback_profile = "library"
browser_tool = "chrome-devtools"

[anchor]
enabled = true
max_attempts = 2
```

**Step 2: Move lead prompt to lead.md**

Extract the current `promptTemplate` from `internal/lead/prompt.go` into `internal/defaults/prompts/lead.md`. Keep the same Go template variables (`{{.Spec}}`, `{{.GoalID}}`, `{{.RepoName}}`, `{{.Description}}`). Add a new `{{.SpotterFeedback}}` variable for retry context (empty on first attempt).

**Step 3: Create spotter.md prompt**

New prompt template for the per-goal validator. Template variables: `{{.GoalID}}`, `{{.RepoName}}`, `{{.Description}}`, `{{.WorkDir}}`, `{{.ProfileContent}}`, `{{.DoneJSON}}`.

The prompt instructs the spotter to:
1. Examine the repo and determine project type
2. Read the matching validation profile
3. Execute each check in the profile
4. Use Chrome DevTools MCP for browser checks if frontend
5. Write SPOT.json with pass/fail and issues list

**Step 4: Move spotter prompt to anchor.md**

Extract the current `spotterTemplate` from `internal/spotter/prompt.go` into `internal/defaults/prompts/anchor.md`. Same template variables (`{{.Spec}}`, `{{.RepoDiffs}}`, `{{.Summaries}}`). Rename all references from "spotter" to "anchor" in the prompt text.

**Step 5: Create validation profile files**

Write the four `.toml` profile files (frontend, backend, cli, library) with human-readable check descriptions as specified in the design doc.

**Step 6: Create embed.go with //go:embed directive**

```go
// internal/defaults/defaults.go
package defaults

import "embed"

//go:embed belayer.toml prompts/*.md profiles/*.toml
var FS embed.FS
```

**Step 7: Write test that all embedded files are readable**

```go
// internal/defaults/defaults_test.go
func TestEmbeddedFilesExist(t *testing.T) {
    files := []string{
        "belayer.toml",
        "prompts/lead.md",
        "prompts/spotter.md",
        "prompts/anchor.md",
        "profiles/frontend.toml",
        "profiles/backend.toml",
        "profiles/cli.toml",
        "profiles/library.toml",
    }
    for _, f := range files {
        data, err := FS.ReadFile(f)
        require.NoError(t, err, "missing embedded file: %s", f)
        assert.NotEmpty(t, data, "empty embedded file: %s", f)
    }
}
```

**Step 8: Run test to verify**

Run: `go test ./internal/defaults/...`
Expected: PASS

**Step 9: Commit**

```bash
git add internal/defaults/
git commit -m "feat: embed default config, prompt templates, and validation profiles"
```

---

### Task 2: Config Loader with Resolution Chain

Build a config loader that resolves: instance config > global config > embedded defaults.

**Files:**
- Create: `internal/belayerconfig/config.go`
- Create: `internal/belayerconfig/config_test.go`

Using `belayerconfig` to avoid collision with existing `internal/config/` package.

**Step 1: Write failing test for config loading from embedded defaults**

```go
func TestLoadConfig_EmbeddedDefaults(t *testing.T) {
    cfg, err := belayerconfig.Load("", "")
    require.NoError(t, err)
    assert.Equal(t, "claude", cfg.Agents.Provider)
    assert.Equal(t, 8, cfg.Execution.MaxLeads)
    assert.True(t, cfg.Validation.Enabled)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/belayerconfig/... -run TestLoadConfig_EmbeddedDefaults`
Expected: FAIL — package doesn't exist

**Step 3: Implement config types and Load function**

```go
// internal/belayerconfig/config.go
package belayerconfig

type Config struct {
    Agents     AgentsConfig     `toml:"agents"`
    Execution  ExecutionConfig  `toml:"execution"`
    Validation ValidationConfig `toml:"validation"`
    Anchor     AnchorConfig     `toml:"anchor"`
}

type AgentsConfig struct {
    Provider    string `toml:"provider"`
    LeadModel   string `toml:"lead_model"`
    ReviewModel string `toml:"review_model"`
    Permissions string `toml:"permissions"`
}

type ExecutionConfig struct {
    MaxLeads     int    `toml:"max_leads"`
    PollInterval string `toml:"poll_interval"`
    StaleTimeout string `toml:"stale_timeout"`
    MaxRetries   int    `toml:"max_retries"`
}

type ValidationConfig struct {
    Enabled            bool   `toml:"enabled"`
    AutoDetectProject  bool   `toml:"auto_detect_project"`
    FallbackProfile    string `toml:"fallback_profile"`
    BrowserTool        string `toml:"browser_tool"`
}

type AnchorConfig struct {
    Enabled     bool `toml:"enabled"`
    MaxAttempts int  `toml:"max_attempts"`
}
```

`Load(globalDir, instanceDir string)` reads belayer.toml from embedded defaults, then overlays global, then instance. Empty string means skip that layer.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/belayerconfig/... -run TestLoadConfig_EmbeddedDefaults`
Expected: PASS

**Step 5: Write test for global override**

Test that a belayer.toml in globalDir overrides embedded defaults (e.g., max_leads = 4).

**Step 6: Write test for instance override**

Test that instanceDir belayer.toml overrides global (e.g., provider = "codex").

**Step 7: Add prompt and profile loading**

`LoadPrompt(globalDir, instanceDir, name string) (string, error)` — reads `prompts/{name}.md` with same resolution chain.

`LoadProfile(globalDir, instanceDir, name string) (string, error)` — reads `profiles/{name}.toml` with same resolution chain.

**Step 8: Write tests for prompt/profile loading**

Verify embedded defaults load, global overrides work, instance overrides work.

**Step 9: Run all tests**

Run: `go test ./internal/belayerconfig/...`
Expected: PASS

**Step 10: Commit**

```bash
git add internal/belayerconfig/
git commit -m "feat: config loader with embedded > global > instance resolution"
```

---

### Task 3: Rename Spotter → Anchor

Rename the existing cross-repo reviewer from "spotter" to "anchor" throughout the codebase.

**Files:**
- Rename: `internal/spotter/` → `internal/anchor/`
- Modify: `internal/model/types.go` — rename event types
- Modify: `internal/store/store.go` — rename methods and table references
- Modify: `internal/setter/taskrunner.go` — rename spotter methods to anchor
- Modify: `internal/setter/setter.go` — rename spotter references
- Modify: all test files in affected packages

**Step 1: Rename the spotter package directory**

```bash
mv internal/spotter internal/anchor
```

**Step 2: Update package name in all files under internal/anchor/**

Change `package spotter` to `package anchor` in:
- `internal/anchor/prompt.go`
- `internal/anchor/prompt_test.go`
- `internal/anchor/verdict.go`

Rename types:
- `SpotterPromptData` → `AnchorPromptData`
- `BuildSpotterPrompt` → `BuildAnchorPrompt`
- `spotterTemplate` → `anchorTemplate`

Update the prompt text itself: replace "spotter agent" with "anchor agent" in the template string.

**Step 3: Update model/types.go**

Rename `EventSpotterSpawned` → `EventAnchorSpawned`. Keep `SpotterReview` type as-is for now (DB table is `spotter_reviews` — renaming the table is risky, type name can be updated later).

**Step 4: Update store/store.go**

Rename methods:
- `InsertSpotterReview` → `InsertAnchorReview`
- `GetSpotterReviewsForTask` → `GetAnchorReviewsForTask`

Keep the underlying SQL queries hitting `spotter_reviews` table unchanged (no schema migration needed).

**Step 5: Update setter/taskrunner.go**

Rename fields and methods:
- `spotterAttempt` → `anchorAttempt`
- `spotterRunning` → `anchorRunning`
- `SpawnSpotter()` → `SpawnAnchor()`
- `CheckSpotterVerdict()` → `CheckAnchorVerdict()`
- `SpotterRunning()` → `AnchorRunning()`
- `SpotterAttempt()` → `AnchorAttempt()`

**Step 6: Update setter/setter.go**

Update all references from spotter to anchor in the tick() loop and log messages.

**Step 7: Update all import paths**

Replace `"github.com/donovan-yohan/belayer/internal/spotter"` with `"github.com/donovan-yohan/belayer/internal/anchor"` in all files.

**Step 8: Update all tests**

Update test files in `internal/anchor/`, `internal/setter/`, `internal/store/` to use new names.

**Step 9: Run all tests**

Run: `go test ./...`
Expected: PASS — all existing tests pass with renamed types

**Step 10: Commit**

```bash
git add -A
git commit -m "refactor: rename spotter to anchor for cross-repo reviewer"
```

---

### Task 4: New Goal Status "spotting" and SPOT.json Types

Add the "spotting" goal status and define the SPOT.json data structure.

**Files:**
- Modify: `internal/model/types.go`
- Create: `internal/spotter/types.go` (new spotter package for the validator)
- Test: `internal/spotter/types_test.go`

**Step 1: Write failing test for new GoalStatus**

```go
func TestGoalStatusSpotting(t *testing.T) {
    assert.Equal(t, GoalStatus("spotting"), GoalStatusSpotting)
}
```

**Step 2: Add GoalStatusSpotting to model/types.go**

```go
const (
    GoalStatusPending  GoalStatus = "pending"
    GoalStatusRunning  GoalStatus = "running"
    GoalStatusSpotting GoalStatus = "spotting"  // new
    GoalStatusComplete GoalStatus = "complete"
    GoalStatusFailed   GoalStatus = "failed"
)
```

Add new event types:
```go
EventSpotterSpawned  EventType = "spotter_spawned"   // per-goal spotter
EventSpotterVerdict  EventType = "spotter_verdict"    // per-goal verdict
```

**Step 3: Create spotter types**

```go
// internal/spotter/types.go
package spotter

// SpotJSON is the structured output from a spotter validation run.
type SpotJSON struct {
    Pass        bool     `json:"pass"`
    ProjectType string   `json:"project_type"`
    Issues      []Issue  `json:"issues"`
    Screenshots []string `json:"screenshots,omitempty"`
}

// Issue describes a single validation failure.
type Issue struct {
    Check       string `json:"check"`
    Description string `json:"description"`
    Severity    string `json:"severity"` // "error" | "warning"
}
```

**Step 4: Write test for SpotJSON parsing**

```go
func TestParseSpotJSON(t *testing.T) {
    raw := `{"pass": false, "project_type": "frontend", "issues": [{"check": "visual_quality", "description": "Text not wrapping", "severity": "error"}]}`
    var spot SpotJSON
    err := json.Unmarshal([]byte(raw), &spot)
    require.NoError(t, err)
    assert.False(t, spot.Pass)
    assert.Equal(t, "frontend", spot.ProjectType)
    assert.Len(t, spot.Issues, 1)
}
```

**Step 5: Run tests**

Run: `go test ./internal/model/... ./internal/spotter/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/model/types.go internal/spotter/
git commit -m "feat: add spotting goal status and SPOT.json types"
```

---

### Task 5: Spotter Prompt Template

Build the spotter prompt that instructs the LLM to determine project type, read the validation profile, and execute checks.

**Files:**
- Create: `internal/spotter/prompt.go`
- Create: `internal/spotter/prompt_test.go`

**Step 1: Write failing test**

```go
func TestBuildSpotterPrompt(t *testing.T) {
    prompt, err := BuildSpotterPrompt(SpotterPromptData{
        GoalID:      "setup",
        RepoName:    "frontend",
        Description: "Initialize project scaffolding",
        WorkDir:     "/tmp/test",
        Profiles:    map[string]string{"frontend": "build = \"Run build\""},
        DoneJSON:    `{"status": "complete", "summary": "scaffolded"}`,
    })
    require.NoError(t, err)
    assert.Contains(t, prompt, "setup")
    assert.Contains(t, prompt, "SPOT.json")
    assert.Contains(t, prompt, "frontend")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/spotter/... -run TestBuildSpotterPrompt`
Expected: FAIL

**Step 3: Implement SpotterPromptData and BuildSpotterPrompt**

```go
type SpotterPromptData struct {
    GoalID      string
    RepoName    string
    Description string
    WorkDir     string
    Profiles    map[string]string // profile name -> content
    DoneJSON    string
}
```

The prompt template (also saved as `internal/defaults/prompts/spotter.md`) instructs:
1. Look at the repo contents and determine project type (frontend, backend, CLI, library)
2. Read the matching validation profile below
3. Execute each check — adapt commands to the actual project setup
4. For frontend projects: use Chrome DevTools MCP to start dev server, navigate, screenshot, check console
5. Write SPOT.json with results

**Step 4: Run test to verify it passes**

Run: `go test ./internal/spotter/... -run TestBuildSpotterPrompt`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/spotter/
git commit -m "feat: spotter prompt template for per-goal validation"
```

---

### Task 6: Spotter Validation in TaskRunner

Add spotter spawning and SPOT.json checking to the TaskRunner.

**Files:**
- Modify: `internal/setter/taskrunner.go`
- Modify: `internal/setter/taskrunner_test.go` (or create if needed)

**Step 1: Write failing test for SpawnSpotter on goal completion**

Test that when a goal's DONE.json is detected, the TaskRunner transitions the goal to "spotting" status and spawns a spotter in the same tmux window.

**Step 2: Add SpawnSpotter method to TaskRunner**

```go
func (tr *TaskRunner) SpawnSpotter(goal *model.Goal) error {
    // 1. Mark goal as spotting
    // 2. Load spotter prompt template (from config loader)
    // 3. Load all validation profiles
    // 4. Read DONE.json for context
    // 5. Build prompt with SpotterPromptData
    // 6. Spawn fresh claude -p session in the SAME tmux window
    //    (the lead has already exited, window is idle)
    // 7. Record spotter_spawned event
}
```

Key detail: reuse the window name (same as goal ID). The lead session has exited so the window is at a shell prompt. Send a new `claude -p` command.

**Step 3: Add CheckSpotResult method to TaskRunner**

```go
func (tr *TaskRunner) CheckSpotResult(goal *model.Goal) (*spotter.SpotJSON, bool, error) {
    // 1. Check for SPOT.json in goal's worktree
    // 2. Parse it
    // 3. If pass: mark goal complete, kill window, return (spot, true, nil)
    // 4. If fail: mark goal failed, increment attempt, return (spot, true, nil)
    //    - The setter will re-queue as a lead retry with spotter feedback
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/setter/... -run TestSpawnSpotter`
Expected: PASS

**Step 5: Write test for spot failure → lead retry with feedback**

Test that when SPOT.json has `pass: false`, the goal gets re-queued with issues injected into the lead prompt.

**Step 6: Run tests**

Run: `go test ./internal/setter/...`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/setter/
git commit -m "feat: spotter validation in TaskRunner with SPOT.json checking"
```

---

### Task 7: Setter Loop — Spotting State Handling

Update the setter's tick() loop to handle the new "spotting" goal status.

**Files:**
- Modify: `internal/setter/setter.go`
- Modify: `internal/setter/setter_test.go`

**Step 1: Write failing test for spotting flow**

Test the full flow: lead writes DONE.json → goal transitions to spotting → spotter spawned → SPOT.json written → goal transitions to complete.

**Step 2: Update CheckCompletions in tick()**

Current flow: DONE.json detected → goal marked complete → ready goals returned.

New flow: DONE.json detected → goal marked **spotting** → spotter spawned (if validation enabled). If validation disabled, skip straight to complete (old behavior).

**Step 3: Add spotting check to tick()**

After processing running goals, add a loop for spotting goals:

```go
// Check spotting goals
for _, goal := range runner.dag.Goals() {
    if goal.Status != model.GoalStatusSpotting {
        continue
    }
    spot, found, err := runner.CheckSpotResult(goal)
    if err != nil { ... }
    if !found { continue }
    if spot.Pass {
        // Goal validated, check if newly ready goals are unblocked
    } else {
        // Re-queue goal as lead with spotter feedback
    }
}
```

**Step 4: Update AllGoalsComplete check**

Now requires all goals to be `complete` (not `spotting`). The existing `AllGoalsComplete()` in DAG should already handle this since spotting ≠ complete.

**Step 5: Run all tests**

Run: `go test ./internal/setter/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/setter/
git commit -m "feat: setter handles spotting state in tick loop"
```

---

### Task 8: Lead Prompt — Inject Spotter Feedback on Retry

When a lead retries after spotter failure, inject the spotter's issues into the prompt.

**Files:**
- Modify: `internal/defaults/prompts/lead.md`
- Modify: `internal/lead/prompt.go`
- Modify: `internal/lead/prompt_test.go`

**Step 1: Write failing test**

```go
func TestBuildPrompt_WithSpotterFeedback(t *testing.T) {
    prompt, err := BuildPrompt(PromptData{
        Spec: "build a site",
        GoalID: "setup",
        RepoName: "frontend",
        Description: "scaffold project",
        SpotterFeedback: "FAILED CHECKS:\n- visual_quality: Text not wrapping properly\n- console_errors: 2 errors in console",
    })
    require.NoError(t, err)
    assert.Contains(t, prompt, "Previous Attempt Feedback")
    assert.Contains(t, prompt, "Text not wrapping")
}
```

**Step 2: Add SpotterFeedback field to PromptData**

```go
type PromptData struct {
    Spec            string
    GoalID          string
    RepoName        string
    Description     string
    SpotterFeedback string // empty on first attempt
}
```

**Step 3: Update lead.md template**

Add conditional section:
```
{{if .SpotterFeedback}}
## Previous Attempt Feedback

A validator found issues with your previous attempt. You MUST address these:

{{.SpotterFeedback}}

Fix these issues before marking the goal complete.
{{end}}
```

**Step 4: Run tests**

Run: `go test ./internal/lead/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/lead/ internal/defaults/prompts/lead.md
git commit -m "feat: inject spotter feedback into lead prompt on retry"
```

---

### Task 9: `belayer init` Writes Config to Disk

Update the init command to write default config files to `~/.belayer/config/`.

**Files:**
- Modify: `internal/cli/init.go` (or wherever the init command lives)
- Create: `internal/defaults/write.go`
- Test: `internal/defaults/write_test.go`

**Step 1: Write failing test for WriteDefaults**

```go
func TestWriteDefaults(t *testing.T) {
    dir := t.TempDir()
    err := defaults.WriteToDir(dir)
    require.NoError(t, err)

    // Verify all files written
    data, err := os.ReadFile(filepath.Join(dir, "belayer.toml"))
    require.NoError(t, err)
    assert.Contains(t, string(data), "[agents]")

    data, err = os.ReadFile(filepath.Join(dir, "prompts", "lead.md"))
    require.NoError(t, err)
    assert.NotEmpty(t, data)
}
```

**Step 2: Implement WriteToDir**

```go
// WriteToDir copies all embedded defaults to the given directory.
// Existing files are NOT overwritten (user customizations preserved).
func WriteToDir(dir string) error {
    return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
        if d.IsDir() {
            return os.MkdirAll(filepath.Join(dir, path), 0755)
        }
        target := filepath.Join(dir, path)
        if _, err := os.Stat(target); err == nil {
            return nil // don't overwrite existing
        }
        data, _ := FS.ReadFile(path)
        return os.WriteFile(target, data, 0644)
    })
}
```

**Step 3: Run test**

Run: `go test ./internal/defaults/... -run TestWriteDefaults`
Expected: PASS

**Step 4: Update belayer init to call WriteToDir**

After creating `~/.belayer/config.json`, also call `defaults.WriteToDir(configDir + "/config")`.

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/defaults/ internal/cli/
git commit -m "feat: belayer init writes default config, prompts, and profiles to disk"
```

---

### Task 10: Wire Config into Setter/Spawner

Replace hardcoded values in the setter with config-loaded values. Update the spawner to use prompt templates from config.

**Files:**
- Modify: `internal/setter/setter.go` — use belayerconfig.Config
- Modify: `internal/setter/taskrunner.go` — load prompts from config
- Modify: `internal/lead/prompt.go` — load template from config instead of hardcoded
- Modify: `internal/anchor/prompt.go` — load template from config
- Modify: `internal/cli/setter.go` (or wherever setter command lives) — load config and pass to setter

**Step 1: Update Setter.Config to include belayerconfig**

Add `belayerconfig.Config` to the Setter struct so it has access to loaded prompts and profiles.

**Step 2: Update BuildPrompt to accept template string**

Change `BuildPrompt` from using the hardcoded `promptTemplate` constant to accepting a template string parameter. The caller loads it from config.

```go
func BuildPrompt(templateStr string, data PromptData) (string, error) {
```

Same for `BuildAnchorPrompt` and `BuildSpotterPrompt`.

**Step 3: Update TaskRunner to load prompts from config**

In `SpawnGoal()`, load the lead prompt template via config loader. In `SpawnAnchor()`, load the anchor template. In `SpawnSpotter()`, load the spotter template and profiles.

**Step 4: Update CLI setter command**

Load `belayerconfig.Config` from the instance's config directory and pass it through to the Setter and TaskRunner.

**Step 5: Run all tests**

Run: `go test ./...`
Expected: PASS — tests may need updating to pass template strings

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: wire config loader into setter, spawner, and prompt builders"
```

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
