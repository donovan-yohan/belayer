# Interactive Lead Sessions Implementation Plan

> **Status**: Completed | **Created**: 2026-03-08 | **Completed**: 2026-03-08
> **Design Doc**: `docs/plans/2026-03-08-interactive-lead-sessions-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Replace `claude -p` one-shot sessions with full interactive Claude Code sessions for all agent types (lead, spotter, anchor), enabling harness workflow, CLAUDE.md discovery, skills, and MCP tools.

**Architecture:** The setter prepares worktree environments (`.claude/CLAUDE.md` + `.lead/GOAL.json`) before spawning. The spawner switches from stdin-piped `claude -p` to `claude --dangerously-skip-permissions "initial prompt"`. Stuck detection uses log file mtime + tmux pane inspection. The coordination protocol (DONE.json polling, DAG, retry) is unchanged.

**Tech Stack:** Go 1.24, embed FS, existing tmux/spawner abstractions, Go `text/template` for CLAUDE.md rendering.

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-08 | Design | Full interactive sessions for all agents | The harness stack (skills, CLAUDE.md, MCP tools, reflect/complete) compounds — partial adoption loses the value |
| 2026-03-08 | Design | Positional arg, not `--initial-prompt` | `--initial-prompt` doesn't exist in Claude Code; positional arg is the equivalent |
| 2026-03-08 | Design | No send-keys nudging | Claude Code's Ink TUI makes `tmux send-keys` fragile (GitHub #15553, #23513); prevent + restart instead |
| 2026-03-08 | Design | Log mtime for silence detection | Simpler and more reliable than tmux `monitor-silence` hooks for Go integration |
| 2026-03-08 | Design | `.claude/CLAUDE.md` not project CLAUDE.md | Keeps belayer instructions out of git history while auto-loaded by Claude Code |
| 2026-03-08 | Design | Assume harness installed | No custom harness plugin needed; user's existing harness provides the workflow |
| 2026-03-08 | Retrospective | Plan completed | 8/8 tasks, net -45 lines, new goalctx package, CLAUDE.md environment prep pattern |

## Progress

- [x] Task 1: CLAUDE.md templates for lead, spotter, anchor _(completed 2026-03-08)_
- [x] Task 2: GOAL.json types and writers _(completed 2026-03-08)_
- [x] Task 3: Environment preparation in TaskRunner _(completed 2026-03-08)_
- [x] Task 4: Update ClaudeSpawner for interactive sessions _(completed 2026-03-08)_
- [x] Task 5: Tmux `remain-on-exit` and pane death detection _(completed 2026-03-08)_
- [x] Task 6: Log mtime silence detection in stale checker _(completed 2026-03-08)_
- [x] Task 7: Remove old prompt template system _(completed 2026-03-08)_
- [x] Task 8: Update tests _(completed 2026-03-08)_

## Dependency Graph

```
Task 1 ──┐
Task 2 ──┼── Task 3 ──── Task 7
Task 4 ──┘         \
Task 5 ──────────── Task 6
                         \
Task 8 (after all others)
```

Tasks 1, 2, 4, 5 are independent and can run in parallel.
Task 3 depends on 1, 2, 4.
Task 6 depends on 5.
Task 7 depends on 3.
Task 8 depends on all.

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-08 | Worker-5 completed Task 6 despite redirect; worker-6 also ran it | Duplicate commits, but worker-6 caught missing SpawnSpotter remain-on-exit | Both commits kept — worker-6 added the missing call |

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: CLAUDE.md Templates for Lead, Spotter, Anchor

Create embedded Go templates for the `.claude/CLAUDE.md` files that the setter writes into worktrees before spawning agents.

**Files:**
- Create: `internal/defaults/claudemd/lead.md`
- Create: `internal/defaults/claudemd/spotter.md`
- Create: `internal/defaults/claudemd/anchor.md`
- Modify: `internal/defaults/defaults.go` (add embed directive)
- Test: `internal/defaults/defaults_test.go`

**Step 1: Create lead CLAUDE.md template**

Create `internal/defaults/claudemd/lead.md`:

```markdown
# Belayer Lead

You are operating as an autonomous lead agent managed by belayer.

## Your Assignment

Read `.lead/GOAL.json` for your full assignment context including task spec, goal description, and any feedback from previous attempts.

## Autonomous Operation

You MUST operate fully autonomously:
- NEVER ask questions or wait for user input
- NEVER request clarification — make your best judgment and proceed
- If you encounter ambiguity, document your decision and move forward
- Use available skills, MCP tools, and harness commands as needed

## Workflow

1. Read `.lead/GOAL.json` to understand your assignment
2. Use `/harness:plan` to create an implementation plan for your goal
3. Use `/harness:orchestrate` to execute with agent teams if beneficial
4. Implement, test, commit, and push your changes
5. Use `/harness:reflect` to update documentation
6. Write `DONE.json` when complete (see format below)

## DONE.json Contract

When finished, write `DONE.json` in the working directory:

```json
{
  "status": "complete",
  "summary": "Brief description of what was done",
  "files_changed": ["list", "of", "files"],
  "notes": "Any context for reviewers"
}
```

If you cannot complete the goal, write DONE.json with `"status": "failed"` and explain what blocked you.

IMPORTANT: You MUST commit, push, and write DONE.json before your session ends.
```

**Step 2: Create spotter CLAUDE.md template**

Create `internal/defaults/claudemd/spotter.md`:

```markdown
# Belayer Spotter

You are operating as an autonomous spotter (validator) agent managed by belayer.

## Your Assignment

Read `.lead/GOAL.json` for your full assignment context including what was implemented, validation profiles, and the DONE.json from the lead.

## Autonomous Operation

You MUST operate fully autonomously:
- NEVER ask questions or wait for user input
- NEVER request clarification — make your best judgment and proceed
- Use available skills, MCP tools (Chrome DevTools for frontend validation), and harness commands

## Workflow

1. Read `.lead/GOAL.json` to understand what the lead implemented
2. Examine the repo to determine project type (frontend, backend, CLI, library)
3. Read the matching validation profile from `.lead/profiles/`
4. Execute each check in the profile (build, tests, dev server, browser, etc.)
5. Write `SPOT.json` with your verdict

## SPOT.json Contract

Write `SPOT.json` in the working directory:

```json
{
  "pass": true,
  "project_type": "frontend",
  "issues": [],
  "screenshots": []
}
```

If checks fail:

```json
{
  "pass": false,
  "project_type": "frontend",
  "issues": [
    {"check": "visual_quality", "severity": "error", "description": "Text not wrapping properly in hero section"}
  ],
  "screenshots": ["screenshot-1.png"]
}
```

IMPORTANT: You MUST write SPOT.json before your session ends.
```

**Step 3: Create anchor CLAUDE.md template**

Create `internal/defaults/claudemd/anchor.md`:

```markdown
# Belayer Anchor

You are operating as an autonomous anchor (cross-repo reviewer) agent managed by belayer.

## Your Assignment

Read `.lead/GOAL.json` for your full assignment context including diffs from all repositories, goal summaries, and the original task specification.

## Autonomous Operation

You MUST operate fully autonomously:
- NEVER ask questions or wait for user input
- NEVER request clarification — make your best judgment and proceed

## Workflow

1. Read `.lead/GOAL.json` to understand the full task context
2. Review ALL repository diffs against the original task specification
3. Check cross-repo alignment:
   - API contracts match between frontend and backend
   - Shared types, schemas, or interfaces are consistent
   - Integration points are compatible
4. Verify each repo's changes fulfill their assigned goals
5. Write `VERDICT.json` with your verdict

## VERDICT.json Contract

Write `VERDICT.json` in the working directory:

If approved:
```json
{
  "verdict": "approve",
  "repos": {
    "repo-name": {"status": "pass", "goals": []}
  }
}
```

If rejected (specify correction goals):
```json
{
  "verdict": "reject",
  "repos": {
    "failing-repo": {
      "status": "fail",
      "goals": ["Fix the response schema to match frontend expectations"]
    }
  }
}
```

IMPORTANT: You MUST write VERDICT.json before your session ends. Include ALL repos in the verdict.
```

**Step 4: Update defaults.go embed directive**

Add to `internal/defaults/defaults.go`:

```go
//go:embed belayer.toml prompts/*.md profiles/*.toml claudemd/*.md
var FS embed.FS
```

**Step 5: Add test for new embedded files**

Add to `internal/defaults/defaults_test.go`:

```go
func TestClaudeMDTemplatesExist(t *testing.T) {
    for _, name := range []string{"lead.md", "spotter.md", "anchor.md"} {
        data, err := defaults.FS.ReadFile("claudemd/" + name)
        require.NoError(t, err, "claudemd/%s should be embedded", name)
        assert.NotEmpty(t, data)
    }
}
```

**Step 6: Run tests**

Run: `go test ./internal/defaults/...`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/defaults/claudemd/ internal/defaults/defaults.go internal/defaults/defaults_test.go
git commit -m "feat: add CLAUDE.md templates for lead, spotter, anchor agents"
```

---

### Task 2: GOAL.json Types and Writers

Create Go types and writer functions for the `.lead/GOAL.json` context files, one variant per agent role.

**Files:**
- Create: `internal/goalctx/goalctx.go`
- Create: `internal/goalctx/goalctx_test.go`

**Step 1: Write the failing test**

Create `internal/goalctx/goalctx_test.go`:

```go
package goalctx

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestWriteLeadGoal(t *testing.T) {
    dir := t.TempDir()
    goal := LeadGoal{
        Role:            "lead",
        TaskSpec:        "Build an API",
        GoalID:          "api-1",
        RepoName:        "api",
        Description:     "Add /users endpoint",
        Attempt:         1,
        SpotterFeedback: "",
    }
    err := WriteGoalJSON(dir, goal)
    require.NoError(t, err)

    data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
    require.NoError(t, err)

    var parsed LeadGoal
    require.NoError(t, json.Unmarshal(data, &parsed))
    assert.Equal(t, "lead", parsed.Role)
    assert.Equal(t, "api-1", parsed.GoalID)
    assert.Equal(t, "Build an API", parsed.TaskSpec)
}

func TestWriteSpotterGoal(t *testing.T) {
    dir := t.TempDir()
    goal := SpotterGoal{
        Role:        "spotter",
        GoalID:      "fe-1",
        RepoName:    "app",
        Description: "Scaffold frontend",
        WorkDir:     "/tmp/worktree",
        Profiles:    map[string]string{"frontend": "[checks]\nbuild = \"npm run build\""},
        DoneJSON:    `{"status":"complete","summary":"done"}`,
    }
    err := WriteGoalJSON(dir, goal)
    require.NoError(t, err)

    data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
    require.NoError(t, err)

    var parsed SpotterGoal
    require.NoError(t, json.Unmarshal(data, &parsed))
    assert.Equal(t, "spotter", parsed.Role)
    assert.Contains(t, parsed.Profiles, "frontend")
}

func TestWriteAnchorGoal(t *testing.T) {
    dir := t.TempDir()
    goal := AnchorGoal{
        Role:     "anchor",
        TaskSpec: "Build an app",
        RepoDiffs: []RepoDiff{
            {RepoName: "api", DiffStat: "handlers.go | 25 +++", Diff: "+func Get()"},
        },
        Summaries: []GoalSummary{
            {GoalID: "api-1", RepoName: "api", Summary: "Added endpoint"},
        },
    }
    err := WriteGoalJSON(dir, goal)
    require.NoError(t, err)

    data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
    require.NoError(t, err)

    var parsed AnchorGoal
    require.NoError(t, json.Unmarshal(data, &parsed))
    assert.Equal(t, "anchor", parsed.Role)
    assert.Len(t, parsed.RepoDiffs, 1)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/goalctx/... -v`
Expected: FAIL (package doesn't exist)

**Step 3: Implement goalctx package**

Create `internal/goalctx/goalctx.go`:

```go
package goalctx

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
)

// LeadGoal is the GOAL.json context for a lead agent.
type LeadGoal struct {
    Role            string `json:"role"`
    TaskSpec        string `json:"task_spec"`
    GoalID          string `json:"goal_id"`
    RepoName        string `json:"repo_name"`
    Description     string `json:"description"`
    Attempt         int    `json:"attempt"`
    SpotterFeedback string `json:"spotter_feedback,omitempty"`
}

// SpotterGoal is the GOAL.json context for a spotter agent.
type SpotterGoal struct {
    Role        string            `json:"role"`
    GoalID      string            `json:"goal_id"`
    RepoName    string            `json:"repo_name"`
    Description string            `json:"description"`
    WorkDir     string            `json:"work_dir"`
    Profiles    map[string]string `json:"profiles"`
    DoneJSON    string            `json:"done_json"`
}

// AnchorGoal is the GOAL.json context for an anchor agent.
type AnchorGoal struct {
    Role      string        `json:"role"`
    TaskSpec  string        `json:"task_spec"`
    RepoDiffs []RepoDiff    `json:"repo_diffs"`
    Summaries []GoalSummary `json:"summaries"`
}

// RepoDiff contains git diff output for a single repo.
type RepoDiff struct {
    RepoName string `json:"repo_name"`
    DiffStat string `json:"diff_stat"`
    Diff     string `json:"diff"`
}

// GoalSummary contains the completion summary for a single goal.
type GoalSummary struct {
    GoalID      string `json:"goal_id"`
    RepoName    string `json:"repo_name"`
    Description string `json:"description,omitempty"`
    Status      string `json:"status,omitempty"`
    Summary     string `json:"summary"`
    Notes       string `json:"notes,omitempty"`
}

// WriteGoalJSON writes the goal context to <dir>/.lead/GOAL.json.
func WriteGoalJSON(dir string, goal any) error {
    leadDir := filepath.Join(dir, ".lead")
    if err := os.MkdirAll(leadDir, 0o755); err != nil {
        return fmt.Errorf("creating .lead directory: %w", err)
    }

    data, err := json.MarshalIndent(goal, "", "  ")
    if err != nil {
        return fmt.Errorf("marshaling GOAL.json: %w", err)
    }

    goalPath := filepath.Join(leadDir, "GOAL.json")
    if err := os.WriteFile(goalPath, data, 0o644); err != nil {
        return fmt.Errorf("writing GOAL.json: %w", err)
    }

    return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/goalctx/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/goalctx/
git commit -m "feat: add goalctx package for GOAL.json types and writer"
```

---

### Task 3: Environment Preparation in TaskRunner

Update `SpawnGoal`, `SpawnSpotter`, and `SpawnAnchor` to write `.claude/CLAUDE.md` and `.lead/GOAL.json` instead of building prompt strings.

**Files:**
- Modify: `internal/setter/taskrunner.go`

**Step 1: Add helper to write CLAUDE.md**

Add a `writeClaudeMD` method to TaskRunner that:
1. Reads the role-specific CLAUDE.md template from embedded defaults (or config chain)
2. Checks if `.claude/CLAUDE.md` already exists in the worktree; if so, prepends belayer content
3. Writes to `<worktree>/.claude/CLAUDE.md`

```go
func (tr *TaskRunner) writeClaudeMD(worktreePath, role string) error {
    tmplBytes, err := defaults.FS.ReadFile("claudemd/" + role + ".md")
    if err != nil {
        return fmt.Errorf("reading %s CLAUDE.md template: %w", role, err)
    }
    belayerContent := string(tmplBytes)

    claudeDir := filepath.Join(worktreePath, ".claude")
    if err := os.MkdirAll(claudeDir, 0o755); err != nil {
        return fmt.Errorf("creating .claude directory: %w", err)
    }

    claudeMDPath := filepath.Join(claudeDir, "CLAUDE.md")

    // Preserve existing CLAUDE.md content
    existing, _ := os.ReadFile(claudeMDPath)
    if len(existing) > 0 {
        belayerContent = belayerContent + "\n\n---\n\n" + string(existing)
    }

    return os.WriteFile(claudeMDPath, []byte(belayerContent), 0o644)
}
```

**Step 2: Update SpawnGoal**

Replace the prompt template loading and `lead.BuildPrompt()` call with:
1. `tr.writeClaudeMD(worktreePath, "lead")`
2. `goalctx.WriteGoalJSON(worktreePath, goalctx.LeadGoal{...})`
3. Pass a short initial prompt string to `spawner.Spawn()` instead of the full prompt

**Step 3: Update SpawnSpotter**

Replace the profile loading, template loading, and `spotter.BuildSpotterPrompt()` call with:
1. `tr.writeClaudeMD(worktreePath, "spotter")`
2. Write profiles to `.lead/profiles/` directory for agent discovery
3. `goalctx.WriteGoalJSON(worktreePath, goalctx.SpotterGoal{...})`
4. Pass a short initial prompt to `spawner.Spawn()`

**Step 4: Update SpawnAnchor**

Replace the template loading, diff gathering, and `anchor.BuildAnchorPrompt()` call with:
1. `tr.writeClaudeMD(tr.taskDir, "anchor")`
2. `goalctx.WriteGoalJSON(tr.taskDir, goalctx.AnchorGoal{...})`
3. Pass a short initial prompt to `spawner.Spawn()`

Note: `GatherDiffs()` and `GatherSummaries()` stay — their output goes into `AnchorGoal` struct fields instead of `AnchorPromptData`.

**Step 5: Build and run existing tests**

Run: `go build ./cmd/belayer && go test ./internal/setter/...`
Expected: Some tests may fail due to changed SpawnGoal behavior — fix in Task 8.

**Step 6: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "feat: write .claude/CLAUDE.md + .lead/GOAL.json instead of prompt templates"
```

---

### Task 4: Update ClaudeSpawner for Interactive Sessions

Change the spawner from `claude -p` with stdin piping to `claude` with positional argument.

**Files:**
- Modify: `internal/lead/claude.go`
- Modify: `internal/lead/spawner.go` (rename `Prompt` field to `InitialPrompt` for clarity)

**Step 1: Update SpawnOpts**

In `internal/lead/spawner.go`, rename `Prompt` to `InitialPrompt`:

```go
type SpawnOpts struct {
    TmuxSession    string
    WindowName     string
    WorkDir        string
    InitialPrompt  string
}
```

**Step 2: Update ClaudeSpawner.Spawn()**

In `internal/lead/claude.go`, replace the implementation:

```go
func (c *ClaudeSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
    // Build command: full interactive session with positional prompt
    cmd := fmt.Sprintf("cd %s && claude --dangerously-skip-permissions %s 2>&1; echo 'Claude session exited'",
        shellQuote(opts.WorkDir),
        shellQuote(opts.InitialPrompt))

    return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}
```

No more temp file writing. No more stdin piping. No more `-p` flag.

**Step 3: Update all callers of SpawnOpts**

Search for all `lead.SpawnOpts{` usages and rename `Prompt:` to `InitialPrompt:`.

**Step 4: Build**

Run: `go build ./cmd/belayer`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/lead/claude.go internal/lead/spawner.go internal/setter/taskrunner.go
git commit -m "feat: switch ClaudeSpawner from claude -p to interactive session with positional arg"
```

---

### Task 5: Tmux `remain-on-exit` and Pane Death Detection

Add tmux methods for `remain-on-exit` configuration and pane state inspection.

**Files:**
- Modify: `internal/tmux/tmux.go` (add methods to interface + RealTmux)

**Step 1: Add new methods to TmuxManager interface**

```go
// SetRemainOnExit configures a window to keep the pane open after the process exits.
SetRemainOnExit(session, windowName string, enabled bool) error

// IsPaneDead checks if the process in a window has exited.
IsPaneDead(session, windowName string) (bool, error)

// CapturePaneContent captures the last N lines of visible pane content.
CapturePaneContent(session, windowName string, lines int) (string, error)
```

**Step 2: Implement on RealTmux**

```go
func (r *RealTmux) SetRemainOnExit(session, windowName string, enabled bool) error {
    target := session + ":" + windowName
    val := "off"
    if enabled {
        val = "on"
    }
    cmd := exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", val)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("tmux set-option remain-on-exit -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
    }
    return nil
}

func (r *RealTmux) IsPaneDead(session, windowName string) (bool, error) {
    target := session + ":" + windowName
    cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_dead}")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return false, fmt.Errorf("tmux display-message -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
    }
    return strings.TrimSpace(string(output)) == "1", nil
}

func (r *RealTmux) CapturePaneContent(session, windowName string, lines int) (string, error) {
    target := session + ":" + windowName
    startLine := fmt.Sprintf("-%d", lines)
    cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", startLine)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("tmux capture-pane -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
    }
    return string(output), nil
}
```

**Step 3: Build**

Run: `go build ./cmd/belayer`
Expected: PASS (may need to update mock tmux in tests — deferred to Task 8)

**Step 4: Commit**

```bash
git add internal/tmux/tmux.go
git commit -m "feat: add tmux remain-on-exit, pane death detection, and capture-pane"
```

---

### Task 6: Log Mtime Silence Detection in Stale Checker

Enhance `CheckStaleGoals` to detect stuck sessions earlier using log file modification time, before the full stale timeout fires.

**Files:**
- Modify: `internal/setter/taskrunner.go`

**Step 1: Add silence detection to CheckStaleGoals**

Before the existing `timedOut` check, add:

```go
// Check for silence — no log output for silenceThreshold
logPath := tr.logMgr.LogPath(tr.task.ID, g.ID)
if info, statErr := os.Stat(logPath); statErr == nil {
    silenceThreshold := 2 * time.Minute
    if now.Sub(info.ModTime()) > silenceThreshold {
        // Capture pane to check if waiting for input
        paneContent, captureErr := tr.tmux.CapturePaneContent(tr.tmuxSession, windowName, 30)
        if captureErr == nil && looksLikeInputPrompt(paneContent) {
            windowDead = true // treat as stuck
            reason = "waiting for input"
        }

        // Also check if process has exited
        if dead, deadErr := tr.tmux.IsPaneDead(tr.tmuxSession, windowName); deadErr == nil && dead {
            windowDead = true
            reason = "process exited without signal file"
        }
    }
}
```

**Step 2: Add looksLikeInputPrompt helper**

```go
// looksLikeInputPrompt checks if captured pane content suggests the session
// is waiting for user input rather than actively working.
func looksLikeInputPrompt(content string) bool {
    lines := strings.Split(strings.TrimSpace(content), "\n")
    if len(lines) == 0 {
        return false
    }
    lastLine := strings.TrimSpace(lines[len(lines)-1])
    // Claude Code shows ">" when waiting for input
    return lastLine == ">" || strings.HasSuffix(lastLine, "> ")
}
```

**Step 3: Wire remain-on-exit into SpawnGoal**

After creating the tmux window in `SpawnGoal`, `SpawnSpotter`, and `SpawnAnchor`, call:
```go
tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true)
```

**Step 4: Build and test**

Run: `go build ./cmd/belayer && go test ./internal/setter/...`
Expected: PASS (or minor test fixes needed)

**Step 5: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "feat: add log mtime silence detection and remain-on-exit for stuck sessions"
```

---

### Task 7: Remove Old Prompt Template System

Clean up the now-unused prompt building code and templates.

**Files:**
- Delete: `internal/defaults/prompts/lead.md`
- Delete: `internal/defaults/prompts/spotter.md`
- Delete: `internal/defaults/prompts/anchor.md`
- Modify: `internal/defaults/defaults.go` (update embed directive to remove `prompts/*.md`)
- Modify: `internal/lead/prompt.go` (remove BuildPrompt, BuildPromptDefault, PromptData)
- Modify: `internal/spotter/prompt.go` (remove BuildSpotterPrompt, BuildSpotterPromptDefault, SpotterPromptData)
- Modify: `internal/anchor/prompt.go` (remove BuildAnchorPrompt, BuildAnchorPromptDefault, AnchorPromptData)
- Delete: `internal/lead/prompt_test.go`
- Delete: `internal/spotter/prompt_test.go`
- Delete: `internal/anchor/prompt_test.go`
- Modify: `internal/belayerconfig/config.go` (remove LoadPrompt if no longer used)
- Modify: `internal/setter/taskrunner.go` (remove unused imports for belayerconfig prompt loading)

**Step 1: Remove prompt template files**

```bash
rm internal/defaults/prompts/lead.md
rm internal/defaults/prompts/spotter.md
rm internal/defaults/prompts/anchor.md
```

**Step 2: Update embed directive**

Change `internal/defaults/defaults.go`:
```go
//go:embed belayer.toml profiles/*.toml claudemd/*.md
var FS embed.FS
```

**Step 3: Clean up prompt builder functions**

Remove from `internal/lead/prompt.go`: `PromptData`, `BuildPrompt`, `BuildPromptDefault`. Keep the file if it still has other exports, delete if empty.

Remove from `internal/spotter/prompt.go`: `SpotterPromptData`, `BuildSpotterPrompt`, `BuildSpotterPromptDefault`. Keep types like `SpotJSON` and `Issue`.

Remove from `internal/anchor/prompt.go`: `AnchorPromptData`, `RepoDiff`, `GoalSummary`, `BuildAnchorPrompt`, `BuildAnchorPromptDefault`. Note: `RepoDiff` and `GoalSummary` may have moved to `goalctx` package — verify no circular imports.

**Step 4: Delete prompt test files**

```bash
rm internal/lead/prompt_test.go
rm internal/spotter/prompt_test.go
rm internal/anchor/prompt_test.go
```

**Step 5: Clean up unused imports in taskrunner.go**

Remove imports for `lead`, `spotter`, `anchor` packages if they're only used for prompt building. Keep imports for types still in use (SpotJSON, VerdictJSON).

**Step 6: Remove LoadPrompt from belayerconfig if unused**

If `belayerconfig.LoadPrompt` is no longer called anywhere, remove it. Keep `LoadProfile` since validation profiles are still used.

**Step 7: Build and test**

Run: `go build ./cmd/belayer && go test ./...`
Expected: PASS

**Step 8: Commit**

```bash
git add -A
git commit -m "refactor: remove old prompt template system replaced by CLAUDE.md + GOAL.json"
```

---

### Task 8: Update Tests

Update all tests to work with the new interactive session spawning, mock the new tmux methods, and verify environment preparation.

**Files:**
- Modify: `internal/setter/setter_test.go`
- Modify: `internal/defaults/defaults_test.go`
- Add to: `internal/defaults/write_test.go` (if CLAUDE.md writing needs testing)

**Step 1: Update mock TmuxManager**

Add mock implementations for the 3 new interface methods:
- `SetRemainOnExit` — no-op, record calls
- `IsPaneDead` — return configurable value
- `CapturePaneContent` — return configurable string

**Step 2: Update SpawnGoal tests**

Tests that check `SpawnGoal` behavior need to verify:
- `.claude/CLAUDE.md` is written to the worktree
- `.lead/GOAL.json` is written with correct content
- `spawner.Spawn()` is called with `InitialPrompt` (not the old full prompt)
- `SetRemainOnExit` is called on the window

**Step 3: Update SpawnSpotter tests**

Verify:
- `.claude/CLAUDE.md` is written with spotter template
- `.lead/GOAL.json` contains profiles and DONE.json content
- Profiles written to `.lead/profiles/` if applicable

**Step 4: Update anchor flow tests**

Verify:
- `.claude/CLAUDE.md` is written with anchor template
- `.lead/GOAL.json` contains repo diffs and summaries

**Step 5: Add silence detection tests**

Test `looksLikeInputPrompt()` with various inputs:
- `">"` → true
- `"some output\n>"` → true
- `"thinking..."` → false
- `""` → false

**Step 6: Verify CLAUDE.md conflict handling**

Test that when an existing `.claude/CLAUDE.md` exists, belayer content is prepended.

**Step 7: Run full test suite**

Run: `go test ./... && go build ./cmd/belayer`
Expected: ALL PASS

**Step 8: Commit**

```bash
git add internal/setter/setter_test.go internal/defaults/
git commit -m "test: update tests for interactive session spawning and environment preparation"
```

---

## Outcomes & Retrospective

**What worked:**
- Parallel dispatch of independent tasks (1, 2, 4, 5) maximized throughput
- Clean separation between environment prep (CLAUDE.md + GOAL.json) and spawner change made tasks truly independent
- Net -45 lines despite adding new functionality — cleaner codebase
- Workers proactively fixed tests in their own tasks, reducing Task 8 scope

**What didn't:**
- Duplicate work on Tasks 3, 6, 7 due to workers completing tasks before receiving redirect/shutdown messages
- Worker-5 ignored the "stand by" message and completed Task 6 anyway (though it caught a missing SetRemainOnExit call)

**Learnings to codify:**
- None requested by user
