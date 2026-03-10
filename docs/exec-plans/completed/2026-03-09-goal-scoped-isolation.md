# Goal-Scoped Isolation Implementation Plan

> **Status**: Completed | **Created**: 2026-03-09 | **Completed**: 2026-03-10
> **Design Doc**: _(from conversation — no formal design doc)_
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-09 | Design | Use `--append-system-prompt` for role instructions instead of `.claude/CLAUDE.md` | Shared worktree means multiple goals overwrite each other's CLAUDE.md; also conflicts with repo's own `.claude/` |
| 2026-03-09 | Design | Use `--append-system-prompt` (not `--system-prompt`) | `--system-prompt` replaces the entire default prompt including tool instructions; `--append-system-prompt` preserves built-in behavior + user plugins |
| 2026-03-09 | Design | Drive harness workflow via initial prompt, not system prompt | Initial prompt is dynamic per-spawn; tells lead to use /harness:plan, /harness:orchestrate, /harness:complete explicitly |
| 2026-03-09 | Design | Goal-scoped subdirectory `.lead/<goalID>/` for signal files | GOAL.json, DONE.json, SPOT.json must not collide between concurrent same-repo goals |
| 2026-03-09 | Design | Separate tmux windows for leads and spotters | Reusing the lead's window caused infinite loops when context bled between roles |
| 2026-03-09 | Design | Anchor keeps its own window (already separate) | Anchor operates at task level, not goal level — no change needed |

## Progress

- [x] Task 1: Add `AppendSystemPrompt` to `SpawnOpts` and `ClaudeSpawner` _(completed 2026-03-10)_
- [x] Task 2: Make `WriteGoalJSON` goal-scoped _(completed 2026-03-10)_
- [x] Task 3: Update `SpawnGoal` — system prompt + goal-scoped dir + remove `writeClaudeMD` _(completed 2026-03-10)_
- [x] Task 4: Update `SpawnSpotter` — own tmux window + system prompt + goal-scoped signal files _(completed 2026-03-10)_
- [x] Task 5: Update `CheckCompletions` — read DONE.json from goal-scoped path _(completed 2026-03-10)_
- [x] Task 6: Update `CheckSpotResult` — read SPOT.json from goal-scoped path _(completed 2026-03-10)_
- [x] Task 7: Update `SpawnAnchor` — system prompt, remove `writeClaudeMD` _(completed 2026-03-10)_
- [x] Task 8: Delete `writeClaudeMD` method and `.orig` backup logic _(completed 2026-03-10)_
- [x] Task 9: Update tests _(completed 2026-03-10)_
- [x] Task 10: Update lead/spotter/anchor CLAUDE.md templates (path references) _(completed 2026-03-10)_

## Surprises & Discoveries

None — clean execution.

## Plan Drift

None — all tasks followed the plan as specified.

---

## Context

### Problem

Three bugs caused by sharing worktree state:

1. **CLAUDE.md collision**: Lead writes `.claude/CLAUDE.md` with lead role, spotter overwrites with spotter role. Next goal spawned into same worktree reads stale spotter CLAUDE.md and enters infinite loop thinking it's a spotter.

2. **Signal file collision**: DONE.json, SPOT.json, and GOAL.json all written to worktree root or `.lead/`. Concurrent same-repo goals (or sequential goals reusing the worktree) can read stale signal files.

3. **Repo `.claude/` conflict**: Repos may have their own `.claude/` configs checked in. The `writeClaudeMD` method's `.orig` backup hack is brittle and can corrupt repo settings.

### Solution

- **Role instructions**: Pass via `--append-system-prompt` flag (per-process, zero file footprint, preserves built-in Claude Code behavior + user plugins)
- **Signal files**: Namespace under `.lead/<goalID>/` (GOAL.json, DONE.json, SPOT.json)
- **Window isolation**: Spotters get their own tmux window (`spot-<goalID>`) instead of reusing the lead's window

### Implications

1. **Lead CLAUDE.md template content** still needs to be passed — just via `--append-system-prompt` instead of a file. The embedded `defaults/claudemd/*.md` files remain the source of truth. Using `--append-system-prompt` (not `--system-prompt`) preserves the default Claude Code system prompt, which means all built-in tools, plugins, and skills the user has installed locally remain available to agents.

2. **GOAL.json path change** ripples to:
   - Lead instructions (tell them to read `.lead/<goalID>/GOAL.json` — but goalID is known from the system prompt)
   - Spotter instructions (same)
   - `CheckCompletions()` — looks for `.lead/<goalID>/DONE.json` instead of `DONE.json`
   - `CheckSpotResult()` — looks for `.lead/<goalID>/SPOT.json` instead of `SPOT.json`
   - `GatherSummaries()` — reads DONE.json from goal-scoped path

3. **Anchor is unaffected** by goal-scoping (it operates at task level in `taskDir`), but should still switch to `--append-system-prompt` for consistency.

4. **`writeProfiles`** currently writes to `.lead/profiles/` in the shared worktree. This is fine — profiles are repo-level, not goal-level. No change needed.

5. **`writeClaudeMD` deletion** means no more `.claude/CLAUDE.md.orig` backup logic. The repo's own `.claude/` directory is never touched.

6. **Initial prompt update**: Currently leads are told "Read .lead/GOAL.json and begin working." The goal-scoped path means either:
   - Option A: Embed the goalID in the initial prompt: "Read .lead/api-1/GOAL.json"
   - Option B: Put the path in the system prompt itself
   - **Decision: Option A** — it's already dynamic per-spawn, minimal change

7. **Spotter window naming**: Currently spotter reuses `<repo>-<goalID>`. With separate windows, use `spot-<goalID>` to distinguish from lead window `<repo>-<goalID>`.

8. **Lead window cleanup**: When `CheckCompletions` detects DONE.json and spawns a spotter, the lead's tmux window should now be killed (lead is done). Previously it was kept alive for the spotter to reuse.

---

### Task 1: Add `AppendSystemPrompt` to `SpawnOpts` and `ClaudeSpawner`

**Files:**
- Modify: `internal/lead/spawner.go`
- Modify: `internal/lead/claude.go`
- Test: `internal/lead/claude_test.go`

**Step 1: Write failing test for `--append-system-prompt` flag**

Add to `internal/lead/claude_test.go`:

```go
func TestClaudeSpawner_AppendSystemPrompt(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewClaudeSpawner(tm)
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:        "s",
		WindowName:         "w",
		WorkDir:            t.TempDir(),
		InitialPrompt:      "Do the thing",
		AppendSystemPrompt: "You are a lead agent.",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	assert.Contains(t, sentKeys, "--append-system-prompt")
	assert.Contains(t, sentKeys, "You are a lead agent.")
}

func TestClaudeSpawner_NoAppendSystemPrompt(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewClaudeSpawner(tm)
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:   "s",
		WindowName:    "w",
		WorkDir:       t.TempDir(),
		InitialPrompt: "Do the thing",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	assert.NotContains(t, sentKeys, "--append-system-prompt")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/lead/ -run TestClaudeSpawner_AppendSystemPrompt -v`
Expected: FAIL — `SpawnOpts` doesn't have `AppendSystemPrompt` field

**Step 3: Add `AppendSystemPrompt` field to `SpawnOpts`**

In `internal/lead/spawner.go`, add field:

```go
type SpawnOpts struct {
	TmuxSession        string
	WindowName         string
	WorkDir            string
	InitialPrompt      string
	AppendSystemPrompt string // Role-specific instructions appended to default system prompt via --append-system-prompt
}
```

**Step 4: Update `ClaudeSpawner.Spawn` to pass `--append-system-prompt`**

In `internal/lead/claude.go`:

```go
func (c *ClaudeSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	var appendPromptFlag string
	if opts.AppendSystemPrompt != "" {
		appendPromptFlag = " --append-system-prompt " + shellQuote(opts.AppendSystemPrompt)
	}

	cmd := fmt.Sprintf("cd %s && claude --dangerously-skip-permissions%s %s 2>&1; echo 'Claude session exited'",
		shellQuote(opts.WorkDir),
		appendPromptFlag,
		shellQuote(opts.InitialPrompt))

	return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/lead/ -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/lead/spawner.go internal/lead/claude.go internal/lead/claude_test.go
git commit -m "feat: add AppendSystemPrompt field to SpawnOpts for --append-system-prompt flag"
```

---

### Task 2: Make `WriteGoalJSON` goal-scoped

**Files:**
- Modify: `internal/goalctx/goalctx.go`
- Test: `internal/goalctx/goalctx_test.go`

**Step 1: Write failing test for goal-scoped path**

Add to `internal/goalctx/goalctx_test.go`:

```go
func TestWriteGoalJSON_GoalScoped(t *testing.T) {
	dir := t.TempDir()
	goal := LeadGoal{
		Role:    "lead",
		GoalID:  "api-1",
	}
	err := WriteGoalJSON(dir, "api-1", goal)
	require.NoError(t, err)

	// Should be at .lead/<goalID>/GOAL.json
	data, err := os.ReadFile(filepath.Join(dir, ".lead", "api-1", "GOAL.json"))
	require.NoError(t, err)

	var parsed LeadGoal
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "api-1", parsed.GoalID)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/goalctx/ -run TestWriteGoalJSON_GoalScoped -v`
Expected: FAIL — `WriteGoalJSON` has wrong signature

**Step 3: Update `WriteGoalJSON` signature to accept goalID**

In `internal/goalctx/goalctx.go`:

```go
// WriteGoalJSON writes the goal context to <dir>/.lead/<goalID>/GOAL.json.
func WriteGoalJSON(dir string, goalID string, goal any) error {
	goalDir := filepath.Join(dir, ".lead", goalID)
	if err := os.MkdirAll(goalDir, 0o755); err != nil {
		return fmt.Errorf("creating .lead/%s directory: %w", goalID, err)
	}

	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling GOAL.json: %w", err)
	}

	goalPath := filepath.Join(goalDir, "GOAL.json")
	if err := os.WriteFile(goalPath, data, 0o644); err != nil {
		return fmt.Errorf("writing GOAL.json: %w", err)
	}

	return nil
}
```

**Step 4: Update existing tests to pass goalID**

Update all three test functions (`TestWriteLeadGoal`, `TestWriteSpotterGoal`, `TestWriteAnchorGoal`) to pass a goalID argument and read from the new path.

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/goalctx/ -v`
Expected: ALL PASS

**Step 6: Fix compile errors in callers**

All callers of `WriteGoalJSON` need the new goalID argument. These are in `taskrunner.go` (SpawnGoal, SpawnSpotter, SpawnAnchor). They will fail to compile until Task 3+. This is expected — just ensure `goalctx` tests pass in isolation.

**Step 7: Commit**

```bash
git add internal/goalctx/goalctx.go internal/goalctx/goalctx_test.go
git commit -m "feat: make WriteGoalJSON goal-scoped under .lead/<goalID>/"
```

---

### Task 3: Update `SpawnGoal` — append-system-prompt + goal-scoped dir + harness workflow in initial prompt

**Files:**
- Modify: `internal/setter/taskrunner.go`
- Modify: `internal/defaults/claudemd/lead.md`

**Step 1: Update `lead.md` template**

This is now the `--append-system-prompt` content. Keep it focused on identity, autonomy rules, and the DONE.json contract. The harness workflow steps go in the initial prompt (dynamic per-spawn).

```markdown
# Belayer Lead

You are operating as an autonomous lead agent managed by belayer.

## Autonomous Operation

You MUST operate fully autonomously:
- NEVER ask questions or wait for user input
- NEVER request clarification — make your best judgment and proceed
- If you encounter ambiguity, document your decision and move forward
- Use available skills, MCP tools, and harness commands as needed

## DONE.json Contract

When finished, write DONE.json in the same directory as your GOAL.json:

{
  "status": "complete",
  "summary": "Brief description of what was done",
  "files_changed": ["list", "of", "files"],
  "notes": "Any context for reviewers"
}

If you cannot complete the goal, write DONE.json with "status": "failed" and explain what blocked you.

IMPORTANT: You MUST commit and write DONE.json before your session ends.

## Mail

You can receive messages from the orchestration system.
When prompted, run `belayer mail read` to check your messages.
```

**Step 2: Update `SpawnGoal` in `taskrunner.go`**

Replace the `writeClaudeMD` call with `AppendSystemPrompt` in `SpawnOpts`. Update `WriteGoalJSON` call with goalID. Build initial prompt with explicit harness workflow steps.

```go
func (tr *TaskRunner) SpawnGoal(queued QueuedGoal) error {
	goal := queued.Goal

	// Guard: don't spawn if the goal is already running in the DAG
	if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil && dagGoal.Status == model.GoalStatusRunning {
		return nil
	}

	// If this is a retry after spotter failure, reset goal status to pending first
	if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil && dagGoal.Status == model.GoalStatusFailed {
		if err := tr.store.ResetGoalStatus(goal.ID); err != nil {
			return fmt.Errorf("resetting goal status: %w", err)
		}
		dagGoal.Status = model.GoalStatusPending
	}

	windowName := fmt.Sprintf("%s-%s", goal.RepoName, goal.ID)

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating window %s: %w", windowName, err)
	}

	// Keep pane open after process exits for death detection
	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for %s failed: %v", windowName, err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, goal.ID)
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for %s failed: %v", windowName, err)
	}

	worktreePath := tr.worktrees[goal.RepoName]

	// Write goal-scoped GOAL.json
	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", goal.ID)
	if err := goalctx.WriteGoalJSON(worktreePath, goal.ID, goalctx.LeadGoal{
		Role:            "lead",
		TaskSpec:        tr.task.Spec,
		GoalID:          goal.ID,
		RepoName:        goal.RepoName,
		Description:     goal.Description,
		Attempt:         goal.Attempt,
		SpotterFeedback: queued.SpotterFeedback,
	}); err != nil {
		return fmt.Errorf("writing GOAL.json for %s: %w", goal.ID, err)
	}

	// Load role-specific system prompt
	appendPrompt, err := defaults.FS.ReadFile("claudemd/lead.md")
	if err != nil {
		return fmt.Errorf("reading lead system prompt: %w", err)
	}

	// Set mail address
	mailAddr := fmt.Sprintf("task/%s/lead/%s/%s", tr.task.ID, goal.RepoName, goal.ID)
	if err := tr.tmux.SetEnvironment(tr.tmuxSession, "BELAYER_MAIL_ADDRESS", mailAddr); err != nil {
		log.Printf("warning: failed to set BELAYER_MAIL_ADDRESS: %v", err)
	}

	// Build initial prompt with explicit harness workflow
	initialPrompt := fmt.Sprintf(`Read %s and begin working on your assignment. Follow these steps:

1. Read %s to understand your goal and task spec
2. If this repo does not have harness docs yet, run /harness:init
3. Run /harness:plan to create an implementation plan for your goal
4. Run /harness:orchestrate to execute the plan
5. Run /harness:complete to finalize — this will reflect, review, and commit
6. Write DONE.json in the same directory as your GOAL.json when complete`, goalJSONPath, goalJSONPath)

	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      initialPrompt,
	}); err != nil {
		return fmt.Errorf("spawning agent for %s: %w", goal.ID, err)
	}

	// ... (rest unchanged: MarkRunning, UpdateGoalStatus, InsertEvent, startedAt)
}
```

**Step 3: Run tests (will fail — tests reference old behavior)**

Run: `go test ./internal/setter/ -run TestTaskRunner_SpawnGoal -v`
Expected: Compile errors from `WriteGoalJSON` signature change. Fix in Task 9.

**Step 4: Commit**

```bash
git add internal/setter/taskrunner.go internal/defaults/claudemd/lead.md
git commit -m "feat(spawn): use --append-system-prompt for lead role, harness workflow in initial prompt"
```

---

### Task 4: Update `SpawnSpotter` — own tmux window + system prompt + goal-scoped signal files

**Files:**
- Modify: `internal/setter/taskrunner.go`
- Modify: `internal/defaults/claudemd/spotter.md`

**Step 1: Update `spotter.md` template**

Same pattern as lead — remove `.lead/GOAL.json` hardcoded path, reference initial prompt for path.

**Step 2: Update `SpawnSpotter`**

Key changes:
- Create a NEW window named `spot-<goalID>` instead of reusing the lead's window
- Use `--append-system-prompt` instead of `writeClaudeMD`
- Write GOAL.json to `.lead/<goalID>/GOAL.json` (same dir as lead — spotter reads DONE.json from there too)
- Write profiles to `.lead/<goalID>/profiles/` instead of `.lead/profiles/`

```go
func (tr *TaskRunner) SpawnSpotter(goal *model.Goal) error {
	tr.dag.MarkSpotting(goal.ID)
	if err := tr.store.UpdateGoalStatus(goal.ID, model.GoalStatusSpotting); err != nil {
		return fmt.Errorf("updating goal status to spotting: %w", err)
	}

	// Kill the lead's window — it's done
	leadWindowName := fmt.Sprintf("%s-%s", goal.RepoName, goal.ID)
	tr.tmux.KillWindow(tr.tmuxSession, leadWindowName)

	// Create spotter's own window
	windowName := fmt.Sprintf("spot-%s", goal.ID)
	worktreePath := tr.worktrees[goal.RepoName]

	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating spotter window: %w", err)
	}

	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for spotter %s failed: %v", windowName, err)
	}

	// Write profiles to goal-scoped directory
	profiles, err := tr.writeProfiles(worktreePath, goal.ID)
	// ...

	// Read DONE.json from goal-scoped path
	donePath := filepath.Join(worktreePath, ".lead", goal.ID, "DONE.json")
	doneData, readErr := os.ReadFile(donePath)
	// ...

	// Write spotter GOAL.json to goal-scoped path
	if err := goalctx.WriteGoalJSON(worktreePath, goal.ID, goalctx.SpotterGoal{...}); err != nil {
		// ...
	}

	// Load system prompt
	systemPrompt, err := defaults.FS.ReadFile("claudemd/spotter.md")
	// ...

	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", goal.ID)
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(systemPrompt),
		InitialPrompt:      fmt.Sprintf("Read %s and begin validating the lead's work.", goalJSONPath),
	}); err != nil {
		// ...
	}

	// ...
}
```

**Step 3: Update `writeProfiles` signature** to accept goalID and write to `.lead/<goalID>/profiles/`

**Step 4: Update `CheckSpotResult`** window kill to use `spot-<goalID>` window name

**Step 5: Commit**

```bash
git add internal/setter/taskrunner.go internal/defaults/claudemd/spotter.md
git commit -m "feat(spotter): separate tmux window, system prompt, goal-scoped paths"
```

---

### Task 5: Update `CheckCompletions` — read DONE.json from goal-scoped path

**Files:**
- Modify: `internal/setter/taskrunner.go`

**Step 1: Update DONE.json path**

```go
// In CheckCompletions:
donePath := filepath.Join(worktreePath, ".lead", g.ID, "DONE.json")
```

**Step 2: Kill lead window when transitioning to spotting**

When `validationEnabled` is true and we spawn a spotter, now kill the lead's window since the spotter gets its own:

```go
if tr.validationEnabled {
	if spotErr := tr.SpawnSpotter(g); spotErr != nil {
		// ... (SpawnSpotter now kills the lead window internally)
	}
}
```

(SpawnSpotter already handles this in Task 4.)

**Step 3: When validation is disabled, also kill window using lead's window name**

The non-validation path already kills the window — just verify the window name is correct.

**Step 4: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "fix: read DONE.json from goal-scoped .lead/<goalID>/ path"
```

---

### Task 6: Update `CheckSpotResult` — read SPOT.json from goal-scoped path

**Files:**
- Modify: `internal/setter/taskrunner.go`

**Step 1: Update SPOT.json path**

```go
// In CheckSpotResult:
spotPath := filepath.Join(worktreePath, ".lead", goal.ID, "SPOT.json")
```

**Step 2: Update window kill to use spotter window name**

```go
windowName := fmt.Sprintf("spot-%s", goal.ID)
tr.tmux.KillWindow(tr.tmuxSession, windowName)
```

**Step 3: Update DONE.json removal on failure to use goal-scoped path**

```go
os.Remove(filepath.Join(worktreePath, ".lead", goal.ID, "DONE.json"))
```

**Step 4: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "fix: read SPOT.json from goal-scoped path, kill spotter window"
```

---

### Task 7: Update `SpawnAnchor` — system prompt, remove `writeClaudeMD`

**Files:**
- Modify: `internal/setter/taskrunner.go`
- Modify: `internal/defaults/claudemd/anchor.md`

**Step 1: Update `anchor.md` to reference initial prompt for GOAL.json path**

**Step 2: Update `SpawnAnchor`**

Replace `writeClaudeMD` call with `AppendSystemPrompt` in `SpawnOpts`. Anchor still uses `taskDir` for GOAL.json and VERDICT.json (not goal-scoped — anchor is task-level).

```go
systemPrompt, err := defaults.FS.ReadFile("claudemd/anchor.md")
if err != nil {
	return fmt.Errorf("reading anchor system prompt: %w", err)
}

// WriteGoalJSON for anchor uses a sentinel goalID like "anchor"
if err := goalctx.WriteGoalJSON(tr.taskDir, "anchor", goalctx.AnchorGoal{...}); err != nil {
	// ...
}

if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
	TmuxSession:        tr.tmuxSession,
	WindowName:         windowName,
	WorkDir:            tr.taskDir,
	AppendSystemPrompt: string(systemPrompt),
	InitialPrompt:      "Read .lead/anchor/GOAL.json and begin cross-repo review.",
}); err != nil {
	// ...
}
```

**Step 3: Update `CheckAnchorVerdict`** — VERDICT.json path. Anchor writes to taskDir root — could move to `.lead/anchor/VERDICT.json` for consistency, or leave as-is since there's only ever one anchor per task.

**Decision:** Leave VERDICT.json at `taskDir/VERDICT.json` — no collision risk, and changing it requires updating the anchor template instructions too.

**Step 4: Commit**

```bash
git add internal/setter/taskrunner.go internal/defaults/claudemd/anchor.md
git commit -m "feat(anchor): use --append-system-prompt, goal-scoped GOAL.json"
```

---

### Task 8: Delete `writeClaudeMD` method and `.orig` backup logic

**Files:**
- Modify: `internal/setter/taskrunner.go`

**Step 1: Remove `writeClaudeMD` method entirely** (lines 489-520)

**Step 2: Verify no remaining callers**

Run: `grep -rn writeClaudeMD internal/`
Expected: No results

**Step 3: Commit**

```bash
git add internal/setter/taskrunner.go
git commit -m "refactor: remove writeClaudeMD and .orig backup logic"
```

---

### Task 9: Update tests

**Files:**
- Modify: `internal/setter/setter_test.go`

**Step 1: Update `TestTaskRunner_SpawnGoal`**

- Verify `AppendSystemPrompt` is set in spawned opts
- Verify GOAL.json is at `.lead/<goalID>/GOAL.json` instead of `.lead/GOAL.json`
- Remove assertions about `.claude/CLAUDE.md` if any exist

**Step 2: Update `TestTaskRunner_CheckCompletions`**

- Write DONE.json to `.lead/<goalID>/DONE.json` instead of worktree root

**Step 3: Update `TestTaskRunner_SpawnSpotter`**

- Verify spotter gets its own window name `spot-<goalID>`
- Verify `AppendSystemPrompt` is set
- Verify lead window is killed

**Step 4: Update `TestTaskRunner_CheckSpotResult`**

- Write SPOT.json to `.lead/<goalID>/SPOT.json` instead of worktree root
- Verify `spot-<goalID>` window is killed (not `<repo>-<goalID>`)

**Step 5: Update `TestTaskRunner_SpawnAnchor`**

- Verify `AppendSystemPrompt` is set
- Verify GOAL.json at `.lead/anchor/GOAL.json`

**Step 6: Update `GatherSummaries` test**

- DONE.json path changed to `.lead/<goalID>/DONE.json`

**Step 7: Run full test suite**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 8: Commit**

```bash
git add internal/setter/setter_test.go
git commit -m "test: update all setter tests for goal-scoped isolation"
```

---

### Task 10: Update lead/spotter/anchor CLAUDE.md templates (path references)

**Files:**
- Modify: `internal/defaults/claudemd/lead.md`
- Modify: `internal/defaults/claudemd/spotter.md`
- Modify: `internal/defaults/claudemd/anchor.md`

**Step 1: Review templates for any remaining hardcoded `.lead/GOAL.json` references**

These were partially updated in Tasks 3, 4, 7. Do a final pass:
- Lead: DONE.json instructions should say "Write DONE.json in the same directory as your GOAL.json"
- Spotter: SPOT.json instructions should say "Write SPOT.json in the same directory as your GOAL.json"
- Anchor: VERDICT.json stays at worktree root

**Step 2: Verify embed still works**

Run: `go build ./cmd/belayer`
Expected: Compiles successfully

**Step 3: Commit**

```bash
git add internal/defaults/claudemd/
git commit -m "docs: update role templates for goal-scoped signal file paths"
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
