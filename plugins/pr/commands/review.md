---
description: Use when reviewing a PR, when helping evaluate someone else's changes, or when asked to review code on a branch
---

# Review PR

Runs a comprehensive multi-perspective review of a PR using specialized agents.

## Usage

```
/pr:review              # Review PR for current branch
/pr:review 123          # Review PR #123
```

## Prerequisites

This command requires the **pr-review-toolkit** plugin. Optional: **adr** plugin for architecture compliance.

## Invocation

**IMMEDIATELY execute this workflow:**

### 1. Check Prerequisites

Verify the `pr-review-toolkit` plugin is installed by checking if its agents are available.

**If not installed, STOP and print:**
```
ERROR: Missing required plugin: pr-review-toolkit

Install it:
  /plugins add pr-review-toolkit
```

### 2. Find the PR

If PR number provided as argument, include it in the command. Otherwise detect from current branch:
```bash
# With PR number argument:
gh pr view <number> --json number,title,url,headRefName,baseRefName

# Without (uses current branch):
gh pr view --json number,title,url,headRefName,baseRefName
```

Extract and assign the target branch for use in all subsequent diffs:
```bash
BASE="$(gh pr view <number> --json baseRefName -q .baseRefName)"
```

If no PR exists for current branch, inform user and stop.

### 3. Run All Review Agents

Get the diff against the PR's actual target branch:
```bash
git diff "$BASE"...HEAD
```

Spawn **all 6 pr-review-toolkit agents in parallel**, passing each the PR number and diff context. Each agent covers a distinct review dimension:

| Agent | Focus |
|-------|-------|
| `code-reviewer` | Code quality, bugs, logic errors, CLAUDE.md/style guide adherence |
| `silent-failure-hunter` | Silent failures, inadequate error handling, inappropriate fallbacks |
| `pr-test-analyzer` | Test coverage completeness, missing edge cases |
| `type-design-analyzer` | Type design quality, encapsulation, invariant expression |
| `comment-analyzer` | Comment accuracy, staleness, long-term maintainability |
| `code-simplifier` | Unnecessary complexity, DRY violations, code smells |

**All 6 agents run concurrently.** Wait for all to complete before proceeding.

### 4. Aggregate and Post Results

Collect findings from all agents. For each finding, post an **inline review comment on the specific file and line** using a single GitHub pull request review.

Build the review payload as a JSON file to avoid shell escaping issues and ensure correct API format:
```bash
cat > /tmp/review.json << 'TEMPLATE'
{
  "event": "COMMENT",
  "commit_id": "<HEAD commit SHA>",
  "body": "## PR Review Summary\n\n**Agents run:** code-reviewer, silent-failure-hunter, pr-test-analyzer, type-design-analyzer, comment-analyzer, code-simplifier\n\n<high-level summary>",
  "comments": [
    {
      "path": "<file>",
      "line": <line number>,
      "side": "RIGHT",
      "body": "**[agent-name]** <finding>"
    }
  ]
}
TEMPLATE

gh api repos/{owner}/{repo}/pulls/<number>/reviews --input /tmp/review.json
```

**Required fields per inline comment:** `path`, `line`, `side` ("RIGHT" for new code), and `body`. The top-level `commit_id` must be the PR's HEAD SHA (`gh pr view <number> --json headRefOid -q .headRefOid`).

Prefix each inline comment body with the agent name in bold (e.g., **[silent-failure-hunter]**) so the reviewer knows which perspective the finding came from.

If an agent found no issues, note it in the summary body as passing.

### 5. ADR Compliance Check

Check if `docs/ARCHITECTURE.md` exists in the project.

**If it exists:**
- Get the PR diff using the saved `baseRefName`: `git diff "$BASE"...HEAD`
- Run `/adr:review` against that diff
- Report any CRITICAL or WARNING violations alongside the review results
- If violations found, note them clearly:
  ```
  ## ADR Compliance

  {N} architecture rule violations found in this PR:
  {violation summary from /adr:review output}
  ```

**If it does not exist:** Skip silently — not every project uses ADRs.

### 6. Report Completion

Summarize:
```
## Review Complete

**PR:** #<number> - <title>
**Agents:** 6/6 completed
**Findings:** N total across all agents
**ADR:** {N violations | compliant | skipped}

Review comments posted inline on the PR.
```

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Running without pr-review-toolkit | Install: `/plugins add pr-review-toolkit` |
| Diffing against repo default branch | Always use the PR's `baseRefName` — the PR may target `develop`, `release/*`, etc. |
| Posting a single summary comment | Use inline review comments on specific lines so feedback is actionable |
| Running agents sequentially | All 6 agents are independent — run them in parallel |
