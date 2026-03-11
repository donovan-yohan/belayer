# Planning & Review Hats Design

**Date:** 2026-03-11
**Status:** Approved
**Context:** Belayer excels at the implementation hat (decompose work, spawn leads, validate, create PRs). This design adds the two missing hats: sprint planning/refinement (tracker intake) and code review orchestration (PR monitoring and reaction).

## Motivation

Software engineers wear three hats:

1. **Planning/Refinement** — refine sprints, break down tickets, prioritize
2. **Implementation** — write code, run tests, validate
3. **Code Review** — review PRs, react to CI, address feedback

Belayer covers hat 2 deeply. This design extends it to cover hats 1 and 3, inspired by learnings from the agent-orchestrator codebase (plugin architecture, tracker integrations, SCM lifecycle management).

## Architectural Approach

**Extended daemon with plugin interfaces** (Approach C from brainstorm). The existing belayer daemon gains two new phases in its tick loop, backed by clean Go interfaces that make each capability a registered, config-selected module. This matches belayer's established pattern: deterministic Go handles orchestration, agentic nodes handle judgment calls.

## Extended Problem Lifecycle

```
              PLANNING HAT                         IMPLEMENTATION HAT                            REVIEW HAT
         ┌─────────────────────────┐        ┌──────────────────────────┐        ┌───────────────────────────────────────┐
         │                         │        │                          │        │                                       │
Tracker ─► imported ─► enriching ─► pending ─► decomposing ─► running ─► aligning ─► pr_creating ─► pr_monitoring ─┬─► merged
         │                         │        │                          │        │          │                │        │
         │   (tracker + agentic    │        │  (existing belayer flow) │        │          ▼                │        │
         │    spec assembly)       │        │                          │        │     ci_fixing (max 2)     │        │
         └─────────────────────────┘        └──────────────────────────┘        │          │                │        │
                                                                                │          ▼                │        │
                                                                                │     review_reacting ──────┘        │
                                                                                │     (human-driven)                 │
                                                                                │                                    │
                                                                                └────────────────────────────► closed ┘
```

**New states:**

| State | Hat | Description |
|-------|-----|-------------|
| `imported` | Planning | Issue pulled from tracker, stored in `tracker_issues` table |
| `enriching` | Planning | Agentic node converting issue into problem spec + suggested climbs |
| `pr_creating` | Review | Leads topped, anchor approved, building PRs (possibly stacked) |
| `pr_monitoring` | Review | PRs open, polling for CI and review events |
| `ci_fixing` | Review | CI failed, lead dispatched to fix. Cap is **per-PR** (default 2). `ci_fix_count` is incremented at **dispatch time** (not completion), so in-flight fixes at crash time are already counted against the cap. |
| `review_reacting` | Review | Human requested changes, brainstorm/bug cycle spawned |
| `merged` | Review | Terminal — all PRs merged |
| `closed` | Review | Terminal — PR closed without merge |

Existing states (`pending`, `decomposing`, `running`, `aligning`) are unchanged.

**Crash recovery for new states:** On daemon restart, `GetActiveProblems()` must include the new in-flight states (`imported`, `enriching`, `pr_creating`, `pr_monitoring`, `ci_fixing`, `review_reacting`). For `pr_monitoring` specifically, the daemon re-reads the `pull_requests` table to restore monitored PRs and uses `last_polled_at` as the resume point for `GetNewActivity()` calls.

## Tracker Plugin Interface

Two Go interfaces, registered by provider name in crag config. Read-only — belayer pulls issues but never writes back to the tracker.

```go
// Tracker pulls issues from external trackers.
type Tracker interface {
    // ListIssues returns issues matching the filter (e.g., labeled "belayer").
    ListIssues(ctx context.Context, filter IssueFilter) ([]Issue, error)

    // GetIssue fetches a single issue by its tracker-native ID (e.g., "ENG-1234").
    GetIssue(ctx context.Context, id string) (*Issue, error)
}

// Issue is the tracker-agnostic representation of a work item.
type Issue struct {
    ID           string            // tracker-native ID (e.g., "ENG-1234", "#42")
    Title        string
    Body         string            // description/markdown
    Comments     []Comment         // discussion thread
    Labels       []string
    Priority     string            // normalized: critical, high, medium, low
    Assignee     string
    LinkedIssues []LinkedIssue     // parent epics, blockers, related
    URL          string            // web link back to tracker
    Raw          map[string]any    // provider-specific fields preserved
}

type IssueFilter struct {
    Labels   []string  // e.g., ["belayer", "ready"]
    Assignee string
    Sprint   string    // current sprint (Jira-specific, ignored by others)
    Status   []string  // e.g., ["todo", "ready"]
}
```

**Implementations:**

- `internal/tracker/github/` — uses `gh` CLI (same auth pattern as existing PR creation). `ListIssues` maps to `gh issue list --label belayer --json ...`. `GetIssue` maps to `gh issue view <id> --json ...`.
- `internal/tracker/jira/` — uses Jira REST API v3. Auth via `JIRA_API_TOKEN` env var + base URL from crag config. `ListIssues` maps to JQL queries.

**Spec assembly:** The daemon (not the tracker) converts an `Issue` into a problem spec via an agentic node — an ephemeral Claude session that receives the issue struct and produces a structured problem spec + suggested climbs. This keeps the tracker interface thin and the intelligence in the orchestration layer. The agentic node output is stored in the `agentic_decisions` table with node type `tracker_spec_assembly`.

**Sync strategy:**

- **Automatic sync**: Daily (configurable via `sync_interval`), filtered by required label/tag
- **On-demand**: `belayer tracker sync` CLI command for immediate refresh
- **Direct intake**: `belayer problem create --ticket ENG-1234` or setter's `/belayer:ticket ENG-1234`

## SCM Provider Interface & PR Stacking

```go
// SCMProvider handles PR lifecycle operations.
type SCMProvider interface {
    CreatePR(ctx context.Context, repo, worktree string, opts PROptions) (*PR, error)
    CreateStackedPRs(ctx context.Context, repo, worktree string, splits []PRSplit) ([]*PR, error)
    GetPRStatus(ctx context.Context, repo string, prNumber int) (*PRStatus, error)
    GetNewActivity(ctx context.Context, repo string, prNumber int, since time.Time) (*PRActivity, error)
    ReplyToComment(ctx context.Context, repo string, prNumber int, commentID int64, body string) error
    Merge(ctx context.Context, repo string, prNumber int) error
}

type PROptions struct {
    Title      string
    Body       string   // generated from problem spec + climb summaries
    BaseBranch string
    Draft      bool
}

type PRSplit struct {
    Title            string
    Body             string
    Commits          []string  // commit SHAs in this slice
    StackPosition    int       // 1-indexed position in the stack; CreateStackedPRs handles
                               // sequential creation and base-branch chaining internally
}

type PRStatus struct {
    Number       int
    State        string   // open, merged, closed
    CIStatus     string   // passing, failing, pending
    CIDetails    []Check  // individual check names + statuses
    Reviews      []Review // approved, changes_requested, commented
    Mergeable    bool
    URL          string
}

type PRActivity struct {
    Comments      []ReviewComment
    Reviews       []Review
    CITransitions []CITransition  // e.g., passing→failing
}
```

**Initial implementation:** GitHub via `gh` CLI.

**Stacking logic:**

1. After anchor approves, count total diff lines across all climbs per repo
2. **<= 1000 lines**: single PR per repo (current behavior, better title/body)
3. **> 1000 lines**: group climbs into chunks under 1000 lines each. Each chunk becomes a PR, chained via base branch dependencies (PR 1 targets `main`, PR 2 targets PR 1's branch, etc.)
4. **Single climb > 1000 lines**: spawn an agentic node to analyze the diff and propose logical split points (by file group, by feature boundary). Cherry-pick commits into separate branches and create the stack.

**Stacked PR worktree management:** Each stacked branch needs its own worktree since a worktree is locked to a single branch. For stacked PRs, create sub-worktrees at `tasks/<problem-id>/<repo-name>-stack-<n>/` for each branch in the stack. These are created during `pr_creating` and cleaned up after all PRs in the stack reach terminal state.

**PR body generation:** An agentic node generates the PR title and body from the problem spec, climb summaries, and files changed — replacing the current static "Created by belayer spotter review." text. Outputs are stored in the `agentic_decisions` table with node type `pr_body_gen`.

## PR Monitoring & Reaction Engine

The reaction engine is a new phase in the daemon tick loop. For each problem in `pr_monitoring` state, it polls the SCM provider every 60 seconds and reacts to state transitions.

| Event | Reaction |
|-------|----------|
| CI passing → failing | Increment `ci_fix_count`. If < `ci_fix_attempts` (default 2): spawn a lead with CI failure details as goal, using `/harness:bug`. Push fix, continue monitoring. |
| CI failing → passing | Record event. No action. |
| New review comment | Spawn agentic node to draft a reply. Post via `SCMProvider.ReplyToComment()`. Informational only — no code changes. |
| Changes requested | Human-driven loop. Spawn a lead with review feedback as input. Lead runs `/harness:brainstorm` or `/harness:bug` depending on feedback nature (agentic node decides). Push fix, PR updates, monitoring continues. |
| Approved | Record event. If all PRs approved + CI passing and `auto_merge` enabled, call `SCMProvider.Merge()`. |
| Merged | Terminal. Update problem status to `merged`. |
| Closed (not merged) | Terminal. Update problem status to `closed`. |
| CI fix attempts exhausted | Mark problem as `stuck`. Human intervention required. |

**Key distinction:**

- **CI failures** are autonomous — belayer tries to fix, capped at `ci_fix_attempts` (default 2)
- **Review comments** get replies but no code changes
- **Change requests** are human-driven — each triggers a full brainstorm/bug cycle, no cap (human controls pace)

**Stacked PR handling:** CI and reviews tracked per-PR. Problem only reaches terminal state when all PRs in the stack are merged. If a lower PR gets changes requested, the fix may propagate up the stack — reaction engine re-runs agentic decomposition on the updated diff.

## Setter Session Extensions

**New slash commands (all `/belayer:` prefixed):**

| Command | Purpose |
|---------|---------|
| `/belayer:status` | Full lifecycle view — problems, climbs, PRs, CI, reviews |
| `/belayer:problem-create` | Create problem (existing, renamed) |
| `/belayer:problem-list` | List problems (existing, renamed) |
| `/belayer:ticket <ID>` | Fetch ticket from tracker, create problem |
| `/belayer:ticket-list` | Preview issues matching label filter |
| `/belayer:sync` | Trigger immediate tracker sync |
| `/belayer:prs` | List all monitored PRs — status, CI state, pending reviews |
| `/belayer:pr <number>` | Deep view of specific PR — checks, comments, reaction history |
| `/belayer:logs` | View lead session logs (existing, renamed) |
| `/belayer:message` | Send mail to agent (existing, renamed) |
| `/belayer:mail` | Read inbox (existing, renamed) |

**Updated setter CLAUDE.md template** (`internal/defaults/claudemd/setter.md`):
- Awareness of tracker plugin and ticket commands
- Awareness of PR monitoring state and how to interpret it
- Conversational intake: user can say "implement ENG-1234" or "pick up the next ready ticket"
- Instructions for triaging "stuck" problems (CI fix exhausted)

## CLI Commands

**New tracker commands:**

| Command | Purpose |
|---------|---------|
| `belayer tracker sync` | Immediate sync — fetch issues matching label filter, import as `imported` problems |
| `belayer tracker list` | Preview matching issues without importing (dry run) |
| `belayer tracker show <ID>` | Fetch and display a single issue's details |

**New PR commands:**

| Command | Purpose |
|---------|---------|
| `belayer pr list` | Show all PRs belayer is monitoring for the current crag |
| `belayer pr show <number>` | Detailed PR view — CI checks, reviews, reaction history |
| `belayer pr retry <number>` | Manually trigger a CI fix attempt (bypasses attempt cap) |

**Extended existing commands:**

| Command | Change |
|---------|--------|
| `belayer problem create` | Accepts `--ticket <ID>` flag — fetches from tracker, runs intake pipeline |
| `belayer status` | Shows full lifecycle including PR monitoring state |

## Configuration

```toml
[tracker]
provider = "github"         # "github" or "jira"
label = "belayer"            # required filter label — only tagged issues are eligible
sync_interval = "24h"        # automatic sync frequency

[tracker.github]
# uses gh CLI auth — no extra config

[tracker.jira]
base_url = "https://mycompany.atlassian.net"
project = "ENG"
# auth via JIRA_API_TOKEN env var

[review]
poll_interval = "60s"        # PR status polling frequency
ci_fix_attempts = 2          # autonomous CI fix attempts before marking stuck
auto_merge = false           # merge automatically when approved + CI passing

[pr]
stack_threshold = 1000       # lines changed before stacking PRs
draft = true                 # create PRs as drafts by default
```

Config follows existing resolution chain: crag config > global config > embedded defaults. Tracker config lives at crag level since different crags may use different providers.

## SQLite Schema Extensions

**New tables:**

```sql
-- Imported issues from trackers
CREATE TABLE tracker_issues (
    id TEXT PRIMARY KEY,              -- tracker-native ID (e.g., "ENG-1234")
    provider TEXT NOT NULL,           -- "github" or "jira"
    title TEXT NOT NULL,
    body TEXT,
    comments_json TEXT,               -- JSON array of comments
    labels_json TEXT,                 -- JSON array of labels
    priority TEXT,
    assignee TEXT,
    url TEXT,
    raw_json TEXT,                    -- full provider response preserved
    problem_id TEXT,                  -- FK to problems (null until converted)
    synced_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

-- PRs created and monitored by belayer
CREATE TABLE pull_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    problem_id TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    url TEXT NOT NULL,
    stack_position INTEGER DEFAULT 1, -- 1-indexed position in stack (1 = first or only PR)
    stack_size INTEGER DEFAULT 1,
    ci_status TEXT DEFAULT 'pending', -- pending, passing, failing
    ci_fix_count INTEGER DEFAULT 0,
    review_status TEXT DEFAULT 'pending', -- pending, commented, changes_requested, approved
    state TEXT DEFAULT 'open',        -- open, merged, closed
    last_polled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (problem_id) REFERENCES problems(id)
);

-- Reaction history for audit trail
CREATE TABLE pr_reactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_id INTEGER NOT NULL,
    trigger_type TEXT NOT NULL,       -- ci_failure, review_comment, changes_requested
    trigger_payload TEXT,             -- JSON with details
    action_taken TEXT NOT NULL,       -- lead_dispatched, comment_replied, marked_stuck
    lead_id TEXT,                     -- FK to leads if a lead was spawned
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (pr_id) REFERENCES pull_requests(id)
);
```

**Extended `problems` table:**

- Add `tracker_issue_id TEXT` column (nullable FK to `tracker_issues`)
- New status values: `imported`, `enriching`, `pr_creating`, `pr_monitoring`, `merged`, `closed`
- The existing `jira_ref TEXT` column is **superseded** by `tracker_issue_id`. Migration should leave existing `jira_ref` values in place (no backfill) but all new tracker-sourced problems use `tracker_issue_id` exclusively. `jira_ref` is deprecated and will be removed in a future migration.

**New event types** for `events` table:

- `issue_imported`, `issue_converted`
- `pr_created`, `pr_stacked`
- `ci_failed`, `ci_fix_dispatched`, `ci_fix_succeeded`, `ci_fix_exhausted`
- `review_comment_received`, `review_comment_replied`
- `changes_requested`, `review_reaction_dispatched`
- `pr_approved`, `pr_merged`, `pr_closed`

## New Go Packages

| Package | Purpose |
|---------|---------|
| `internal/tracker/` | `Tracker` interface + `Issue` types |
| `internal/tracker/github/` | GitHub Issues implementation via `gh` CLI |
| `internal/tracker/jira/` | Jira implementation via REST API |
| `internal/scm/` | `SCMProvider` interface + PR types |
| `internal/scm/github/` | GitHub PR implementation via `gh` CLI |
| `internal/review/` | Reaction engine — event detection, dispatch logic, state transitions |

## Polling Strategy

| Source | Interval | Purpose |
|--------|----------|---------|
| SQLite (internal) | 2s (existing) | Problem/climb state transitions |
| Tracker (external) | 24h default + on-demand | Issue sync |
| SCM/PRs (external) | 60s | CI status, review events, merge state |

Tracker and PR polling run within the existing daemon tick loop on their own timers (check elapsed time each tick, only poll when interval has passed). This avoids hammering external APIs on the 2-second internal tick.

**Webhook acceleration (future):** The event model is designed so webhook-delivered events map to the same types as polled events. The reaction engine doesn't care how an event arrived. This allows bolting on webhook support later without rewriting the reaction logic.

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| Read-only tracker intake | Avoids unwanted modifications to team's tracker. Can add write-back later. |
| Plugin interfaces, compiled-in | Matches belayer's stage. External plugins add complexity without current need. |
| Polling first, webhook-ready model | Matches existing daemon pattern. Event model supports webhook acceleration later. |
| Stacked PRs by climb boundary | Climbs are already logical units. Agentic decomposition as fallback for large single climbs. |
| One CI fix loop, human-driven review loop | Prevents autonomous spiraling. Human stays in control of review cycle. |
| `/belayer:` prefix for all setter commands | Clean namespace, avoids collisions with other plugins. Old command names are not aliased — setter restart required on upgrade. |
| Crag-level tracker config | Different crags may use different trackers (open-source vs enterprise). |
| `jira_ref` deprecated, not backfilled | New `tracker_issue_id` FK supersedes it. Existing data left in place; column removed in future migration. |
| CI fix cap is per-PR | Each PR independently tracks its fix attempts. Relevant for stacked PRs where individual PRs can fail CI independently. |
| New agentic nodes write to `agentic_decisions` | `tracker_spec_assembly` and `pr_body_gen` node types follow the existing contract. |
