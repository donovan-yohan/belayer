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

| Category | Criteria | Action |
|----------|----------|--------|
| **Actionable** | Requests code change | Make the change |
| **Question** | Asks for clarification | Draft response |
| **Discussion** | Debate about approach | Summarize options |
| **Resolved** | Already addressed | Skip |

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
| **Actionable — declined with rationale** | Reply explaining why with clear reasoning, then **resolve the thread** |
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
| Blindly implementing all feedback | Evaluate each comment critically |
| Not testing after changes | Always verify before pushing |
| Not replying to reviewers | Always communicate what was addressed |
| Addressing comments but not resolving threads on GitHub | Always resolve threads for fixed/declined items via `gh api graphql` |
| Resolving question/discussion threads | Only resolve threads where you made a fix or gave a definitive decline with rationale |
| Using REST comment ID as GraphQL threadId | Fetch `node_id` from REST API first, then pass that to the GraphQL mutation |
| jq filters with `!=` in double quotes | Always use **single quotes** around jq expressions, or use `gh api --jq` flag |
