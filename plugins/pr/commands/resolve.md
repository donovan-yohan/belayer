---
description: Use when PR has unaddressed comments, when review feedback needs action, or when asked to handle PR feedback
---

# Resolve PR Comments

Analyzes outstanding PR comments and determines actions to address them.

## Usage

```
/pr:resolve              # Resolve comments on current branch's PR
/pr:resolve 123          # Resolve comments on PR #123
/pr:resolve --dry-run    # Show plan without making changes
```

## Invocation

**IMMEDIATELY execute this workflow:**

### 1. Fetch PR and Comments

```bash
gh pr view --json number,url
```

Fetch comments in parallel using `--jq` with **single-quoted** filters (prevents zsh `!` expansion errors):
```bash
gh api repos/{owner}/{repo}/pulls/<number>/comments --paginate --jq '.[] | {id, path, line, body, user: .user.login}'
gh api repos/{owner}/{repo}/pulls/<number>/reviews --paginate --jq '[.[] | select(.body != "") | {id, state, body, user: .user.login}]'
gh api repos/{owner}/{repo}/issues/<number>/comments --paginate --jq '.[] | {id, body, user: .user.login}'
```

### 2. Categorize Comments

**Default bias: implement everything.** Agents are fast and capable — the cost of implementing feedback is low, the cost of deferring is high (context loss, stale PRs, reviewer fatigue). Treat every comment as work to do in THIS PR unless it literally cannot be done here.

**Evaluate correctness, not effort.** The question is never "is this too much work?" — it's "is this technically sound?" Read the surrounding code, understand the reviewer's intent, and verify the suggestion wouldn't introduce a bug or regression. If it's correct, implement it regardless of scope. If it's wrong, decline with a clear technical explanation of why.

| Category | Criteria | Action |
|----------|----------|--------|
| **Actionable** | Requests code change or suggests an improvement | Verify correctness, then make the change. If the reviewer took time to suggest it, it's worth doing now |
| **Question** | Asks for clarification | Draft response AND make any code change implied by the question (add comment, rename, clarify logic) |
| **Discussion** | Debate about approach | Pick the best option and implement it. If genuinely ambiguous, ask the user — do NOT defer to a follow-up PR |
| **Resolved** | Already addressed | Skip |

**The "follow-up PR" trap:** Never categorize work as "out of scope" or "better in a separate PR" unless it would require changes to files/systems completely unrelated to this PR's purpose. Reviewer feedback on code IN this PR belongs IN this PR.

### 3. Present Action Plan

```markdown
## PR Comments Analysis

### Actionable (N items)
| # | File:Line | Request | Proposed Fix |
|---|-----------|---------|--------------|

### Questions (N items)
| # | From | Question | Draft Response |
|---|------|----------|----------------|

### Ready to resolve actionable items? (y/n)
```

### 4. Execute Fixes (if approved and not --dry-run)

For each actionable item: read file, make change, verify tests pass.

After all changes: run project tests, commit with message summarizing fixes, push.

### 5. Reply and Resolve Comments on GitHub

After pushing fixes, **you MUST resolve comment threads on GitHub** using the GraphQL API. Do not skip this step.

#### Resolution rules

| Category | GitHub Action |
|----------|--------------|
| **Actionable — fixed** | Reply with what you changed, then **resolve the thread** |
| **Actionable — declined (rare)** | Reply explaining why with a specific technical justification. Only valid reasons: (1) the suggestion is technically incorrect — would introduce a bug, break a contract, or cause a regression (explain what and why), (2) the change would break an unrelated system, (3) it requires access/permissions you don't have, (4) the user explicitly told you not to. "Too much work" and "better as a follow-up" are NOT valid reasons. Then **resolve the thread** |
| **Question / Discussion** | Reply if you have useful context, but **leave the thread open** |

#### How to resolve a thread

First, get the thread ID for each review comment. Review comment IDs from the REST API (`/pulls/<number>/comments`) are **not** GraphQL node IDs — you must convert them:

```bash
# Get the GraphQL node_id for a review comment (REST comment id -> node_id)
gh api repos/{owner}/{repo}/pulls/comments/<comment_id> --jq '.node_id'
```

Then resolve the thread using GraphQL:

```bash
gh api graphql -f query='
  mutation {
    resolveReviewThread(input: {threadId: "<NODE_ID>"}) {
      thread { isResolved }
    }
  }
'
```

**Important:** The `threadId` must be a GraphQL node ID (starts with `PRRT_` or similar), not a REST integer ID. If you have the REST comment ID, fetch the `node_id` field first as shown above.

#### Reply to individual comments

```bash
gh api repos/{owner}/{repo}/pulls/<number>/comments/<comment_id>/replies -f body='<your reply>'
```

Reply **before** resolving so the reviewer sees what was done.

### 6. Report Completion

Report files modified, commit SHA, and which comments were resolved vs left open. Suggest `/pr:update` to sync PR description.

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Deferring feedback to a "follow-up PR" | Implement it now. The reviewer gave feedback on THIS code — address it in THIS PR. Agents are fast; use that. |
| Categorizing suggestions as "out of scope" | If the reviewer commented on code in this PR, it's in scope. Period. |
| Declining feedback because it's "a lot of work" | That's exactly what agents are for. Implement it. |
| Implementing a suggestion without verifying correctness | Read the surrounding code first. If the suggestion would introduce a bug, decline with a specific technical explanation — don't blindly apply it |
| Not testing after changes | Always verify before pushing |
| Not replying to reviewers | Always communicate what was addressed |
| Addressing comments but not resolving threads on GitHub | Always resolve threads for fixed/declined items via `gh api graphql` |
| Resolving question/discussion threads | Only resolve threads where you made a fix or gave a definitive decline with rationale |
| Using REST comment ID as GraphQL threadId | Fetch `node_id` from REST API first, then pass that to the GraphQL mutation |
| jq filters with `!=` in double quotes | Always use **single quotes** around jq expressions, or use `gh api --jq` flag |
