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

### 5. Offer to Reply and Resolve

Ask user to choose:
1. **Reply individually** - Reply to each comment with fix details, resolve threads via `gh api graphql`
2. **Summary comment** - Post single `gh pr comment` listing all addressed feedback
3. **Skip** - User handles replies manually

### 6. Report Completion

Report files modified, commit SHA, and remaining questions/discussions. Suggest `/pr:update` to sync PR description.

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Blindly implementing all feedback | Evaluate each comment critically |
| Not testing after changes | Always verify before pushing |
| Not replying to reviewers | Always communicate what was addressed |
| jq filters with `!=` in double quotes | Always use **single quotes** around jq expressions, or use `gh api --jq` flag |
