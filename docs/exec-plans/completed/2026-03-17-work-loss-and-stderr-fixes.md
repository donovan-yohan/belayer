# Work Loss Prevention & PR Stderr Capture

> **Status**: Completed | **Created**: 2026-03-17 | **Completed**: 2026-03-17
> **Bug Analysis**: `docs/bug-analyses/2026-03-17-lead-work-loss-on-cleanup-bug-analysis.md`, `docs/bug-analyses/2026-03-17-pr-creation-stderr-lost-bug-analysis.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Prevent lead work loss by pushing branches on climb completion, and capture stderr from `gh` commands for better error diagnostics.

**Architecture:** Two independent fixes: (1) Add a `git push` call in `CheckCompletions` after a climb is marked complete — uses the existing `GitRunner` interface and `tr.worktrees` map; (2) Change `runInDir` in the GitHub SCM provider to use `CombinedOutput` instead of `Output`. Plus a defense-in-depth warning in `CleanupProblemWorktrees`.

**Tech Stack:** Go, git, gh CLI

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Design | Push on climb completion, not periodically | Completion is the natural commit boundary; avoids pushing incomplete work or creating noise |
| 2026-03-17 | Design | Best-effort push (log warning, don't fail) | Push failure shouldn't block the daemon's main loop; the work is still local |
| 2026-03-17 | Design | CombinedOutput for all gh calls | Consistent with repo.go patterns; stderr is where gh writes diagnostics |

## Progress

- [x] Task 1: Push branch on climb completion _(completed 2026-03-17)_
- [x] Task 2: Capture stderr from gh commands _(completed 2026-03-17)_
- [x] Task 3: Safety check in CleanupProblemWorktrees _(completed 2026-03-17)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-17 | Task 1: Worker added nil guard on `tr.git` to prevent panic in existing tests that construct ProblemRunner without git field | No functional impact — defensive code | Accepted as improvement |

## Plan Drift

| Task | Planned | Actual | Why |
|------|---------|--------|-----|
| Task 1 | Direct push call | Added `if tr.git != nil` guard around push | Existing tests construct ProblemRunner without git field; nil dereference would break them |

---

### Task 1: Push branch on climb completion

**Files:**
- Modify: `internal/belayer/taskrunner.go:440` (CheckCompletions, after completedCount++)
- Modify: `internal/belayer/setter_test.go:139-153` (add call-tracking to mockGitRunner)
- Test: `internal/belayer/taskrunner_test.go`

- [ ] **Step 1: Add call-tracking to mockGitRunner**

In `internal/belayer/setter_test.go`, add a `calls` field to `mockGitRunner` and record calls in `Run`:

```go
type gitCall struct {
	workdir string
	args    []string
}

type mockGitRunner struct {
	responses map[string]string // key "<workdir>:<args[0]>" -> output
	calls     []gitCall
}

func newMockGitRunner() *mockGitRunner {
	return &mockGitRunner{responses: make(map[string]string)}
}

func (m *mockGitRunner) Run(workdir string, args ...string) (string, error) {
	m.calls = append(m.calls, gitCall{workdir: workdir, args: append([]string{}, args...)})
	key := workdir + ":" + args[0]
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", nil
}
```

- [ ] **Step 2: Write the failing test**

Add to `internal/belayer/taskrunner_test.go`:

```go
func TestCheckCompletions_PushesBranch(t *testing.T) {
	goals := []model.Climb{
		{ID: "climb-1", ProblemID: "push-test", RepoName: "repo-a", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusRunning},
	}
	runner, _, _, _, mg, _ := newTestRunner(t, "push-test", goals)

	// Create TOP.json for the running climb
	worktreeDir := runner.worktrees["repo-a"]
	leadDir := filepath.Join(worktreeDir, ".lead", "climb-1")
	require.NoError(t, os.MkdirAll(leadDir, 0o755))
	topJSON := `{"status":"complete","summary":"done","files_changed":["a.go"],"notes":""}`
	require.NoError(t, os.WriteFile(filepath.Join(leadDir, "TOP.json"), []byte(topJSON), 0o644))

	// Mark climb as running in the DAG (newTestRunner inserts as pending, we need running)
	runner.dag.MarkReady("climb-1")
	runner.startedAt["climb-1"] = time.Now().Add(-5 * time.Minute)
	require.NoError(t, runner.store.UpdateClimbStatus("climb-1", model.ClimbStatusRunning))
	runner.dag.Get("climb-1").Status = model.ClimbStatusRunning

	_, completed, err := runner.CheckCompletions()
	require.NoError(t, err)
	assert.Equal(t, 1, completed)

	// Verify push was called
	var pushCalls []gitCall
	for _, c := range mg.calls {
		if len(c.args) > 0 && c.args[0] == "push" {
			pushCalls = append(pushCalls, c)
		}
	}
	require.Len(t, pushCalls, 1, "expected one git push call")
	assert.Equal(t, worktreeDir, pushCalls[0].workdir)
	assert.Equal(t, []string{"push", "-u", "origin", "HEAD"}, pushCalls[0].args)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/belayer/ -run TestCheckCompletions_PushesBranch -v`
Expected: FAIL — no push call exists yet, so `pushCalls` will be empty.

- [ ] **Step 4: Add the push call in CheckCompletions**

In `internal/belayer/taskrunner.go`, after `completedCount++` (line 440) and before `windowName := ...` (line 442), add:

```go
		// Push branch to remote as a safety net — if the daemon crashes or
		// cleanup runs before HandleApproval, the work is preserved on origin.
		if _, pushErr := tr.git.Run(worktreePath, "push", "-u", "origin", "HEAD"); pushErr != nil {
			log.Printf("warning: failed to push branch for completed climb %s: %v", climb.ID, pushErr)
		}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/belayer/ -run TestCheckCompletions_PushesBranch -v`
Expected: PASS

- [ ] **Step 6: Verify existing tests still pass**

Run: `go test ./internal/belayer/ -v`
Expected: All pass. Existing tests use mockGitRunner which now records calls but still returns canned responses — no behavioral change.

- [ ] **Step 7: Commit**

```bash
git add internal/belayer/taskrunner.go internal/belayer/taskrunner_test.go internal/belayer/setter_test.go
git commit -m "fix: push branch to remote on climb completion to prevent work loss"
```

### Task 2: Capture stderr from gh commands

**Files:**
- Modify: `internal/scm/github/github.go:170-173` (runInDir)
- Modify: `internal/scm/github/github.go:188-190` (CreatePR error wrapping)
- Test: `internal/scm/github/github_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/scm/github/github_test.go`:

```go
func TestRunInDir_CapturesStderr(t *testing.T) {
	// Use a command that writes to stderr and exits non-zero
	out, err := runInDir(context.Background(), t.TempDir(), "sh", "-c", "echo 'stderr msg' >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from failing command")
	}
	// The output should contain the stderr message
	if !strings.Contains(string(out), "stderr msg") {
		t.Errorf("expected output to contain stderr, got: %q", string(out))
	}
}
```

Add the necessary imports (`context`, `strings`) if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scm/github/ -run TestRunInDir_CapturesStderr -v`
Expected: FAIL — `cmd.Output()` discards stderr, so `out` won't contain "stderr msg".

- [ ] **Step 3: Change runInDir to use CombinedOutput**

In `internal/scm/github/github.go`, change line 173:

From:
```go
	return cmd.Output()
```
To:
```go
	return cmd.CombinedOutput()
```

- [ ] **Step 4: Update CreatePR error formatting to include output**

In `internal/scm/github/github.go`, change the error at line 190:

From:
```go
		return nil, fmt.Errorf("gh pr create: %w", err)
```
To:
```go
		return nil, fmt.Errorf("gh pr create: %s: %w", strings.TrimSpace(string(out)), err)
```

Ensure `strings` is imported (it is already: used at line 194).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/scm/github/ -run TestRunInDir_CapturesStderr -v`
Expected: PASS

- [ ] **Step 6: Run all github tests**

Run: `go test ./internal/scm/github/ -v`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/scm/github/github.go internal/scm/github/github_test.go
git commit -m "fix: capture stderr from gh commands for better error diagnostics"
```

### Task 3: Safety check in CleanupProblemWorktrees

**Files:**
- Modify: `internal/repo/repo.go` (add HasUnpushedCommits)
- Modify: `internal/crag/crag.go:243-269` (CleanupProblemWorktrees)
- Test: `internal/repo/repo_test.go`
- Test: `internal/crag/crag_test.go`

- [ ] **Step 1: Write the failing test for HasUnpushedCommits**

Add to `internal/repo/repo_test.go`:

```go
func TestHasUnpushedCommits(t *testing.T) {
	bareDir := initBareRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")

	if err := WorktreeAdd(bareDir, worktreePath, "test-branch"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Fresh worktree has no new commits — should return false
	has, err := HasUnpushedCommits(worktreePath)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if has {
		t.Error("fresh worktree should have no unpushed commits")
	}

	// Make a commit in the worktree
	newFile := filepath.Join(worktreePath, "new.txt")
	if err := os.WriteFile(newFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", worktreePath, "add", "new.txt"},
		{"-C", worktreePath, "commit", "-m", "test commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	// Now should return true
	has, err = HasUnpushedCommits(worktreePath)
	if err != nil {
		t.Fatalf("HasUnpushedCommits after commit: %v", err)
	}
	if !has {
		t.Error("worktree with commits ahead should have unpushed commits")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repo/ -run TestHasUnpushedCommits -v`
Expected: FAIL — function doesn't exist yet.

- [ ] **Step 3: Implement HasUnpushedCommits**

Add to `internal/repo/repo.go`:

```go
// HasUnpushedCommits checks if the worktree's branch has commits not on the
// default branch. Returns true if there are local-only commits that would be
// lost if the worktree were removed.
func HasUnpushedCommits(worktreePath string) (bool, error) {
	baseBranch := detectBaseBranch(worktreePath)
	cmd := exec.Command("git", "-C", worktreePath, "rev-list", "--count", baseBranch+"..HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If the base branch doesn't exist in this repo, assume there could be work
		return true, nil
	}
	count := strings.TrimSpace(string(output))
	return count != "0", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/repo/ -run TestHasUnpushedCommits -v`
Expected: PASS

- [ ] **Step 5: Add warning log to CleanupProblemWorktrees**

In `internal/crag/crag.go`, in `CleanupProblemWorktrees`, add a check before the `WorktreeRemove` call. Replace the loop body (lines 251-259):

From:
```go
	for _, entry := range cfg.Repos {
		worktreePath := filepath.Join(cragDir, tasksDir, problemID, entry.Name)
		if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
			continue
		}
		bareRepoDir := filepath.Join(cragDir, entry.BarePath)
		if err := repo.WorktreeRemove(bareRepoDir, worktreePath); err != nil {
			errs = append(errs, fmt.Errorf("removing worktree for %s: %w", entry.Name, err))
		}
	}
```

To:
```go
	for _, entry := range cfg.Repos {
		worktreePath := filepath.Join(cragDir, tasksDir, problemID, entry.Name)
		if _, statErr := os.Stat(worktreePath); os.IsNotExist(statErr) {
			continue
		}
		if has, checkErr := repo.HasUnpushedCommits(worktreePath); checkErr == nil && has {
			log.Printf("warning: worktree %s has unpushed commits — work may be lost", worktreePath)
		}
		bareRepoDir := filepath.Join(cragDir, entry.BarePath)
		if err := repo.WorktreeRemove(bareRepoDir, worktreePath); err != nil {
			errs = append(errs, fmt.Errorf("removing worktree for %s: %w", entry.Name, err))
		}
	}
```

Add `"log"` to the imports in `crag.go` if not already present.

- [ ] **Step 6: Run all crag and repo tests**

Run: `go test ./internal/crag/ ./internal/repo/ -v`
Expected: All tests pass.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/repo/repo.go internal/repo/repo_test.go internal/crag/crag.go
git commit -m "fix: warn on cleanup of worktrees with unpushed commits (defense-in-depth)"
```

---

## Outcomes & Retrospective

**What worked:**
- Parallel worker dispatch — all 3 independent tasks completed simultaneously
- Plan review caught 6 blocking compilation errors before execution
- Multi-persona review caught a significant runtime bug (CombinedOutput corrupting URL parsing)

**What didn't:**
- Initial plan had wrong test APIs (testutil.NewTestStore, InsertClimb, mockTmuxManager) — needed a full review pass to correct
- CombinedOutput seemed like the obvious fix for stderr capture but created a worse bug on the success path

**Learnings to codify:**
- In Go, prefer `cmd.Output()` + `exec.ExitError.Stderr` over `cmd.CombinedOutput()` when stdout is parsed programmatically
- Always consider the success path when changing error handling — stderr warnings from CLI tools are common even on exit 0
