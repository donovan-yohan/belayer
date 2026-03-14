---
description: Use when PR description is stale, after resolving comments, or when PR content has changed since creation
---

# Update PR Description

Syncs PR description with current state of changes.

## Usage

```
/pr:update              # Update current branch's PR
/pr:update 123          # Update PR #123
/pr:update --preview    # Show changes without applying
```

## Invocation

**IMMEDIATELY execute this workflow:**

### 1. Get Current State

If PR number provided as argument, include it in the commands. Otherwise detect from current branch:
```bash
# Current PR description and target branch
gh pr view <number> --json number,title,body,commits,reviews,baseRefName

# Extract target branch for diffs
BASE="$(gh pr view <number> --json baseRefName -q .baseRefName)"

# All commits since the PR's actual target branch (NOT the repo default)
git log "$BASE"..HEAD --oneline

# Current diff
gh pr diff <number>
```

### 2. Analyze Changes Since PR Creation

Compare:
- Original PR description (what it claims)
- Current commits (what actually changed)
- Review comments resolved (what was addressed)

Identify:
- New commits added after PR creation
- Sections of description now outdated
- Review feedback that was addressed

### 3. Generate Updated Description

Preserve:
- Linked issues (`Fixes #123`)
- Screenshots or images
- Manual notes from author
- Testing instructions if still valid

Update:
- Summary to reflect current state
- Changes list to include new commits
- Testing section if approach changed

Add (if applicable):
- "Updated after review" note
- Summary of resolved feedback

### 4. Preview Changes

Show diff of old vs new description:

```markdown
## PR Description Update Preview

### Changes detected:
- 2 new commits since original PR
- 3 review comments resolved
- Error handling added to auth module

### Description diff:

@@ Summary @@
- Adds user authentication
+ Adds user authentication with improved error handling

@@ Changes @@
+ - Auth: Add null checks per review feedback
+ - API: Switch to const declarations

### Apply this update? (y/n)
```

### 5. Apply Update (if approved and not --preview)

```bash
gh pr edit <number> --body "$(cat <<'EOF'
<updated description content>
EOF
)"
```

### 6. Report Completion

```markdown
## PR Updated

**PR:** #<number>
**Changes:** Summary updated, N new items in changes list

View: <pr-url>
```

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Overwriting manual notes | Preserve user-added sections |
| Losing linked issues | Keep Fixes/Closes references |
| Updating without preview | Always show diff first |
