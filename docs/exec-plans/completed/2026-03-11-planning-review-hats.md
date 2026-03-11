# Planning & Review Hats Implementation Plan

> **Status**: Completed | **Created**: 2026-03-11 | **Completed**: 2026-03-11
> **Design Doc**: `docs/design-docs/2026-03-11-planning-review-hats-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-11 | Design | Extended daemon with plugin interfaces (Approach C) | Matches belayer's established pattern: deterministic Go orchestration, ephemeral Claude for judgment |
| 2026-03-11 | Design | Read-only tracker intake | Avoids unwanted modifications to team's tracker |
| 2026-03-11 | Design | Polling first, webhook-ready event model | Matches existing daemon pattern; event model supports webhook acceleration later |
| 2026-03-11 | Design | Stacked PRs by climb boundary | Climbs are already logical units; agentic decomposition as fallback for large single climbs |
| 2026-03-11 | Design | One CI fix loop, human-driven review loop | Prevents autonomous spiraling; human stays in control |
| 2026-03-11 | Design | `/belayer:` prefix for all setter commands | Clean namespace, avoids collisions; old names not aliased |
| 2026-03-11 | Design | `jira_ref` deprecated, not backfilled | New `tracker_issue_id` supersedes it |
| 2026-03-11 | Design | CI fix cap is per-PR | Each PR independently tracks fix attempts; relevant for stacked PRs |
| 2026-03-11 | Design | Crag-level tracker config | Different crags may use different trackers |
| 2026-03-11 | Retrospective | Plan completed | 19/19 tasks, 7 surprises, 7 review fixes (3 critical). Full planning & review hats implemented. |

## Progress

- [x] Task 1: Schema migration & new model types _(completed 2026-03-11)_
- [x] Task 2: Store CRUD for tracker_issues _(completed 2026-03-11)_
- [x] Task 3: Store CRUD for pull_requests and pr_reactions _(completed 2026-03-11)_
- [x] Task 4: Config extensions (tracker + review sections) _(completed 2026-03-11)_
- [x] Task 5: Tracker interface & GitHub implementation _(completed 2026-03-11)_
- [x] Task 6: Tracker spec assembly agentic node _(completed 2026-03-11)_
- [x] Task 7: Tracker sync in daemon tick loop _(completed 2026-03-11)_
- [x] Task 8: Tracker CLI commands _(completed 2026-03-11)_
- [x] Task 9: SCM Provider interface & GitHub implementation _(completed 2026-03-11)_
- [x] Task 10: PR stacking logic _(completed 2026-03-11)_
- [x] Task 11: PR body generation agentic node _(completed 2026-03-11)_
- [x] Task 12: Refactor existing createPR to use SCM provider _(completed 2026-03-11)_
- [x] Task 13: Reaction engine core _(completed 2026-03-11)_
- [x] Task 14: CI fix dispatch _(completed 2026-03-11)_
- [x] Task 15: Review comment & changes-requested handling _(completed 2026-03-11)_
- [x] Task 16: PR monitoring in daemon tick loop _(completed 2026-03-11)_
- [x] Task 17: Extended problem lifecycle states _(completed 2026-03-11)_
- [x] Task 18: PR CLI commands _(completed 2026-03-11)_
- [x] Task 19: Setter session extensions _(completed 2026-03-11)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-11 | Worker-5 committed worker-6's specassembly files | Task 6 found its work already committed | No action needed — files were correct |
| 2026-03-11 | tracker_issues.problem_id FK constraint rejects empty string | Store needed sql.NullString for unlinked issues | Worker-2 used sql.NullString + scanTrackerIssue helper |
| 2026-03-11 | GitHub CI checks can have NEUTRAL/SKIPPED conclusions | SCM provider needed to treat these as success-equivalent | Worker-9 added NEUTRAL+SKIPPED alongside SUCCESS |
| 2026-03-11 | tick() signature changed to accept context.Context | Existing setter_test.go calls broke (18 call sites) | Orchestrator fixed: replaced .tick() with .tick(context.Background()) + nil guard on belayerCfg |
| 2026-03-11 | GitHub tracker passed bare filesystem path to `gh -R` | All tracker CLI commands would fail at runtime | Review fix: added `repo.OwnerRepoFromURL()` helper, pass `owner/repo` format |
| 2026-03-11 | `ReplyToComment` used non-existent GitHub API endpoint | PR review replies would always 404 | Review fix: corrected to `pulls/{pr}/comments/{id}/replies` |
| 2026-03-11 | `Review.State` not lowercased from GitHub API | Reaction engine would never detect `changes_requested`/`approved` | Review fix: added `strings.ToLower()` in both parse functions |

## Plan Drift

| Task | Plan said | Actually happened | Why |
|------|-----------|-------------------|-----|
| Task 18 | Inline resolve/load/open in each subcommand | Extracted `loadPRDeps` helper | Consistent with `loadTrackerDeps` pattern in tracker_cmd.go |

---

## File Structure

**New files:**

| File | Responsibility |
|------|---------------|
| `internal/db/migrations/004_planning_review_hats.sql` | Schema migration: new tables, new columns, new indexes |
| `internal/model/tracker.go` | `Issue`, `IssueFilter`, `Comment`, `LinkedIssue` types |
| `internal/model/scm.go` | `PR`, `PRStatus`, `PRActivity`, `PROptions`, `PRSplit`, `Check`, `Review`, `ReviewComment`, `CITransition` types |
| `internal/model/review.go` | `PullRequest` (DB row), `PRReaction` (DB row) types |
| `internal/tracker/tracker.go` | `Tracker` interface definition |
| `internal/tracker/github/github.go` | GitHub Issues implementation via `gh` CLI |
| `internal/tracker/jira/jira.go` | Jira implementation stub (REST API v3) |
| `internal/scm/scm.go` | `SCMProvider` interface definition |
| `internal/scm/github/github.go` | GitHub PR implementation via `gh` CLI |
| `internal/review/engine.go` | Reaction engine: event detection, dispatch logic, state transitions |
| `internal/cli/tracker_cmd.go` | `belayer tracker sync/list/show` commands |
| `internal/cli/pr_cmd.go` | `belayer pr list/show/retry` commands |
| `internal/defaults/commands/ticket.md` | Setter `/belayer:ticket` command |
| `internal/defaults/commands/ticket-list.md` | Setter `/belayer:ticket-list` command |
| `internal/defaults/commands/sync.md` | Setter `/belayer:sync` command |
| `internal/defaults/commands/prs.md` | Setter `/belayer:prs` command |
| `internal/defaults/commands/pr.md` | Setter `/belayer:pr` command |

**Modified files:**

| File | Changes |
|------|---------|
| `internal/model/types.go` | New problem statuses, new event types |
| `internal/store/store.go` | CRUD for `tracker_issues`, `pull_requests`, `pr_reactions`; extend `GetActiveProblems` |
| `internal/belayerconfig/config.go` | `TrackerConfig`, `ReviewConfig`, `PRConfig` structs |
| `internal/defaults/belayer.toml` | Default `[tracker]`, `[review]`, `[pr]` sections |
| `internal/belayer/belayer.go` | New tick phases: tracker sync, PR monitoring |
| `internal/belayer/taskrunner.go` | `HandleApproval` uses SCM provider; new PR-lifecycle methods |
| `internal/cli/root.go` | Register `tracker` and `pr` command groups |
| `internal/cli/problem.go` | `--ticket` flag on `problem create` |
| `internal/defaults/claudemd/setter.md` | Awareness of tracker, PR monitoring, new commands |
| `internal/defaults/defaults.go` | Embed new command files |

**Test files (co-located):**

| File | Tests |
|------|-------|
| `internal/store/store_test.go` | New CRUD operations, migration validation |
| `internal/tracker/github/github_test.go` | `gh` CLI output parsing |
| `internal/scm/github/github_test.go` | PR creation, status parsing, stacking |
| `internal/review/engine_test.go` | Event detection, dispatch decisions, state transitions |
| `internal/belayer/belayer_test.go` | New tick phases integration |

---

## Chunk 1: Foundation

### Task 1: Schema migration & new model types

**Files:**
- Create: `internal/db/migrations/004_planning_review_hats.sql`
- Create: `internal/model/tracker.go`
- Create: `internal/model/scm.go`
- Create: `internal/model/review.go`
- Modify: `internal/model/types.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write the migration SQL**

Create `internal/db/migrations/004_planning_review_hats.sql`:

```sql
-- New tables for planning & review hats

CREATE TABLE tracker_issues (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    comments_json TEXT,
    labels_json TEXT,
    priority TEXT,
    assignee TEXT,
    url TEXT,
    raw_json TEXT,
    problem_id TEXT,
    synced_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

CREATE TABLE pull_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    problem_id TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    url TEXT NOT NULL,
    stack_position INTEGER DEFAULT 1,
    stack_size INTEGER DEFAULT 1,
    ci_status TEXT DEFAULT 'pending',
    ci_fix_count INTEGER DEFAULT 0,
    review_status TEXT DEFAULT 'pending',
    state TEXT DEFAULT 'open',
    last_polled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

CREATE TABLE pr_reactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id INTEGER NOT NULL,
    trigger_type TEXT NOT NULL,
    trigger_payload TEXT,
    action_taken TEXT NOT NULL,
    lead_id TEXT,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (pr_id) REFERENCES pull_requests(id)
);

-- Add tracker_issue_id to problems (nullable FK)
ALTER TABLE problems ADD COLUMN tracker_issue_id TEXT REFERENCES tracker_issues(id);

-- Indexes
CREATE INDEX idx_tracker_issues_problem ON tracker_issues(problem_id);
CREATE INDEX idx_pull_requests_problem ON pull_requests(problem_id);
CREATE INDEX idx_pull_requests_state ON pull_requests(state);
CREATE INDEX idx_pr_reactions_pr ON pr_reactions(pr_id);
```

- [ ] **Step 2: Add new problem statuses and event types to model/types.go**

Add to `ProblemStatus` constants:

```go
ProblemStatusImported     ProblemStatus = "imported"
ProblemStatusEnriching    ProblemStatus = "enriching"
ProblemStatusPRCreating   ProblemStatus = "pr_creating"
ProblemStatusPRMonitoring ProblemStatus = "pr_monitoring"
ProblemStatusCIFixing     ProblemStatus = "ci_fixing"
ProblemStatusReviewReacting ProblemStatus = "review_reacting"
ProblemStatusMerged       ProblemStatus = "merged"
ProblemStatusClosed       ProblemStatus = "closed"
```

Add to `EventType` constants (note: `EventPRCreated` already exists — do NOT re-add it):

```go
EventIssueImported         EventType = "issue_imported"
EventIssueConverted        EventType = "issue_converted"
EventPRStacked             EventType = "pr_stacked"
EventCIFailed              EventType = "ci_failed"
EventCIFixDispatched       EventType = "ci_fix_dispatched"
EventCIFixSucceeded        EventType = "ci_fix_succeeded"
EventCIFixExhausted        EventType = "ci_fix_exhausted"
EventReviewCommentReceived EventType = "review_comment_received"
EventReviewCommentReplied  EventType = "review_comment_replied"
EventChangesRequested      EventType = "changes_requested"
EventReviewReactionDispatched EventType = "review_reaction_dispatched"
EventPRApproved            EventType = "pr_approved"
EventPRMerged              EventType = "pr_merged"
EventPRClosed              EventType = "pr_closed"
```

Add `TrackerIssueID` field to `Problem` struct:

```go
TrackerIssueID string `json:"tracker_issue_id"`
```

- [ ] **Step 3: Create model/tracker.go with tracker types**

```go
package model

// Issue is the tracker-agnostic representation of a work item.
type Issue struct {
    ID           string            `json:"id"`
    Title        string            `json:"title"`
    Body         string            `json:"body"`
    Comments     []Comment         `json:"comments"`
    Labels       []string          `json:"labels"`
    Priority     string            `json:"priority"`
    Assignee     string            `json:"assignee"`
    LinkedIssues []LinkedIssue     `json:"linked_issues"`
    URL          string            `json:"url"`
    Raw          map[string]any    `json:"raw"`
}

// Comment represents a discussion comment on a tracker issue.
type Comment struct {
    Author string `json:"author"`
    Body   string `json:"body"`
    Date   string `json:"date"`
}

// LinkedIssue represents a relationship to another issue.
type LinkedIssue struct {
    ID       string `json:"id"`
    Type     string `json:"type"` // parent, blocker, related
    Title    string `json:"title"`
}

// IssueFilter controls which issues are fetched from the tracker.
type IssueFilter struct {
    Labels   []string `json:"labels"`
    Assignee string   `json:"assignee"`
    Sprint   string   `json:"sprint"`
    Status   []string `json:"status"`
}
```

- [ ] **Step 4: Create model/scm.go with SCM types**

```go
package model

import "time"

// PROptions configures PR creation.
type PROptions struct {
    Title      string `json:"title"`
    Body       string `json:"body"`
    BaseBranch string `json:"base_branch"`
    Draft      bool   `json:"draft"`
}

// PRSplit defines a slice of commits for one PR in a stack.
type PRSplit struct {
    Title         string   `json:"title"`
    Body          string   `json:"body"`
    Commits       []string `json:"commits"`
    StackPosition int      `json:"stack_position"`
}

// PRStatus represents the current state of a PR.
type PRStatus struct {
    Number    int      `json:"number"`
    State     string   `json:"state"`     // open, merged, closed
    CIStatus  string   `json:"ci_status"` // passing, failing, pending
    CIDetails []Check  `json:"ci_details"`
    Reviews   []Review `json:"reviews"`
    Mergeable bool     `json:"mergeable"`
    URL       string   `json:"url"`
}

// Check represents an individual CI check.
type Check struct {
    Name   string `json:"name"`
    Status string `json:"status"` // success, failure, pending
}

// Review represents a PR review.
type Review struct {
    Author string `json:"author"`
    State  string `json:"state"` // approved, changes_requested, commented
    Body   string `json:"body"`
}

// PRActivity represents new activity on a PR since a given time.
type PRActivity struct {
    Comments      []ReviewComment  `json:"comments"`
    Reviews       []Review         `json:"reviews"`
    CITransitions []CITransition   `json:"ci_transitions"`
}

// ReviewComment represents a review comment on a PR.
type ReviewComment struct {
    ID     int64  `json:"id"`
    Author string `json:"author"`
    Body   string `json:"body"`
    Path   string `json:"path"`
    Line   int    `json:"line"`
}

// CITransition represents a CI status change.
type CITransition struct {
    CheckName string `json:"check_name"`
    From      string `json:"from"`
    To        string `json:"to"`
    Time      time.Time `json:"time"`
}
```

- [ ] **Step 5: Create model/review.go with DB row types**

```go
package model

import "time"

// PullRequest represents a row in the pull_requests table.
type PullRequest struct {
    ID            int64     `json:"id"`
    ProblemID     string    `json:"problem_id"`
    RepoName      string    `json:"repo_name"`
    PRNumber      int       `json:"pr_number"`
    URL           string    `json:"url"`
    StackPosition int       `json:"stack_position"`
    StackSize     int       `json:"stack_size"`
    CIStatus      string    `json:"ci_status"`
    CIFixCount    int       `json:"ci_fix_count"`
    ReviewStatus  string    `json:"review_status"`
    State         string    `json:"state"`
    LastPolledAt  *time.Time `json:"last_polled_at"`
    CreatedAt     time.Time `json:"created_at"`
}

// PRReaction represents a row in the pr_reactions table.
type PRReaction struct {
    ID             int64     `json:"id"`
    PRID           int64     `json:"pr_id"`
    TriggerType    string    `json:"trigger_type"`
    TriggerPayload string    `json:"trigger_payload"`
    ActionTaken    string    `json:"action_taken"`
    LeadID         string    `json:"lead_id"`
    CreatedAt      time.Time `json:"created_at"`
}
```

- [ ] **Step 6: Write migration test**

Add to `internal/store/store_test.go` (or create a new `internal/db/db_test.go`):

```go
func TestMigration004_PlanningReviewHats(t *testing.T) {
    db := testutil.OpenTestDB(t)

    // Verify new tables exist
    tables := []string{"tracker_issues", "pull_requests", "pr_reactions"}
    for _, table := range tables {
        var name string
        err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
        if err != nil {
            t.Fatalf("table %s not found: %v", table, err)
        }
    }

    // Verify tracker_issue_id column on problems
    var cid int
    err := db.QueryRow("SELECT cid FROM pragma_table_info('problems') WHERE name='tracker_issue_id'").Scan(&cid)
    if err != nil {
        t.Fatal("tracker_issue_id column not found on problems table")
    }
}
```

- [ ] **Step 7: Run tests to verify migration applies cleanly**

Run: `go test ./internal/db/... -run TestMigration004 -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/db/migrations/004_planning_review_hats.sql internal/model/tracker.go internal/model/scm.go internal/model/review.go internal/model/types.go internal/store/store_test.go
git commit -m "feat: add schema migration 004 and model types for planning & review hats"
```

### Task 2: Store CRUD for tracker_issues

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests for tracker issue CRUD**

```go
func TestInsertTrackerIssue(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    issue := &model.TrackerIssue{
        ID:       "ENG-1234",
        Provider: "jira",
        Title:    "Fix login bug",
        Body:     "The login page crashes",
        SyncedAt: time.Now().UTC(),
    }
    err := s.InsertTrackerIssue(issue)
    if err != nil {
        t.Fatalf("InsertTrackerIssue: %v", err)
    }

    got, err := s.GetTrackerIssue("ENG-1234")
    if err != nil {
        t.Fatalf("GetTrackerIssue: %v", err)
    }
    if got.Title != "Fix login bug" {
        t.Errorf("Title = %q, want %q", got.Title, "Fix login bug")
    }
}

func TestListTrackerIssuesUnlinked(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    // Insert two issues, one linked to a problem, one not
    s.InsertTrackerIssue(&model.TrackerIssue{ID: "ENG-1", Provider: "github", Title: "A", SyncedAt: time.Now().UTC()})
    s.InsertTrackerIssue(&model.TrackerIssue{ID: "ENG-2", Provider: "github", Title: "B", SyncedAt: time.Now().UTC(), ProblemID: "prob-1"})

    issues, err := s.ListTrackerIssues(true) // unlinked only
    if err != nil {
        t.Fatalf("ListTrackerIssues: %v", err)
    }
    if len(issues) != 1 || issues[0].ID != "ENG-1" {
        t.Errorf("expected 1 unlinked issue ENG-1, got %d issues", len(issues))
    }
}

func TestLinkTrackerIssueToProblem(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    s.InsertTrackerIssue(&model.TrackerIssue{ID: "ENG-5", Provider: "github", Title: "X", SyncedAt: time.Now().UTC()})
    err := s.LinkTrackerIssueToProblem("ENG-5", "prob-123")
    if err != nil {
        t.Fatalf("LinkTrackerIssueToProblem: %v", err)
    }

    issue, _ := s.GetTrackerIssue("ENG-5")
    if issue.ProblemID != "prob-123" {
        t.Errorf("ProblemID = %q, want %q", issue.ProblemID, "prob-123")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -run TestInsertTrackerIssue -v`
Expected: FAIL — methods don't exist

- [ ] **Step 3: Add TrackerIssue DB row type to model/review.go**

```go
// TrackerIssue represents a row in the tracker_issues table.
type TrackerIssue struct {
    ID           string    `json:"id"`
    Provider     string    `json:"provider"`
    Title        string    `json:"title"`
    Body         string    `json:"body"`
    CommentsJSON string    `json:"comments_json"`
    LabelsJSON   string    `json:"labels_json"`
    Priority     string    `json:"priority"`
    Assignee     string    `json:"assignee"`
    URL          string    `json:"url"`
    RawJSON      string    `json:"raw_json"`
    ProblemID    string    `json:"problem_id"`
    SyncedAt     time.Time `json:"synced_at"`
}
```

- [ ] **Step 4: Implement store CRUD for tracker_issues**

Add to `internal/store/store.go`:

```go
func (s *Store) InsertTrackerIssue(issue *model.TrackerIssue) error {
    _, err := s.db.Exec(
        `INSERT OR REPLACE INTO tracker_issues (id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        issue.ID, issue.Provider, issue.Title, issue.Body, issue.CommentsJSON, issue.LabelsJSON,
        issue.Priority, issue.Assignee, issue.URL, issue.RawJSON, issue.ProblemID, issue.SyncedAt,
    )
    return err
}

func (s *Store) GetTrackerIssue(id string) (*model.TrackerIssue, error) {
    row := s.db.QueryRow(
        `SELECT id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at
         FROM tracker_issues WHERE id = ?`, id,
    )
    ti := &model.TrackerIssue{}
    err := row.Scan(&ti.ID, &ti.Provider, &ti.Title, &ti.Body, &ti.CommentsJSON, &ti.LabelsJSON,
        &ti.Priority, &ti.Assignee, &ti.URL, &ti.RawJSON, &ti.ProblemID, &ti.SyncedAt)
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("tracker issue %q not found", id)
    }
    return ti, err
}

func (s *Store) ListTrackerIssues(unlinkedOnly bool) ([]model.TrackerIssue, error) {
    query := `SELECT id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at FROM tracker_issues`
    if unlinkedOnly {
        query += ` WHERE problem_id IS NULL OR problem_id = ''`
    }
    query += ` ORDER BY synced_at DESC`

    rows, err := s.db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var issues []model.TrackerIssue
    for rows.Next() {
        var ti model.TrackerIssue
        if err := rows.Scan(&ti.ID, &ti.Provider, &ti.Title, &ti.Body, &ti.CommentsJSON, &ti.LabelsJSON,
            &ti.Priority, &ti.Assignee, &ti.URL, &ti.RawJSON, &ti.ProblemID, &ti.SyncedAt); err != nil {
            return nil, err
        }
        issues = append(issues, ti)
    }
    return issues, rows.Err()
}

func (s *Store) LinkTrackerIssueToProblem(issueID, problemID string) error {
    _, err := s.db.Exec(`UPDATE tracker_issues SET problem_id = ? WHERE id = ?`, problemID, issueID)
    return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/... -run "TestInsertTrackerIssue|TestListTrackerIssues|TestLinkTrackerIssue" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/model/review.go internal/store/store.go internal/store/store_test.go
git commit -m "feat: add store CRUD for tracker_issues table"
```

### Task 3: Store CRUD for pull_requests and pr_reactions

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests for pull_requests CRUD**

```go
func TestInsertPullRequest(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    // Need a problem first (FK constraint)
    setupTestProblem(t, s, "prob-1", "crag-1")

    pr := &model.PullRequest{
        ProblemID:     "prob-1",
        RepoName:      "frontend",
        PRNumber:      42,
        URL:           "https://github.com/org/frontend/pull/42",
        StackPosition: 1,
        StackSize:     1,
        CreatedAt:     time.Now().UTC(),
    }
    id, err := s.InsertPullRequest(pr)
    if err != nil {
        t.Fatalf("InsertPullRequest: %v", err)
    }
    if id == 0 {
        t.Fatal("expected non-zero ID")
    }

    got, err := s.GetPullRequest(id)
    if err != nil {
        t.Fatalf("GetPullRequest: %v", err)
    }
    if got.PRNumber != 42 {
        t.Errorf("PRNumber = %d, want 42", got.PRNumber)
    }
    if got.CIStatus != "pending" {
        t.Errorf("CIStatus = %q, want %q", got.CIStatus, "pending")
    }
}

func TestListPullRequestsForProblem(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    setupTestProblem(t, s, "prob-1", "crag-1")

    s.InsertPullRequest(&model.PullRequest{ProblemID: "prob-1", RepoName: "api", PRNumber: 10, URL: "u1", CreatedAt: time.Now().UTC()})
    s.InsertPullRequest(&model.PullRequest{ProblemID: "prob-1", RepoName: "web", PRNumber: 11, URL: "u2", CreatedAt: time.Now().UTC()})

    prs, err := s.ListPullRequestsForProblem("prob-1")
    if err != nil {
        t.Fatalf("ListPullRequestsForProblem: %v", err)
    }
    if len(prs) != 2 {
        t.Fatalf("expected 2 PRs, got %d", len(prs))
    }
}

func TestUpdatePullRequestStatus(t *testing.T) {
    db := testutil.OpenTestDB(t)
    s := store.New(db)

    setupTestProblem(t, s, "prob-1", "crag-1")

    id, _ := s.InsertPullRequest(&model.PullRequest{ProblemID: "prob-1", RepoName: "api", PRNumber: 10, URL: "u", CreatedAt: time.Now().UTC()})

    err := s.UpdatePullRequestCI(id, "failing", 1)
    if err != nil {
        t.Fatalf("UpdatePullRequestCI: %v", err)
    }

    got, _ := s.GetPullRequest(id)
    if got.CIStatus != "failing" || got.CIFixCount != 1 {
        t.Errorf("CI = %q count=%d, want failing/1", got.CIStatus, got.CIFixCount)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -run TestInsertPullRequest -v`
Expected: FAIL

- [ ] **Step 3: Implement pull_requests CRUD**

Add to `internal/store/store.go`:

```go
func (s *Store) InsertPullRequest(pr *model.PullRequest) (int64, error) {
    result, err := s.db.Exec(
        `INSERT INTO pull_requests (problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        pr.ProblemID, pr.RepoName, pr.PRNumber, pr.URL, pr.StackPosition, pr.StackSize,
        pr.CIStatus, pr.CIFixCount, pr.ReviewStatus, pr.State, pr.CreatedAt,
    )
    if err != nil {
        return 0, err
    }
    return result.LastInsertId()
}

func (s *Store) GetPullRequest(id int64) (*model.PullRequest, error) {
    row := s.db.QueryRow(
        `SELECT id, problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, last_polled_at, created_at
         FROM pull_requests WHERE id = ?`, id,
    )
    pr := &model.PullRequest{}
    err := row.Scan(&pr.ID, &pr.ProblemID, &pr.RepoName, &pr.PRNumber, &pr.URL,
        &pr.StackPosition, &pr.StackSize, &pr.CIStatus, &pr.CIFixCount,
        &pr.ReviewStatus, &pr.State, &pr.LastPolledAt, &pr.CreatedAt)
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("pull request %d not found", id)
    }
    return pr, err
}

func (s *Store) ListPullRequestsForProblem(problemID string) ([]model.PullRequest, error) {
    rows, err := s.db.Query(
        `SELECT id, problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, last_polled_at, created_at
         FROM pull_requests WHERE problem_id = ? ORDER BY stack_position ASC`, problemID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var prs []model.PullRequest
    for rows.Next() {
        var pr model.PullRequest
        if err := rows.Scan(&pr.ID, &pr.ProblemID, &pr.RepoName, &pr.PRNumber, &pr.URL,
            &pr.StackPosition, &pr.StackSize, &pr.CIStatus, &pr.CIFixCount,
            &pr.ReviewStatus, &pr.State, &pr.LastPolledAt, &pr.CreatedAt); err != nil {
            return nil, err
        }
        prs = append(prs, pr)
    }
    return prs, rows.Err()
}

func (s *Store) ListMonitoredPullRequests(cragID string) ([]model.PullRequest, error) {
    rows, err := s.db.Query(
        `SELECT pr.id, pr.problem_id, pr.repo_name, pr.pr_number, pr.url, pr.stack_position, pr.stack_size, pr.ci_status, pr.ci_fix_count, pr.review_status, pr.state, pr.last_polled_at, pr.created_at
         FROM pull_requests pr
         JOIN problems p ON pr.problem_id = p.id
         WHERE p.crag_id = ? AND pr.state = 'open'
         ORDER BY pr.created_at ASC`, cragID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var prs []model.PullRequest
    for rows.Next() {
        var pr model.PullRequest
        if err := rows.Scan(&pr.ID, &pr.ProblemID, &pr.RepoName, &pr.PRNumber, &pr.URL,
            &pr.StackPosition, &pr.StackSize, &pr.CIStatus, &pr.CIFixCount,
            &pr.ReviewStatus, &pr.State, &pr.LastPolledAt, &pr.CreatedAt); err != nil {
            return nil, err
        }
        prs = append(prs, pr)
    }
    return prs, rows.Err()
}

func (s *Store) UpdatePullRequestCI(id int64, ciStatus string, ciFixCount int) error {
    _, err := s.db.Exec(
        `UPDATE pull_requests SET ci_status = ?, ci_fix_count = ?, last_polled_at = ? WHERE id = ?`,
        ciStatus, ciFixCount, time.Now().UTC(), id,
    )
    return err
}

func (s *Store) UpdatePullRequestReview(id int64, reviewStatus string) error {
    _, err := s.db.Exec(
        `UPDATE pull_requests SET review_status = ?, last_polled_at = ? WHERE id = ?`,
        reviewStatus, time.Now().UTC(), id,
    )
    return err
}

func (s *Store) UpdatePullRequestState(id int64, state string) error {
    _, err := s.db.Exec(
        `UPDATE pull_requests SET state = ?, last_polled_at = ? WHERE id = ?`,
        state, time.Now().UTC(), id,
    )
    return err
}

func (s *Store) InsertPRReaction(reaction *model.PRReaction) error {
    _, err := s.db.Exec(
        `INSERT INTO pr_reactions (pr_id, trigger_type, trigger_payload, action_taken, lead_id, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
        reaction.PRID, reaction.TriggerType, reaction.TriggerPayload, reaction.ActionTaken,
        reaction.LeadID, time.Now().UTC(),
    )
    return err
}

func (s *Store) ListPRReactions(prID int64) ([]model.PRReaction, error) {
    rows, err := s.db.Query(
        `SELECT id, pr_id, trigger_type, trigger_payload, action_taken, lead_id, created_at
         FROM pr_reactions WHERE pr_id = ? ORDER BY created_at ASC`, prID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var reactions []model.PRReaction
    for rows.Next() {
        var r model.PRReaction
        if err := rows.Scan(&r.ID, &r.PRID, &r.TriggerType, &r.TriggerPayload, &r.ActionTaken, &r.LeadID, &r.CreatedAt); err != nil {
            return nil, err
        }
        reactions = append(reactions, r)
    }
    return reactions, rows.Err()
}
```

- [ ] **Step 4: Update Problem scan helpers to include tracker_issue_id**

All `Scan` calls for `Problem` in `store.go` need to include the new `TrackerIssueID` column. Update the scan list in `GetProblem`, `ListProblemsForCrag`, `GetProblemsByStatus`, `GetPendingProblems`, `GetActiveProblems` to include `tracker_issue_id` in the SELECT and Scan.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat: add store CRUD for pull_requests, pr_reactions, and tracker_issue_id"
```

### Task 4: Config extensions (tracker + review sections)

**Files:**
- Modify: `internal/belayerconfig/config.go`
- Modify: `internal/defaults/belayer.toml`
- Test: `internal/belayerconfig/config_test.go`

- [ ] **Step 1: Write failing test for new config sections**

```go
func TestLoadConfig_TrackerAndReviewDefaults(t *testing.T) {
    cfg, err := belayerconfig.Load("", "")
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    if cfg.Tracker.Label != "belayer" {
        t.Errorf("Tracker.Label = %q, want %q", cfg.Tracker.Label, "belayer")
    }
    if cfg.Review.CIFixAttempts != 2 {
        t.Errorf("Review.CIFixAttempts = %d, want 2", cfg.Review.CIFixAttempts)
    }
    if cfg.PR.StackThreshold != 1000 {
        t.Errorf("PR.StackThreshold = %d, want 1000", cfg.PR.StackThreshold)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/belayerconfig/... -run TestLoadConfig_TrackerAndReview -v`
Expected: FAIL

- [ ] **Step 3: Add config structs**

Add to `internal/belayerconfig/config.go`:

```go
type TrackerConfig struct {
    Provider     string             `toml:"provider"`
    Label        string             `toml:"label"`
    SyncInterval string             `toml:"sync_interval"`
    GitHub       TrackerGitHubConfig `toml:"github"`
    Jira         TrackerJiraConfig  `toml:"jira"`
}

type TrackerGitHubConfig struct {
    // uses gh CLI auth — no extra config needed
}

type TrackerJiraConfig struct {
    BaseURL string `toml:"base_url"`
    Project string `toml:"project"`
    // auth via JIRA_API_TOKEN env var
}

type ReviewConfig struct {
    PollInterval  string `toml:"poll_interval"`
    CIFixAttempts int    `toml:"ci_fix_attempts"`
    AutoMerge     bool   `toml:"auto_merge"`
}

type PRConfig struct {
    StackThreshold int  `toml:"stack_threshold"`
    Draft          bool `toml:"draft"`
}
```

Add fields to `Config` struct:

```go
Tracker TrackerConfig `toml:"tracker"`
Review  ReviewConfig  `toml:"review"`
PR      PRConfig      `toml:"pr"`
```

- [ ] **Step 4: Update embedded defaults belayer.toml**

Append to `internal/defaults/belayer.toml`:

```toml
[tracker]
provider = "github"
label = "belayer"
sync_interval = "24h"

[tracker.github]

[tracker.jira]
base_url = ""
project = ""

[review]
poll_interval = "60s"
ci_fix_attempts = 2
auto_merge = false

[pr]
stack_threshold = 1000
draft = true
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/belayerconfig/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/belayerconfig/config.go internal/defaults/belayer.toml internal/belayerconfig/config_test.go
git commit -m "feat: add tracker, review, and PR config sections"
```

---

## Chunk 2: Tracker Plugin

### Task 5: Tracker interface & GitHub implementation

**Files:**
- Create: `internal/tracker/tracker.go`
- Create: `internal/tracker/github/github.go`
- Create: `internal/tracker/jira/jira.go`
- Test: `internal/tracker/github/github_test.go`

- [ ] **Step 1: Create the Tracker interface**

```go
// internal/tracker/tracker.go
package tracker

import (
    "context"

    "github.com/donovan-yohan/belayer/internal/model"
)

// Tracker pulls issues from external trackers.
type Tracker interface {
    // ListIssues returns issues matching the filter.
    ListIssues(ctx context.Context, filter model.IssueFilter) ([]model.Issue, error)

    // GetIssue fetches a single issue by its tracker-native ID.
    GetIssue(ctx context.Context, id string) (*model.Issue, error)
}
```

- [ ] **Step 2: Write failing test for GitHub tracker output parsing**

The GitHub implementation shells out to `gh`. Test the JSON parsing logic by extracting it into a testable function.

```go
// internal/tracker/github/github_test.go
package github

import "testing"

func TestParseGHIssueJSON(t *testing.T) {
    raw := `{
        "number": 42,
        "title": "Fix login bug",
        "body": "The login page crashes",
        "labels": [{"name": "belayer"}, {"name": "bug"}],
        "assignees": [{"login": "alice"}],
        "comments": [{"author": {"login": "bob"}, "body": "I can repro", "createdAt": "2026-03-10T10:00:00Z"}],
        "url": "https://github.com/org/repo/issues/42"
    }`
    issue, err := parseGHIssueJSON([]byte(raw))
    if err != nil {
        t.Fatalf("parseGHIssueJSON: %v", err)
    }
    if issue.ID != "#42" {
        t.Errorf("ID = %q, want %q", issue.ID, "#42")
    }
    if issue.Title != "Fix login bug" {
        t.Errorf("Title = %q, want %q", issue.Title, "Fix login bug")
    }
    if len(issue.Labels) != 2 {
        t.Errorf("len(Labels) = %d, want 2", len(issue.Labels))
    }
    if len(issue.Comments) != 1 {
        t.Errorf("len(Comments) = %d, want 1", len(issue.Comments))
    }
    if issue.Assignee != "alice" {
        t.Errorf("Assignee = %q, want %q", issue.Assignee, "alice")
    }
}

func TestParseGHIssueListJSON(t *testing.T) {
    raw := `[
        {"number": 1, "title": "A", "body": "", "labels": [{"name": "belayer"}], "assignees": [], "comments": [], "url": "u1"},
        {"number": 2, "title": "B", "body": "", "labels": [{"name": "belayer"}], "assignees": [], "comments": [], "url": "u2"}
    ]`
    issues, err := parseGHIssueListJSON([]byte(raw))
    if err != nil {
        t.Fatalf("parseGHIssueListJSON: %v", err)
    }
    if len(issues) != 2 {
        t.Errorf("len(issues) = %d, want 2", len(issues))
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tracker/github/... -v`
Expected: FAIL

- [ ] **Step 4: Implement GitHub tracker**

```go
// internal/tracker/github/github.go
package github

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"

    "github.com/donovan-yohan/belayer/internal/model"
)

// Tracker implements tracker.Tracker for GitHub Issues via gh CLI.
type Tracker struct {
    // RepoDir is the working directory for gh commands (determines which repo).
    RepoDir string
}

func New(repoDir string) *Tracker {
    return &Tracker{RepoDir: repoDir}
}

func (t *Tracker) ListIssues(ctx context.Context, filter model.IssueFilter) ([]model.Issue, error) {
    args := []string{"issue", "list", "--json", "number,title,body,labels,assignees,comments,url", "--limit", "100"}
    for _, label := range filter.Labels {
        args = append(args, "--label", label)
    }
    if filter.Assignee != "" {
        args = append(args, "--assignee", filter.Assignee)
    }

    out, err := t.runGH(ctx, args...)
    if err != nil {
        return nil, fmt.Errorf("gh issue list: %w", err)
    }
    return parseGHIssueListJSON(out)
}

func (t *Tracker) GetIssue(ctx context.Context, id string) (*model.Issue, error) {
    // Strip "#" prefix if present
    id = strings.TrimPrefix(id, "#")

    out, err := t.runGH(ctx, "issue", "view", id, "--json", "number,title,body,labels,assignees,comments,url")
    if err != nil {
        return nil, fmt.Errorf("gh issue view %s: %w", id, err)
    }
    return parseGHIssueJSON(out)
}

func (t *Tracker) runGH(ctx context.Context, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "gh", args...)
    cmd.Dir = t.RepoDir
    return cmd.Output()
}

// ghIssue is the JSON shape returned by gh CLI.
type ghIssue struct {
    Number   int        `json:"number"`
    Title    string     `json:"title"`
    Body     string     `json:"body"`
    Labels   []ghLabel  `json:"labels"`
    Assignees []ghUser  `json:"assignees"`
    Comments []ghComment `json:"comments"`
    URL      string     `json:"url"`
}

type ghLabel struct {
    Name string `json:"name"`
}

type ghUser struct {
    Login string `json:"login"`
}

type ghComment struct {
    Author    ghUser `json:"author"`
    Body      string `json:"body"`
    CreatedAt string `json:"createdAt"`
}

func parseGHIssueJSON(data []byte) (*model.Issue, error) {
    var gi ghIssue
    if err := json.Unmarshal(data, &gi); err != nil {
        return nil, err
    }
    return convertGHIssue(&gi), nil
}

func parseGHIssueListJSON(data []byte) ([]model.Issue, error) {
    var gis []ghIssue
    if err := json.Unmarshal(data, &gis); err != nil {
        return nil, err
    }
    issues := make([]model.Issue, len(gis))
    for i := range gis {
        issues[i] = *convertGHIssue(&gis[i])
    }
    return issues, nil
}

func convertGHIssue(gi *ghIssue) *model.Issue {
    labels := make([]string, len(gi.Labels))
    for i, l := range gi.Labels {
        labels[i] = l.Name
    }

    comments := make([]model.Comment, len(gi.Comments))
    for i, c := range gi.Comments {
        comments[i] = model.Comment{
            Author: c.Author.Login,
            Body:   c.Body,
            Date:   c.CreatedAt,
        }
    }

    var assignee string
    if len(gi.Assignees) > 0 {
        assignee = gi.Assignees[0].Login
    }

    return &model.Issue{
        ID:       fmt.Sprintf("#%d", gi.Number),
        Title:    gi.Title,
        Body:     gi.Body,
        Labels:   labels,
        Comments: comments,
        Assignee: assignee,
        URL:      gi.URL,
    }
}
```

- [ ] **Step 5: Create Jira tracker stub**

```go
// internal/tracker/jira/jira.go
package jira

import (
    "context"
    "fmt"

    "github.com/donovan-yohan/belayer/internal/model"
)

// Tracker implements tracker.Tracker for Jira via REST API v3.
type Tracker struct {
    BaseURL string
    Project string
    Token   string // from JIRA_API_TOKEN env var
}

func New(baseURL, project, token string) *Tracker {
    return &Tracker{BaseURL: baseURL, Project: project, Token: token}
}

func (t *Tracker) ListIssues(ctx context.Context, filter model.IssueFilter) ([]model.Issue, error) {
    return nil, fmt.Errorf("jira tracker not yet implemented")
}

func (t *Tracker) GetIssue(ctx context.Context, id string) (*model.Issue, error) {
    return nil, fmt.Errorf("jira tracker not yet implemented")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tracker/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tracker/
git commit -m "feat: add Tracker interface with GitHub implementation and Jira stub"
```

### Task 6: Tracker spec assembly agentic node

**Files:**
- Create: `internal/tracker/specassembly.go`
- Test: `internal/tracker/specassembly_test.go`

The spec assembly node is an agentic Claude session that converts a tracker `Issue` into a problem spec + suggested climbs. It uses the same pattern as existing agentic nodes: write a prompt, spawn via `ClaudeSpawner`, parse output.

However, unlike lead/spotter/anchor which run in tmux windows and use signal files, spec assembly is a **blocking** call — the daemon waits for the result. This means it should use `claude -p` (non-interactive mode) similar to how the existing agentic system was originally built.

- [ ] **Step 1: Write test for prompt construction**

```go
func TestBuildSpecAssemblyPrompt(t *testing.T) {
    issue := &model.Issue{
        ID:    "ENG-1234",
        Title: "Fix login page crash",
        Body:  "The login page crashes when...",
        Comments: []model.Comment{
            {Author: "alice", Body: "I can reproduce this"},
        },
    }
    repos := []string{"frontend", "api"}

    prompt := BuildSpecAssemblyPrompt(issue, repos)

    if !strings.Contains(prompt, "ENG-1234") {
        t.Error("prompt should contain issue ID")
    }
    if !strings.Contains(prompt, "frontend") {
        t.Error("prompt should contain repo names")
    }
    if !strings.Contains(prompt, "spec.md") {
        t.Error("prompt should reference spec.md output format")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/... -run TestBuildSpecAssemblyPrompt -v`
Expected: FAIL

- [ ] **Step 3: Implement spec assembly prompt builder and output parser**

```go
// internal/tracker/specassembly.go
package tracker

import (
    "encoding/json"
    "fmt"
    "strings"

    "github.com/donovan-yohan/belayer/internal/model"
)

// SpecAssemblyOutput is the expected JSON output from the spec assembly agentic node.
type SpecAssemblyOutput struct {
    Spec   string          `json:"spec"`   // markdown problem spec
    Climbs model.ClimbsFile `json:"climbs"` // suggested climbs
}

// BuildSpecAssemblyPrompt constructs the prompt for the spec assembly agentic node.
func BuildSpecAssemblyPrompt(issue *model.Issue, repoNames []string) string {
    var b strings.Builder
    b.WriteString("You are a spec assembly agent. Convert the following tracker issue into a belayer problem spec and suggested climbs.\n\n")
    b.WriteString("## Tracker Issue\n\n")
    b.WriteString(fmt.Sprintf("**ID:** %s\n", issue.ID))
    b.WriteString(fmt.Sprintf("**Title:** %s\n", issue.Title))
    b.WriteString(fmt.Sprintf("**Body:**\n%s\n\n", issue.Body))

    if len(issue.Comments) > 0 {
        b.WriteString("**Discussion:**\n")
        for _, c := range issue.Comments {
            b.WriteString(fmt.Sprintf("- @%s: %s\n", c.Author, c.Body))
        }
        b.WriteString("\n")
    }

    b.WriteString(fmt.Sprintf("**Available repos:** %s\n\n", strings.Join(repoNames, ", ")))

    b.WriteString("## Output Format\n\n")
    b.WriteString("Respond with a single JSON object (no markdown fences):\n\n")
    b.WriteString("```\n")
    b.WriteString(`{"spec": "<markdown problem spec (spec.md content)>", "climbs": {"repos": {"<repo>": {"climbs": [{"id": "<id>", "description": "<desc>", "depends_on": []}]}}}}`)
    b.WriteString("\n```\n\n")
    b.WriteString("The spec should be a clear, actionable problem description suitable for autonomous coding agents.\n")
    b.WriteString("Each climb should be a self-contained unit of work in a single repo.\n")
    b.WriteString("Use short kebab-case IDs like `fix-login-crash` for climb IDs.\n")

    return b.String()
}

// ParseSpecAssemblyOutput parses the agentic node's JSON output.
func ParseSpecAssemblyOutput(raw string) (*SpecAssemblyOutput, error) {
    // Strip markdown fences if present (same pattern as StripMarkdownJSON)
    cleaned := strings.TrimSpace(raw)
    if strings.HasPrefix(cleaned, "```") {
        lines := strings.Split(cleaned, "\n")
        if len(lines) >= 3 {
            cleaned = strings.Join(lines[1:len(lines)-1], "\n")
        }
    }

    var out SpecAssemblyOutput
    if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
        return nil, fmt.Errorf("parsing spec assembly output: %w", err)
    }
    return &out, nil
}
```

- [ ] **Step 4: Write test for output parsing**

```go
func TestParseSpecAssemblyOutput(t *testing.T) {
    raw := `{"spec": "# Fix Login\n\nThe login page crashes.", "climbs": {"repos": {"frontend": {"climbs": [{"id": "fix-login", "description": "Fix crash", "depends_on": []}]}}}}`

    out, err := ParseSpecAssemblyOutput(raw)
    if err != nil {
        t.Fatalf("ParseSpecAssemblyOutput: %v", err)
    }
    if !strings.Contains(out.Spec, "Fix Login") {
        t.Error("spec should contain title")
    }
    if _, ok := out.Climbs.Repos["frontend"]; !ok {
        t.Error("climbs should have frontend repo")
    }
}
```

- [ ] **Step 5: Run all tracker tests**

Run: `go test ./internal/tracker/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tracker/specassembly.go internal/tracker/specassembly_test.go
git commit -m "feat: add spec assembly prompt builder and output parser for tracker intake"
```

### Task 7: Tracker sync in daemon tick loop

**Files:**
- Modify: `internal/belayer/belayer.go`
- Test: `internal/belayer/belayer_test.go`

- [ ] **Step 1: Write test for tracker sync phase**

```go
func TestTrackerSyncPhase(t *testing.T) {
    // Test that the tracker sync phase:
    // 1. Respects sync_interval (doesn't sync on every tick)
    // 2. Calls tracker.ListIssues with configured label filter
    // 3. Inserts new issues into tracker_issues table
    // 4. Skips issues already in tracker_issues
    // 5. Creates problems for issues with auto_import enabled (future)
}
```

This is a complex integration test. The key is testing that `syncTracker()` correctly:
- Checks elapsed time against `sync_interval`
- Calls the tracker interface
- Upserts results into the store

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/belayer/... -run TestTrackerSync -v`
Expected: FAIL

- [ ] **Step 3: Add tracker sync fields to Belayer struct**

Add to `internal/belayer/belayer.go`:

```go
// In Belayer struct:
tracker      tracker.Tracker  // nil if not configured
lastSyncAt   time.Time
syncInterval time.Duration
```

- [ ] **Step 4: Implement syncTracker method**

```go
func (s *Belayer) syncTracker() {
    if s.tracker == nil {
        return
    }
    if time.Since(s.lastSyncAt) < s.syncInterval {
        return
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    filter := model.IssueFilter{
        Labels: []string{s.cfg.Tracker.Label},
    }
    issues, err := s.tracker.ListIssues(ctx, filter)
    if err != nil {
        log.Printf("tracker sync: %v", err)
        return
    }

    for _, issue := range issues {
        commentsJSON, _ := json.Marshal(issue.Comments)
        labelsJSON, _ := json.Marshal(issue.Labels)
        rawJSON, _ := json.Marshal(issue.Raw)

        ti := &model.TrackerIssue{
            ID:           issue.ID,
            Provider:     s.cfg.Tracker.Provider,
            Title:        issue.Title,
            Body:         issue.Body,
            CommentsJSON: string(commentsJSON),
            LabelsJSON:   string(labelsJSON),
            Priority:     issue.Priority,
            Assignee:     issue.Assignee,
            URL:          issue.URL,
            RawJSON:      string(rawJSON),
            SyncedAt:     time.Now().UTC(),
        }
        if err := s.store.InsertTrackerIssue(ti); err != nil {
            log.Printf("tracker sync: inserting issue %s: %v", issue.ID, err)
        }
    }

    s.lastSyncAt = time.Now()
    log.Printf("tracker sync: imported %d issues", len(issues))
}
```

- [ ] **Step 5: Add syncTracker call to tick()**

In `tick()`, add as a new phase before `pollPendingProblems`:

```go
// Phase 0 — Tracker sync (respects interval internally)
s.syncTracker()
```

- [ ] **Step 6: Initialize tracker in Belayer constructor based on config**

Add tracker initialization logic that creates the appropriate `Tracker` implementation based on `cfg.Tracker.Provider`:

```go
func (s *Belayer) initTracker() {
    switch s.cfg.Tracker.Provider {
    case "github":
        // GitHub tracker needs a repo dir — use first repo's bare path
        if len(s.cragConfig.Repos) > 0 {
            s.tracker = ghtracker.New(filepath.Join(s.cragDir, s.cragConfig.Repos[0].BarePath))
        }
    case "jira":
        token := os.Getenv("JIRA_API_TOKEN")
        s.tracker = jiratracker.New(s.cfg.Tracker.Jira.BaseURL, s.cfg.Tracker.Jira.Project, token)
    }

    interval, err := time.ParseDuration(s.cfg.Tracker.SyncInterval)
    if err != nil {
        interval = 24 * time.Hour
    }
    s.syncInterval = interval
}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/belayer/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/belayer/belayer.go internal/belayer/belayer_test.go
git commit -m "feat: add tracker sync phase to daemon tick loop"
```

### Task 8: Tracker CLI commands

**Files:**
- Create: `internal/cli/tracker_cmd.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/problem.go`

- [ ] **Step 1: Create tracker command group**

```go
// internal/cli/tracker_cmd.go
package cli

import (
    "context"
    "fmt"
    "os"
    "text/tabwriter"
    "time"

    "github.com/spf13/cobra"
    "github.com/donovan-yohan/belayer/internal/belayerconfig"
    "github.com/donovan-yohan/belayer/internal/db"
    "github.com/donovan-yohan/belayer/internal/model"
    "github.com/donovan-yohan/belayer/internal/store"
    "github.com/donovan-yohan/belayer/internal/tracker/github"
)

func newTrackerCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "tracker",
        Short: "Manage tracker integrations",
    }
    cmd.AddCommand(newTrackerSyncCmd())
    cmd.AddCommand(newTrackerListCmd())
    cmd.AddCommand(newTrackerShowCmd())
    return cmd
}

func newTrackerSyncCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "sync",
        Short: "Fetch issues from tracker and import",
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            _, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }

            cfg, err := belayerconfig.Load("", cragDir)
            if err != nil {
                return err
            }

            t, err := createTracker(cfg, cragDir)
            if err != nil {
                return err
            }

            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()

            filter := model.IssueFilter{Labels: []string{cfg.Tracker.Label}}
            issues, err := t.ListIssues(ctx, filter)
            if err != nil {
                return fmt.Errorf("fetching issues: %w", err)
            }

            database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
            if err != nil {
                return err
            }
            defer database.Close()
            s := store.New(database)

            for _, issue := range issues {
                ti := issueToTrackerIssue(issue, cfg.Tracker.Provider)
                if err := s.InsertTrackerIssue(ti); err != nil {
                    fmt.Fprintf(os.Stderr, "warning: %s: %v\n", issue.ID, err)
                }
            }

            fmt.Printf("Synced %d issues from %s\n", len(issues), cfg.Tracker.Provider)
            return nil
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}

func newTrackerListCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "list",
        Short: "Preview matching issues without importing (dry run)",
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            _, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }

            cfg, err := belayerconfig.Load("", cragDir)
            if err != nil {
                return err
            }

            t, err := createTracker(cfg, cragDir)
            if err != nil {
                return err
            }

            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()

            filter := model.IssueFilter{Labels: []string{cfg.Tracker.Label}}
            issues, err := t.ListIssues(ctx, filter)
            if err != nil {
                return err
            }

            w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
            fmt.Fprintln(w, "ID\tTITLE\tPRIORITY\tASSIGNEE")
            for _, issue := range issues {
                fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", issue.ID, issue.Title, issue.Priority, issue.Assignee)
            }
            return w.Flush()
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}

func newTrackerShowCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "show <id>",
        Short: "Fetch and display a single issue",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            _, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }

            cfg, err := belayerconfig.Load("", cragDir)
            if err != nil {
                return err
            }

            t, err := createTracker(cfg, cragDir)
            if err != nil {
                return err
            }

            ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
            defer cancel()

            issue, err := t.GetIssue(ctx, args[0])
            if err != nil {
                return err
            }

            fmt.Printf("ID:       %s\n", issue.ID)
            fmt.Printf("Title:    %s\n", issue.Title)
            fmt.Printf("Priority: %s\n", issue.Priority)
            fmt.Printf("Assignee: %s\n", issue.Assignee)
            fmt.Printf("URL:      %s\n", issue.URL)
            if issue.Body != "" {
                fmt.Printf("\n%s\n", issue.Body)
            }
            return nil
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}
```

- [ ] **Step 2: Add `--ticket` flag to `problem create`**

In `internal/cli/problem.go`, add to the `problem create` command:

```go
var ticketID string
cmd.Flags().StringVar(&ticketID, "ticket", "", "Tracker issue ID to fetch and use as spec")
```

When `--ticket` is provided, fetch the issue, run spec assembly, and create the problem from the output instead of requiring spec.md + climbs.json files.

- [ ] **Step 3: Register tracker command in root.go**

Add to `NewRootCmd()`:

```go
rootCmd.AddCommand(newTrackerCmd())
```

- [ ] **Step 4: Run build to verify compilation**

Run: `go build ./cmd/belayer`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/cli/tracker_cmd.go internal/cli/root.go internal/cli/problem.go
git commit -m "feat: add tracker CLI commands (sync, list, show) and --ticket flag"
```

---

## Chunk 3: SCM Provider & PR Creation

### Task 9: SCM Provider interface & GitHub implementation

**Files:**
- Create: `internal/scm/scm.go`
- Create: `internal/scm/github/github.go`
- Test: `internal/scm/github/github_test.go`

- [ ] **Step 1: Create the SCMProvider interface**

```go
// internal/scm/scm.go
package scm

import (
    "context"
    "time"

    "github.com/donovan-yohan/belayer/internal/model"
)

// SCMProvider handles PR lifecycle operations.
type SCMProvider interface {
    CreatePR(ctx context.Context, repoDir string, opts model.PROptions) (*model.PRStatus, error)
    CreateStackedPRs(ctx context.Context, repoDir string, splits []model.PRSplit) ([]*model.PRStatus, error)
    GetPRStatus(ctx context.Context, repoDir string, prNumber int) (*model.PRStatus, error)
    GetNewActivity(ctx context.Context, repoDir string, prNumber int, since time.Time) (*model.PRActivity, error)
    ReplyToComment(ctx context.Context, repoDir string, prNumber int, commentID int64, body string) error
    Merge(ctx context.Context, repoDir string, prNumber int) error
}
```

- [ ] **Step 2: Write failing test for GitHub PR status parsing**

```go
// internal/scm/github/github_test.go
package github

import "testing"

func TestParseGHPRStatusJSON(t *testing.T) {
    raw := `{
        "number": 42,
        "state": "OPEN",
        "url": "https://github.com/org/repo/pull/42",
        "mergeable": "MERGEABLE",
        "statusCheckRollup": [
            {"name": "tests", "status": "COMPLETED", "conclusion": "SUCCESS"},
            {"name": "lint", "status": "COMPLETED", "conclusion": "FAILURE"}
        ],
        "reviews": [
            {"author": {"login": "alice"}, "state": "APPROVED", "body": "LGTM"}
        ]
    }`
    status, err := parseGHPRStatusJSON([]byte(raw))
    if err != nil {
        t.Fatalf("parseGHPRStatusJSON: %v", err)
    }
    if status.Number != 42 {
        t.Errorf("Number = %d, want 42", status.Number)
    }
    if status.State != "open" {
        t.Errorf("State = %q, want %q", status.State, "open")
    }
    if status.CIStatus != "failing" {
        t.Errorf("CIStatus = %q, want %q", status.CIStatus, "failing")
    }
    if !status.Mergeable {
        t.Error("Mergeable should be true")
    }
    if len(status.Reviews) != 1 || status.Reviews[0].State != "approved" {
        t.Error("expected 1 approved review")
    }
}

func TestParseGHPRActivityJSON(t *testing.T) {
    raw := `{
        "comments": [
            {"id": 1, "author": {"login": "bob"}, "body": "Needs fix", "path": "main.go", "line": 42}
        ],
        "reviews": [
            {"author": {"login": "bob"}, "state": "CHANGES_REQUESTED", "body": "See comments"}
        ]
    }`
    activity, err := parseGHPRActivityJSON([]byte(raw))
    if err != nil {
        t.Fatalf("parseGHPRActivityJSON: %v", err)
    }
    if len(activity.Comments) != 1 {
        t.Errorf("len(Comments) = %d, want 1", len(activity.Comments))
    }
    if len(activity.Reviews) != 1 {
        t.Errorf("len(Reviews) = %d, want 1", len(activity.Reviews))
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/scm/github/... -v`
Expected: FAIL

- [ ] **Step 4: Implement GitHub SCM provider**

```go
// internal/scm/github/github.go
package github

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"
    "time"

    "github.com/donovan-yohan/belayer/internal/model"
)

// Provider implements scm.SCMProvider for GitHub via gh CLI.
type Provider struct{}

func New() *Provider {
    return &Provider{}
}

func (p *Provider) CreatePR(ctx context.Context, repoDir string, opts model.PROptions) (*model.PRStatus, error) {
    args := []string{"pr", "create", "--title", opts.Title, "--body", opts.Body}
    if opts.BaseBranch != "" {
        args = append(args, "--base", opts.BaseBranch)
    }
    if opts.Draft {
        args = append(args, "--draft")
    }

    cmd := exec.CommandContext(ctx, "gh", args...)
    cmd.Dir = repoDir
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("gh pr create: %s: %w", strings.TrimSpace(string(out)), err)
    }

    // gh pr create outputs the PR URL
    url := strings.TrimSpace(string(out))

    // Extract PR number from URL (last path segment)
    parts := strings.Split(url, "/")
    var number int
    fmt.Sscanf(parts[len(parts)-1], "%d", &number)

    return &model.PRStatus{
        Number: number,
        State:  "open",
        URL:    url,
    }, nil
}

func (p *Provider) CreateStackedPRs(ctx context.Context, repoDir string, splits []model.PRSplit) ([]*model.PRStatus, error) {
    // Implemented in Task 10 (PR stacking)
    return nil, fmt.Errorf("stacked PRs not yet implemented")
}

func (p *Provider) GetPRStatus(ctx context.Context, repoDir string, prNumber int) (*model.PRStatus, error) {
    out, err := runGH(ctx, repoDir, "pr", "view", fmt.Sprintf("%d", prNumber),
        "--json", "number,state,url,mergeable,statusCheckRollup,reviews")
    if err != nil {
        return nil, err
    }
    return parseGHPRStatusJSON(out)
}

func (p *Provider) GetNewActivity(ctx context.Context, repoDir string, prNumber int, since time.Time) (*model.PRActivity, error) {
    // Get comments and reviews via gh api
    out, err := runGH(ctx, repoDir, "api",
        fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber),
        "--jq", fmt.Sprintf(`[.[] | select(.created_at > "%s")]`, since.Format(time.RFC3339)))
    if err != nil {
        return nil, fmt.Errorf("fetching PR comments: %w", err)
    }

    reviewsOut, err := runGH(ctx, repoDir, "api",
        fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNumber),
        "--jq", fmt.Sprintf(`[.[] | select(.submitted_at > "%s")]`, since.Format(time.RFC3339)))
    if err != nil {
        return nil, fmt.Errorf("fetching PR reviews: %w", err)
    }

    activity := &model.PRActivity{}

    // Parse comments
    var ghComments []ghPRComment
    if len(out) > 2 { // not empty array "[]"
        if err := json.Unmarshal(out, &ghComments); err == nil {
            for _, c := range ghComments {
                activity.Comments = append(activity.Comments, model.ReviewComment{
                    ID:     c.ID,
                    Author: c.User.Login,
                    Body:   c.Body,
                    Path:   c.Path,
                    Line:   c.Line,
                })
            }
        }
    }

    // Parse reviews
    var ghReviews []ghReview
    if len(reviewsOut) > 2 {
        if err := json.Unmarshal(reviewsOut, &ghReviews); err == nil {
            for _, r := range ghReviews {
                activity.Reviews = append(activity.Reviews, model.Review{
                    Author: r.User.Login,
                    State:  strings.ToLower(r.State),
                    Body:   r.Body,
                })
            }
        }
    }

    return activity, nil
}

func (p *Provider) ReplyToComment(ctx context.Context, repoDir string, prNumber int, commentID int64, body string) error {
    _, err := runGH(ctx, repoDir, "api",
        fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments/%d/replies", prNumber, commentID),
        "-f", fmt.Sprintf("body=%s", body))
    return err
}

func (p *Provider) Merge(ctx context.Context, repoDir string, prNumber int) error {
    _, err := runGH(ctx, repoDir, "pr", "merge", fmt.Sprintf("%d", prNumber), "--merge")
    return err
}

func runGH(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "gh", args...)
    cmd.Dir = repoDir
    return cmd.Output()
}

// JSON types for gh CLI output parsing

type ghPRStatus struct {
    Number            int              `json:"number"`
    State             string           `json:"state"`
    URL               string           `json:"url"`
    Mergeable         string           `json:"mergeable"`
    StatusCheckRollup []ghStatusCheck  `json:"statusCheckRollup"`
    Reviews           []ghReview       `json:"reviews"`
}

type ghStatusCheck struct {
    Name       string `json:"name"`
    Status     string `json:"status"`
    Conclusion string `json:"conclusion"`
}

type ghReview struct {
    User  ghUserRef `json:"author"`
    State string    `json:"state"`
    Body  string    `json:"body"`
}

type ghUserRef struct {
    Login string `json:"login"`
}

type ghPRComment struct {
    ID   int64     `json:"id"`
    User ghUserRef `json:"user"`
    Body string    `json:"body"`
    Path string    `json:"path"`
    Line int       `json:"line"`
}

func parseGHPRStatusJSON(data []byte) (*model.PRStatus, error) {
    var gs ghPRStatus
    if err := json.Unmarshal(data, &gs); err != nil {
        return nil, err
    }

    // Determine overall CI status
    ciStatus := "pending"
    allPassing := true
    hasFailure := false
    for _, check := range gs.StatusCheckRollup {
        if check.Status != "COMPLETED" {
            allPassing = false
        } else if check.Conclusion != "SUCCESS" {
            hasFailure = true
            allPassing = false
        }
    }
    if hasFailure {
        ciStatus = "failing"
    } else if allPassing && len(gs.StatusCheckRollup) > 0 {
        ciStatus = "passing"
    }

    // Convert checks
    checks := make([]model.Check, len(gs.StatusCheckRollup))
    for i, c := range gs.StatusCheckRollup {
        status := "pending"
        if c.Status == "COMPLETED" {
            if c.Conclusion == "SUCCESS" {
                status = "success"
            } else {
                status = "failure"
            }
        }
        checks[i] = model.Check{Name: c.Name, Status: status}
    }

    // Convert reviews
    reviews := make([]model.Review, len(gs.Reviews))
    for i, r := range gs.Reviews {
        reviews[i] = model.Review{
            Author: r.User.Login,
            State:  strings.ToLower(r.State),
            Body:   r.Body,
        }
    }

    return &model.PRStatus{
        Number:    gs.Number,
        State:     strings.ToLower(gs.State),
        CIStatus:  ciStatus,
        CIDetails: checks,
        Reviews:   reviews,
        Mergeable: gs.Mergeable == "MERGEABLE",
        URL:       gs.URL,
    }, nil
}

func parseGHPRActivityJSON(data []byte) (*model.PRActivity, error) {
    var raw struct {
        Comments []ghPRComment `json:"comments"`
        Reviews  []ghReview    `json:"reviews"`
    }
    if err := json.Unmarshal(data, &raw); err != nil {
        return nil, err
    }

    activity := &model.PRActivity{}
    for _, c := range raw.Comments {
        activity.Comments = append(activity.Comments, model.ReviewComment{
            ID:     c.ID,
            Author: c.User.Login,
            Body:   c.Body,
            Path:   c.Path,
            Line:   c.Line,
        })
    }
    for _, r := range raw.Reviews {
        activity.Reviews = append(activity.Reviews, model.Review{
            Author: r.User.Login,
            State:  strings.ToLower(r.State),
            Body:   r.Body,
        })
    }
    return activity, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/scm/github/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/scm/
git commit -m "feat: add SCMProvider interface with GitHub implementation"
```

### Task 10: PR stacking logic

**Files:**
- Modify: `internal/scm/github/github.go`
- Create: `internal/scm/stacking.go`
- Test: `internal/scm/stacking_test.go`
- Test: `internal/scm/github/github_test.go`

- [ ] **Step 1: Write test for diff size calculation**

```go
// internal/scm/stacking_test.go
func TestCalculateStackSplits(t *testing.T) {
    // 3 climbs: 400, 800, 300 lines changed (threshold 1000)
    // Expected: climb1+climb3 in PR1 (700 lines), climb2 in PR2 (800 lines)
    climbs := []ClimbDiff{
        {ClimbID: "c1", RepoName: "api", LinesChanged: 400},
        {ClimbID: "c2", RepoName: "api", LinesChanged: 800},
        {ClimbID: "c3", RepoName: "api", LinesChanged: 300},
    }

    splits := CalculateStackSplits(climbs, 1000)
    if len(splits) != 2 {
        t.Fatalf("expected 2 splits, got %d", len(splits))
    }
}

func TestCalculateStackSplits_UnderThreshold(t *testing.T) {
    climbs := []ClimbDiff{
        {ClimbID: "c1", RepoName: "api", LinesChanged: 200},
        {ClimbID: "c2", RepoName: "api", LinesChanged: 300},
    }

    splits := CalculateStackSplits(climbs, 1000)
    if len(splits) != 1 {
        t.Fatalf("expected 1 split (no stacking needed), got %d", len(splits))
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scm/... -run TestCalculateStackSplits -v`
Expected: FAIL

- [ ] **Step 3: Implement stacking logic**

```go
// internal/scm/stacking.go
package scm

import "github.com/donovan-yohan/belayer/internal/model"

// ClimbDiff represents the diff size of a single climb.
type ClimbDiff struct {
    ClimbID      string
    RepoName     string
    LinesChanged int
    Commits      []string // commit SHAs
}

// CalculateStackSplits groups climbs into PR-sized chunks under the threshold.
func CalculateStackSplits(climbs []ClimbDiff, threshold int) []model.PRSplit {
    total := 0
    for _, c := range climbs {
        total += c.LinesChanged
    }

    // Under threshold: single PR
    if total <= threshold {
        split := model.PRSplit{StackPosition: 1}
        for _, c := range climbs {
            split.Commits = append(split.Commits, c.Commits...)
        }
        return []model.PRSplit{split}
    }

    // Greedy bin-packing: add climbs to current split until threshold exceeded
    var splits []model.PRSplit
    current := model.PRSplit{StackPosition: 1}
    currentSize := 0

    for _, c := range climbs {
        if currentSize+c.LinesChanged > threshold && currentSize > 0 {
            splits = append(splits, current)
            current = model.PRSplit{StackPosition: len(splits) + 1}
            currentSize = 0
        }
        current.Commits = append(current.Commits, c.Commits...)
        currentSize += c.LinesChanged
    }
    if len(current.Commits) > 0 {
        splits = append(splits, current)
    }

    return splits
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/scm/... -v`
Expected: PASS

- [ ] **Step 5: Implement CreateStackedPRs in GitHub provider**

Update `internal/scm/github/github.go` to implement `CreateStackedPRs`:

```go
func (p *Provider) CreateStackedPRs(ctx context.Context, repoDir string, splits []model.PRSplit) ([]*model.PRStatus, error) {
    var results []*model.PRStatus
    prevBranch := "" // empty means target main/master

    for i, split := range splits {
        // Create branch for this stack slice
        branchName := fmt.Sprintf("belayer/stack-%d", i+1)

        opts := model.PROptions{
            Title:      split.Title,
            Body:       split.Body,
            BaseBranch: prevBranch,
            Draft:      true,
        }

        status, err := p.CreatePR(ctx, repoDir, opts)
        if err != nil {
            return results, fmt.Errorf("creating stacked PR %d: %w", i+1, err)
        }
        results = append(results, status)
        prevBranch = branchName
    }

    return results, nil
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/scm/
git commit -m "feat: add PR stacking logic and CreateStackedPRs implementation"
```

### Task 11: PR body generation agentic node

**Files:**
- Create: `internal/scm/prbodygen.go`
- Test: `internal/scm/prbodygen_test.go`

- [ ] **Step 1: Write test for PR body prompt construction**

```go
func TestBuildPRBodyPrompt(t *testing.T) {
    prompt := BuildPRBodyPrompt("Fix login crash", "api", []string{"auth.go", "handler.go"}, "Fixed null pointer in auth middleware")
    if !strings.Contains(prompt, "Fix login crash") {
        t.Error("should contain problem spec")
    }
    if !strings.Contains(prompt, "auth.go") {
        t.Error("should contain changed files")
    }
}

func TestParsePRBodyOutput(t *testing.T) {
    raw := `{"title": "[belayer] Fix login crash: api", "body": "## Summary\n\nFixed null pointer in auth middleware.\n\n## Changes\n- auth.go: Added nil check\n- handler.go: Updated error handling"}`
    title, body, err := ParsePRBodyOutput(raw)
    if err != nil {
        t.Fatal(err)
    }
    if title == "" || body == "" {
        t.Error("title and body should not be empty")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scm/... -run TestBuildPRBody -v`
Expected: FAIL

- [ ] **Step 3: Implement PR body generation**

```go
// internal/scm/prbodygen.go
package scm

import (
    "encoding/json"
    "fmt"
    "strings"
)

type PRBodyOutput struct {
    Title string `json:"title"`
    Body  string `json:"body"`
}

func BuildPRBodyPrompt(problemSpec, repoName string, filesChanged []string, climbSummaries string) string {
    var b strings.Builder
    b.WriteString("Generate a PR title and body for the following changes.\n\n")
    b.WriteString("## Problem Spec\n\n")
    b.WriteString(problemSpec)
    b.WriteString("\n\n## Repository\n\n")
    b.WriteString(repoName)
    b.WriteString("\n\n## Files Changed\n\n")
    for _, f := range filesChanged {
        b.WriteString(fmt.Sprintf("- %s\n", f))
    }
    b.WriteString("\n## Climb Summaries\n\n")
    b.WriteString(climbSummaries)
    b.WriteString("\n\n## Output Format\n\n")
    b.WriteString("Respond with a single JSON object (no markdown fences):\n")
    b.WriteString(`{"title": "<short PR title under 72 chars>", "body": "<markdown PR body with ## Summary and ## Changes sections>"}`)
    b.WriteString("\n")
    return b.String()
}

func ParsePRBodyOutput(raw string) (title, body string, err error) {
    cleaned := strings.TrimSpace(raw)
    if strings.HasPrefix(cleaned, "```") {
        lines := strings.Split(cleaned, "\n")
        if len(lines) >= 3 {
            cleaned = strings.Join(lines[1:len(lines)-1], "\n")
        }
    }

    var out PRBodyOutput
    if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
        return "", "", fmt.Errorf("parsing PR body output: %w", err)
    }
    return out.Title, out.Body, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/scm/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scm/prbodygen.go internal/scm/prbodygen_test.go
git commit -m "feat: add PR body generation prompt builder and parser"
```

### Task 12: Refactor existing createPR to use SCM provider

**Files:**
- Modify: `internal/belayer/taskrunner.go`
- Test: `internal/belayer/taskrunner_test.go`

- [ ] **Step 1: Add SCMProvider field to ProblemRunner**

Add `scm scm.SCMProvider` field to `ProblemRunner` struct and inject it during initialization.

- [ ] **Step 2: Refactor HandleApproval to use SCM provider**

Replace the inline `createPR` method with calls to `scm.CreatePR()`. The new flow:
1. Count total diff lines per repo
2. If under `stack_threshold`: single PR via `scm.CreatePR()`
3. If over: calculate splits, create stacked PRs
4. Insert `PullRequest` records into the store
5. Transition problem to `pr_monitoring`

```go
func (tr *ProblemRunner) HandleApproval() error {
    var firstErr error
    for repoName, worktreePath := range tr.worktrees {
        // Push branch
        branchName := fmt.Sprintf("belayer/problem-%s/%s", tr.task.ID, repoName)
        if _, err := tr.git.Run(worktreePath, "push", "-u", "origin", "HEAD:"+branchName); err != nil {
            if firstErr == nil {
                firstErr = fmt.Errorf("pushing %s: %w", repoName, err)
            }
            continue
        }

        // Create PR via SCM provider
        opts := model.PROptions{
            Title: fmt.Sprintf("[belayer] Problem %s: %s", tr.task.ID, repoName),
            Body:  fmt.Sprintf("Problem: %s\nRepo: %s", tr.task.ID, repoName),
            Draft: tr.prConfig.Draft,
        }

        ctx := context.Background()
        status, err := tr.scm.CreatePR(ctx, worktreePath, opts)
        if err != nil {
            log.Printf("warning: failed to create PR for %s: %v", repoName, err)
            if firstErr == nil {
                firstErr = fmt.Errorf("creating PR for %s: %w", repoName, err)
            }
            continue
        }

        // Insert PR record
        pr := &model.PullRequest{
            ProblemID: tr.task.ID,
            RepoName:  repoName,
            PRNumber:  status.Number,
            URL:       status.URL,
            CreatedAt: time.Now().UTC(),
        }
        if _, err := tr.store.InsertPullRequest(pr); err != nil {
            log.Printf("warning: failed to insert PR record: %v", err)
        }

        // Event
        payload := fmt.Sprintf(`{"repo":"%s","url":"%s"}`, repoName, status.URL)
        tr.store.InsertEvent(tr.task.ID, "", model.EventPRCreated, payload)

        log.Printf("anchor: created PR for %s: %s", repoName, status.URL)
    }
    return firstErr
}
```

- [ ] **Step 3: Remove old createPR method**

Delete the `createPR(repoName, worktreePath string)` method and the `os/exec` import if no longer needed.

- [ ] **Step 4: Run existing tests to verify no regressions**

Run: `go test ./internal/belayer/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/belayer/taskrunner.go
git commit -m "refactor: replace inline createPR with SCM provider interface"
```

---

## Chunk 4: Reaction Engine

### Task 13: Reaction engine core

**Files:**
- Create: `internal/review/engine.go`
- Test: `internal/review/engine_test.go`

- [ ] **Step 1: Write test for reaction engine event classification**

```go
// internal/review/engine_test.go
package review

import (
    "testing"

    "github.com/donovan-yohan/belayer/internal/model"
)

func TestClassifyActivity_CIFailure(t *testing.T) {
    prev := &model.PRStatus{CIStatus: "passing"}
    curr := &model.PRStatus{CIStatus: "failing"}

    events := ClassifyActivity(prev, curr, nil)

    if len(events) != 1 || events[0].Type != EventCIFailed {
        t.Errorf("expected 1 ci_failed event, got %v", events)
    }
}

func TestClassifyActivity_Approved(t *testing.T) {
    prev := &model.PRStatus{CIStatus: "passing", Reviews: nil}
    curr := &model.PRStatus{
        CIStatus: "passing",
        Reviews: []model.Review{{State: "approved", Author: "alice"}},
    }

    events := ClassifyActivity(prev, curr, nil)

    hasApproval := false
    for _, e := range events {
        if e.Type == EventApproved {
            hasApproval = true
        }
    }
    if !hasApproval {
        t.Error("expected approval event")
    }
}

func TestClassifyActivity_ChangesRequested(t *testing.T) {
    prev := &model.PRStatus{}
    curr := &model.PRStatus{
        Reviews: []model.Review{{State: "changes_requested", Author: "bob"}},
    }

    events := ClassifyActivity(prev, curr, nil)

    hasChanges := false
    for _, e := range events {
        if e.Type == EventChangesRequested {
            hasChanges = true
        }
    }
    if !hasChanges {
        t.Error("expected changes_requested event")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/review/... -v`
Expected: FAIL

- [ ] **Step 3: Implement event classification**

```go
// internal/review/engine.go
package review

import "github.com/donovan-yohan/belayer/internal/model"

// ReactionEventType represents a detected PR event.
type ReactionEventType string

const (
    EventCIFailed          ReactionEventType = "ci_failed"
    EventCIPassed          ReactionEventType = "ci_passed"
    EventNewComment        ReactionEventType = "new_comment"
    EventChangesRequested  ReactionEventType = "changes_requested"
    EventApproved          ReactionEventType = "approved"
    EventMerged            ReactionEventType = "merged"
    EventClosed            ReactionEventType = "closed"
)

// ReactionEvent represents a detected change in PR state.
type ReactionEvent struct {
    Type    ReactionEventType
    Details any // type-specific payload
}

// ClassifyActivity compares previous and current PR status to detect events.
func ClassifyActivity(prev, curr *model.PRStatus, activity *model.PRActivity) []ReactionEvent {
    var events []ReactionEvent

    // CI transitions
    if prev.CIStatus == "passing" && curr.CIStatus == "failing" {
        events = append(events, ReactionEvent{Type: EventCIFailed})
    }
    if prev.CIStatus == "failing" && curr.CIStatus == "passing" {
        events = append(events, ReactionEvent{Type: EventCIPassed})
    }

    // State transitions
    if curr.State == "merged" && prev.State != "merged" {
        events = append(events, ReactionEvent{Type: EventMerged})
    }
    if curr.State == "closed" && prev.State != "closed" {
        events = append(events, ReactionEvent{Type: EventClosed})
    }

    // Review state changes
    prevReviewState := highestReviewState(prev.Reviews)
    currReviewState := highestReviewState(curr.Reviews)

    if currReviewState == "approved" && prevReviewState != "approved" {
        events = append(events, ReactionEvent{Type: EventApproved})
    }
    if currReviewState == "changes_requested" && prevReviewState != "changes_requested" {
        events = append(events, ReactionEvent{Type: EventChangesRequested})
    }

    // New comments
    if activity != nil && len(activity.Comments) > 0 {
        for _, c := range activity.Comments {
            events = append(events, ReactionEvent{Type: EventNewComment, Details: c})
        }
    }

    return events
}

// highestReviewState returns the most significant review state.
// Priority: changes_requested > approved > commented > ""
func highestReviewState(reviews []model.Review) string {
    state := ""
    for _, r := range reviews {
        switch r.State {
        case "changes_requested":
            return "changes_requested"
        case "approved":
            state = "approved"
        case "commented":
            if state == "" {
                state = "commented"
            }
        }
    }
    return state
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/review/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review/
git commit -m "feat: add reaction engine event classification"
```

### Task 14: CI fix dispatch

**Files:**
- Modify: `internal/review/engine.go`
- Test: `internal/review/engine_test.go`

- [ ] **Step 1: Write test for CI fix decision logic**

```go
func TestShouldDispatchCIFix(t *testing.T) {
    tests := []struct {
        name       string
        fixCount   int
        maxFixes   int
        wantFix    bool
    }{
        {"first attempt", 0, 2, true},
        {"second attempt", 1, 2, true},
        {"exhausted", 2, 2, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ShouldDispatchCIFix(tt.fixCount, tt.maxFixes)
            if got != tt.wantFix {
                t.Errorf("ShouldDispatchCIFix(%d, %d) = %v, want %v", tt.fixCount, tt.maxFixes, got, tt.wantFix)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/review/... -run TestShouldDispatchCIFix -v`
Expected: FAIL

- [ ] **Step 3: Implement CI fix decision**

```go
// ShouldDispatchCIFix returns true if another CI fix attempt should be made.
// ci_fix_count is incremented at dispatch time (not completion).
func ShouldDispatchCIFix(currentFixCount, maxFixAttempts int) bool {
    return currentFixCount < maxFixAttempts
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/review/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review/engine.go internal/review/engine_test.go
git commit -m "feat: add CI fix dispatch decision logic"
```

### Task 15: Review comment & changes-requested handling

**Files:**
- Modify: `internal/review/engine.go`
- Test: `internal/review/engine_test.go`

- [ ] **Step 1: Write test for reaction dispatch decisions**

```go
func TestDecideReaction(t *testing.T) {
    tests := []struct {
        name     string
        event    ReactionEvent
        pr       *model.PullRequest
        maxFixes int
        wantAction string
    }{
        {
            name:     "ci failure under cap",
            event:    ReactionEvent{Type: EventCIFailed},
            pr:       &model.PullRequest{CIFixCount: 0},
            maxFixes: 2,
            wantAction: "lead_dispatched",
        },
        {
            name:     "ci failure at cap",
            event:    ReactionEvent{Type: EventCIFailed},
            pr:       &model.PullRequest{CIFixCount: 2},
            maxFixes: 2,
            wantAction: "marked_stuck",
        },
        {
            name:     "new comment",
            event:    ReactionEvent{Type: EventNewComment},
            pr:       &model.PullRequest{},
            maxFixes: 2,
            wantAction: "comment_replied",
        },
        {
            name:     "changes requested",
            event:    ReactionEvent{Type: EventChangesRequested},
            pr:       &model.PullRequest{},
            maxFixes: 2,
            wantAction: "lead_dispatched",
        },
        {
            name:     "approved with auto_merge",
            event:    ReactionEvent{Type: EventApproved},
            pr:       &model.PullRequest{},
            maxFixes: 2,
            wantAction: "merge_attempted",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            action := DecideReaction(tt.event, tt.pr, tt.maxFixes, true)
            if action != tt.wantAction {
                t.Errorf("DecideReaction = %q, want %q", action, tt.wantAction)
            }
        })
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/review/... -run TestDecideReaction -v`
Expected: FAIL

- [ ] **Step 3: Implement DecideReaction**

```go
// DecideReaction determines what action to take for a given PR event.
func DecideReaction(event ReactionEvent, pr *model.PullRequest, maxFixAttempts int, autoMerge bool) string {
    switch event.Type {
    case EventCIFailed:
        if ShouldDispatchCIFix(pr.CIFixCount, maxFixAttempts) {
            return "lead_dispatched"
        }
        return "marked_stuck"

    case EventCIPassed:
        return "recorded"

    case EventNewComment:
        return "comment_replied"

    case EventChangesRequested:
        return "lead_dispatched"

    case EventApproved:
        if autoMerge {
            return "merge_attempted"
        }
        return "recorded"

    case EventMerged:
        return "status_merged"

    case EventClosed:
        return "status_closed"

    default:
        return "recorded"
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/review/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/review/engine.go internal/review/engine_test.go
git commit -m "feat: add reaction dispatch decision logic for all PR event types"
```

### Task 16: PR monitoring in daemon tick loop

**Files:**
- Modify: `internal/belayer/belayer.go`
- Test: `internal/belayer/belayer_test.go`

- [ ] **Step 1: Add PR monitoring fields to Belayer struct**

```go
// In Belayer struct:
scm           scm.SCMProvider
lastPRPollAt  time.Time
prPollInterval time.Duration
reviewCfg     belayerconfig.ReviewConfig
```

- [ ] **Step 2: Implement monitorPRs method**

```go
func (s *Belayer) monitorPRs() {
    if s.scm == nil {
        return
    }
    if time.Since(s.lastPRPollAt) < s.prPollInterval {
        return
    }

    // Get all open PRs for this crag
    prs, err := s.store.ListMonitoredPullRequests(s.cragID)
    if err != nil {
        log.Printf("pr monitor: listing PRs: %v", err)
        return
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    for _, pr := range prs {
        worktreePath := s.getWorktreeForRepo(pr.ProblemID, pr.RepoName)
        if worktreePath == "" {
            continue
        }

        // Get current PR status
        status, err := s.scm.GetPRStatus(ctx, worktreePath, pr.PRNumber)
        if err != nil {
            log.Printf("pr monitor: getting status for PR #%d: %v", pr.PRNumber, err)
            continue
        }

        // Build previous status from stored data
        prevStatus := &model.PRStatus{
            CIStatus: pr.CIStatus,
            State:    pr.State,
        }

        // Get new activity since last poll
        var since time.Time
        if pr.LastPolledAt != nil {
            since = *pr.LastPolledAt
        } else {
            since = pr.CreatedAt
        }
        activity, _ := s.scm.GetNewActivity(ctx, worktreePath, pr.PRNumber, since)

        // Classify events
        events := review.ClassifyActivity(prevStatus, status, activity)

        // Process each event
        for _, event := range events {
            action := review.DecideReaction(event, &pr, s.reviewCfg.CIFixAttempts, s.reviewCfg.AutoMerge)
            s.executeReaction(ctx, &pr, event, action, worktreePath)
        }

        // Update stored PR status
        s.store.UpdatePullRequestCI(pr.ID, status.CIStatus, pr.CIFixCount)
        if status.State != pr.State {
            s.store.UpdatePullRequestState(pr.ID, status.State)
        }

        // Check for review state changes
        reviewState := review.HighestReviewState(status.Reviews)
        if reviewState != "" && reviewState != pr.ReviewStatus {
            s.store.UpdatePullRequestReview(pr.ID, reviewState)
        }
    }

    s.lastPRPollAt = time.Now()
}

func (s *Belayer) executeReaction(ctx context.Context, pr *model.PullRequest, event review.ReactionEvent, action, worktreePath string) {
    // Record reaction
    reaction := &model.PRReaction{
        PRID:        pr.ID,
        TriggerType: string(event.Type),
        ActionTaken: action,
    }

    switch action {
    case "lead_dispatched":
        if event.Type == review.EventCIFailed {
            // Increment fix count at dispatch time (per design: counted at dispatch, not completion)
            pr.CIFixCount++
            s.store.UpdatePullRequestCI(pr.ID, "failing", pr.CIFixCount)
            s.store.InsertEvent(pr.ProblemID, "", model.EventCIFixDispatched,
                fmt.Sprintf(`{"pr_number":%d,"attempt":%d}`, pr.PRNumber, pr.CIFixCount))

            // Transition problem to ci_fixing state
            s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusCIFixing)

            // Spawn lead with CI failure details as goal
            // Uses the existing lead spawning infrastructure (SpawnClimb pattern)
            // The lead receives CI failure details and uses /harness:bug to fix
            ciDetails, _ := json.Marshal(map[string]any{
                "pr_number": pr.PRNumber,
                "repo":      pr.RepoName,
                "attempt":   pr.CIFixCount,
            })
            log.Printf("pr monitor: dispatching CI fix lead for PR #%d (attempt %d)", pr.PRNumber, pr.CIFixCount)
            // NOTE: Full lead spawn integration (creating worktree climb, queueing to lead queue)
            // requires wiring into ProblemRunner.SpawnClimb(). This records the intent;
            // the daemon's existing lead queue will pick up the spawned climb.
            _ = ciDetails
        }
        if event.Type == review.EventChangesRequested {
            // Human-driven review loop: spawn lead with review feedback
            s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusReviewReacting)
            log.Printf("pr monitor: dispatching review reaction lead for PR #%d", pr.PRNumber)
            // NOTE: Same lead spawn integration as CI fix above.
        }

    case "comment_replied":
        // Spawn agentic node to draft a reply (informational only, no code changes)
        // Uses claude -p with review comment context to generate reply text
        // Then posts via SCMProvider.ReplyToComment()
        if comment, ok := event.Details.(model.ReviewComment); ok {
            log.Printf("pr monitor: drafting reply to comment #%d on PR #%d", comment.ID, pr.PRNumber)
            // NOTE: Agentic node integration (claude -p call + ReplyToComment) follows
            // the same pattern as spec assembly (Task 6). The prompt includes the
            // comment body, PR context, and asks for a concise reply.
            _ = comment
        }
        s.store.InsertEvent(pr.ProblemID, "", model.EventReviewCommentReplied,
            fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber))

    case "marked_stuck":
        s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusStuck)
        s.store.InsertEvent(pr.ProblemID, "", model.EventCIFixExhausted,
            fmt.Sprintf(`{"pr_number":%d,"attempts":%d}`, pr.PRNumber, pr.CIFixCount))

    case "merge_attempted":
        if err := s.scm.Merge(ctx, worktreePath, pr.PRNumber); err != nil {
            log.Printf("pr monitor: merge failed for PR #%d: %v", pr.PRNumber, err)
            return
        }
        s.store.UpdatePullRequestState(pr.ID, "merged")
        s.store.InsertEvent(pr.ProblemID, "", model.EventPRMerged,
            fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber))

    case "status_merged":
        s.store.UpdatePullRequestState(pr.ID, "merged")
        s.store.InsertEvent(pr.ProblemID, "", model.EventPRMerged,
            fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber))
        s.checkAllPRsMerged(pr.ProblemID)

    case "status_closed":
        s.store.UpdatePullRequestState(pr.ID, "closed")
        s.store.InsertEvent(pr.ProblemID, "", model.EventPRClosed,
            fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber))
        s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusClosed)
    }

    s.store.InsertPRReaction(reaction)
}

func (s *Belayer) checkAllPRsMerged(problemID string) {
    prs, err := s.store.ListPullRequestsForProblem(problemID)
    if err != nil {
        return
    }
    allMerged := true
    for _, pr := range prs {
        if pr.State != "merged" {
            allMerged = false
            break
        }
    }
    if allMerged {
        s.store.UpdateProblemStatus(problemID, model.ProblemStatusMerged)
    }
}
```

- [ ] **Step 3: Add monitorPRs call to tick()**

In `tick()`, add as a new phase after processing active problems:

```go
// Phase 4 — PR monitoring (respects interval internally)
s.monitorPRs()
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/belayer/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/belayer/belayer.go internal/belayer/belayer_test.go
git commit -m "feat: add PR monitoring phase to daemon tick loop"
```

---

## Chunk 5: Integration

### Task 17: Extended problem lifecycle states

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/belayer/belayer.go`
- Test: `internal/belayer/belayer_test.go`

- [ ] **Step 1: Update GetActiveProblems to include new in-flight states**

Per the design doc, `GetActiveProblems()` must include the new states:

```go
func (s *Store) GetActiveProblems(cragID string) ([]model.Problem, error) {
    rows, err := s.db.Query(
        `SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
         FROM problems WHERE crag_id = ? AND status IN ('running', 'reviewing', 'imported', 'enriching', 'pr_creating', 'pr_monitoring', 'ci_fixing', 'review_reacting')
         ORDER BY created_at ASC`, cragID,
    )
    // ... same scan logic
}
```

- [ ] **Step 2: Update daemon recover() to handle new states**

On daemon restart, problems in `pr_monitoring` should restore their PR monitoring state from the `pull_requests` table:

```go
func (s *Belayer) recoverPRMonitoring(problem *model.Problem) {
    prs, err := s.store.ListPullRequestsForProblem(problem.ID)
    if err != nil {
        log.Printf("recover: failed to load PRs for problem %s: %v", problem.ID, err)
        return
    }
    log.Printf("recover: restored %d monitored PRs for problem %s", len(prs), problem.ID)
}
```

- [ ] **Step 3: Add state transitions in tick() for new states**

Handle `imported` → `enriching` → `pending` transitions:

```go
// In tick(), after tracker sync:
for _, runner := range s.problems {
    switch runner.Status() {
    case model.ProblemStatusImported:
        // Start spec assembly agentic node
        s.store.UpdateProblemStatus(runner.ProblemID(), model.ProblemStatusEnriching)
        // TODO: spawn spec assembly node

    case model.ProblemStatusEnriching:
        // Check if spec assembly is complete
        // If done: create problem spec + climbs, transition to pending
    }
}
```

- [ ] **Step 4: Add `pr_creating` → `pr_monitoring` transition after HandleApproval**

In `HandleApproval`, after successfully creating PRs:

```go
s.store.UpdateProblemStatus(tr.task.ID, model.ProblemStatusPRMonitoring)
```

- [ ] **Step 5: Run tests**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/belayer/belayer.go
git commit -m "feat: extend problem lifecycle with planning and review states"
```

### Task 18: PR CLI commands

**Files:**
- Create: `internal/cli/pr_cmd.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Create PR command group**

```go
// internal/cli/pr_cmd.go
package cli

import (
    "fmt"
    "os"
    "text/tabwriter"

    "github.com/spf13/cobra"
    "github.com/donovan-yohan/belayer/internal/db"
    "github.com/donovan-yohan/belayer/internal/store"
)

func newPRCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "pr",
        Short: "Manage monitored pull requests",
    }
    cmd.AddCommand(newPRListCmd())
    cmd.AddCommand(newPRShowCmd())
    cmd.AddCommand(newPRRetryCmd())
    return cmd
}

func newPRListCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "list",
        Short: "Show all PRs belayer is monitoring",
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            cragCfg, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }
            _ = cragCfg // cragCfg.Name is the crag ID

            database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
            if err != nil {
                return err
            }
            defer database.Close()
            s := store.New(database)

            prs, err := s.ListMonitoredPullRequests(cragCfg.Name)
            if err != nil {
                return err
            }

            if len(prs) == 0 {
                fmt.Println("No monitored PRs.")
                return nil
            }

            w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
            fmt.Fprintln(w, "PR#\tREPO\tCI\tREVIEW\tSTATE\tURL")
            for _, pr := range prs {
                fmt.Fprintf(w, "#%d\t%s\t%s\t%s\t%s\t%s\n",
                    pr.PRNumber, pr.RepoName, pr.CIStatus, pr.ReviewStatus, pr.State, pr.URL)
            }
            return w.Flush()
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}

func newPRShowCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "show <number>",
        Short: "Detailed PR view with checks, reviews, reaction history",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            cragCfg, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }

            database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
            if err != nil {
                return err
            }
            defer database.Close()
            s := store.New(database)

            var prNumber int
            fmt.Sscanf(args[0], "%d", &prNumber)

            prs, err := s.ListMonitoredPullRequests(cragCfg.Name)
            if err != nil {
                return err
            }

            for _, pr := range prs {
                if pr.PRNumber == prNumber {
                    fmt.Printf("PR #%d (%s)\n", pr.PRNumber, pr.RepoName)
                    fmt.Printf("  URL:      %s\n", pr.URL)
                    fmt.Printf("  CI:       %s (fix count: %d)\n", pr.CIStatus, pr.CIFixCount)
                    fmt.Printf("  Review:   %s\n", pr.ReviewStatus)
                    fmt.Printf("  State:    %s\n", pr.State)
                    fmt.Printf("  Problem:  %s\n", pr.ProblemID)

                    // Show reaction history
                    reactions, _ := s.ListPRReactions(pr.ID)
                    if len(reactions) > 0 {
                        fmt.Println("\n  Reaction History:")
                        for _, r := range reactions {
                            fmt.Printf("    [%s] %s -> %s\n",
                                r.CreatedAt.Format("2006-01-02 15:04"),
                                r.TriggerType, r.ActionTaken)
                        }
                    }
                    return nil
                }
            }

            return fmt.Errorf("PR #%d not found", prNumber)
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}

func newPRRetryCmd() *cobra.Command {
    var cragName string
    cmd := &cobra.Command{
        Use:   "retry <number>",
        Short: "Manually trigger a CI fix attempt (bypasses attempt cap)",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            resolved, err := resolveCragName(cragName)
            if err != nil {
                return err
            }
            cragCfg, cragDir, err := instance.Load(resolved)
            if err != nil {
                return err
            }

            database, err := db.Open(filepath.Join(cragDir, "belayer.db"))
            if err != nil {
                return err
            }
            defer database.Close()
            s := store.New(database)

            var prNumber int
            fmt.Sscanf(args[0], "%d", &prNumber)

            // Find the PR by number
            prs, err := s.ListMonitoredPullRequests(cragCfg.Name)
            if err != nil {
                return err
            }

            for _, pr := range prs {
                if pr.PRNumber == prNumber {
                    // Reset fix count to 0 so the daemon dispatches a new fix on next tick
                    if err := s.UpdatePullRequestCI(pr.ID, pr.CIStatus, 0); err != nil {
                        return fmt.Errorf("resetting CI fix count: %w", err)
                    }
                    fmt.Printf("PR #%d marked for CI fix retry (fix count reset to 0)\n", prNumber)
                    return nil
                }
            }

            return fmt.Errorf("PR #%d not found", prNumber)
        },
    }
    cmd.Flags().StringVar(&cragName, "crag", "", "Crag name")
    return cmd
}
```

- [ ] **Step 2: Register PR command in root.go**

Add to `NewRootCmd()`:

```go
rootCmd.AddCommand(newPRCmd())
```

- [ ] **Step 3: Run build to verify compilation**

Run: `go build ./cmd/belayer`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/cli/pr_cmd.go internal/cli/root.go
git commit -m "feat: add PR CLI commands (list, show, retry)"
```

### Task 19: Setter session extensions

**Files:**
- Create: `internal/defaults/commands/ticket.md`
- Create: `internal/defaults/commands/ticket-list.md`
- Create: `internal/defaults/commands/sync.md`
- Create: `internal/defaults/commands/prs.md`
- Create: `internal/defaults/commands/pr.md`
- Modify: `internal/defaults/claudemd/setter.md`
- Modify: `internal/defaults/defaults.go`

- [ ] **Step 1: Create new setter slash command files**

`internal/defaults/commands/ticket.md`:
```markdown
Fetch a tracker issue and create a belayer problem from it.

Usage: /belayer:ticket <ISSUE_ID>

Steps:
1. Run `belayer tracker show <ISSUE_ID>` to preview the issue
2. Ask the user to confirm they want to create a problem from it
3. Run `belayer problem create --ticket <ISSUE_ID> --crag {{.CragName}}`
4. Report the created problem ID
```

`internal/defaults/commands/ticket-list.md`:
```markdown
Preview issues matching the tracker label filter without importing.

Usage: /belayer:ticket-list

Run: `belayer tracker list --crag {{.CragName}}`
```

`internal/defaults/commands/sync.md`:
```markdown
Trigger immediate tracker sync.

Usage: /belayer:sync

Run: `belayer tracker sync --crag {{.CragName}}`
```

`internal/defaults/commands/prs.md`:
```markdown
List all monitored PRs for this crag.

Usage: /belayer:prs

Run: `belayer pr list --crag {{.CragName}}`
```

`internal/defaults/commands/pr.md`:
```markdown
Show detailed view of a specific PR.

Usage: /belayer:pr <NUMBER>

Run: `belayer pr show <NUMBER> --crag {{.CragName}}`
```

- [ ] **Step 2: Update setter.md template**

Add sections for tracker awareness and PR monitoring to `internal/defaults/claudemd/setter.md`:

```markdown
## Tracker Integration

This crag can pull issues from an external tracker (GitHub Issues or Jira).
- `/belayer:ticket <ID>` — fetch a ticket and create a problem from it
- `/belayer:ticket-list` — preview matching issues from the tracker
- `/belayer:sync` — trigger immediate tracker sync

When a user says "implement ENG-1234" or "pick up the next ready ticket", use the ticket commands.

## PR Monitoring

Belayer monitors PRs it creates and reacts to CI failures and review comments.
- `/belayer:prs` — list all monitored PRs with their status
- `/belayer:pr <number>` — deep view of a specific PR

If a problem is "stuck" due to exhausted CI fix attempts, help the user diagnose the CI failure and decide next steps.
```

- [ ] **Step 3: Rename existing commands to /belayer: prefix**

Rename the files in `internal/defaults/commands/`:
- `problem-create.md` → keep as is (the setter.md template maps to `/belayer:problem-create`)
- `problem-list.md` → keep as is
- `status.md` → keep as is
- `logs.md` → keep as is
- `mail.md` → keep as is
- `message.md` → keep as is

The `/belayer:` prefix is handled by the setter.md template which instructs the setter Claude about the command namespace. No actual file renames needed — the prefix is documentation, not filesystem.

- [ ] **Step 4: Update defaults.go embed directive**

Verify the embed directive in `internal/defaults/defaults.go` covers the new command files. Since it uses `commands/*.md` glob, new `.md` files are automatically included.

- [ ] **Step 5: Run build to verify compilation**

Run: `go build ./cmd/belayer`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add internal/defaults/commands/ internal/defaults/claudemd/setter.md
git commit -m "feat: add setter slash commands for tracker and PR monitoring"
```

---

## Outcomes & Retrospective

**Summary:** 19/19 tasks completed, 1 drift entry, 7 surprises. 7 review findings fixed (3 critical: gh -R format, API endpoint, Review.State casing). 12 findings deferred (test coverage gaps, type design improvements).

**What worked:**
- 5-wave parallel execution with dependency tracking — completed 19 tasks efficiently
- Plan review before execution caught CLI pattern issues that would have been runtime failures
- Micro-reviews after each wave caught stale docs immediately
- Code review caught 3 critical runtime bugs (gh -R format, API endpoint, Review.State)

**What didn't:**
- tmux pane limits blocked spawning review agents after orchestration — needed manual cleanup
- Worker agents in overlapping packages can accidentally stage each other's files
- Empty `repoDir` passed to SCM methods in `monitorPRs` — not caught until review (needs integration tests)
- Partial PR creation failure leaves orphaned records (structural issue, deferred)

**Learnings to codify:**
- GitHub API returns UPPER_CASE for review states — always lowercase-normalize at the boundary
- `gh -R` expects `owner/repo` format, not filesystem paths — use `repo.OwnerRepoFromURL()`
- When changing daemon method signatures, check all test call sites (blast radius)
- Shut down orchestration workers before spawning review agents to avoid pane exhaustion
