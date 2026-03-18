---
description: Use when ready to submit work for review, when changes need to become a PR, or when asked to create a pull request
---

# Author PR

Creates a PR from current work with quality gates and proper description.

## Usage

```
/pr:author                          # Author PR for current changes
/pr:author --skip-simplify          # Skip code-simplifier check
/pr:author --base develop           # Target different base branch
```

## Invocation

**IMMEDIATELY execute this workflow:**

### 1. Detect Current State

```bash
git status --porcelain
git branch --show-current
git log -1 --format="%H %s"
```

Determine:
- Are we on the default branch? (need to create branch)
- Are there uncommitted changes? (need to commit)
- Is branch pushed? (need to push)

### 2. Branch Creation (if on default branch)

Extract initials from git config:
```bash
git config user.name
```

Parse first letter of each word (e.g., "Donovan Yohan" -> "dy").

Ask user for:
- Type: `feat`, `fix`, `refactor`, `docs`, `chore`
- Branch name (kebab-case description)

Create branch: `<initials>/<type>/<name>`

```bash
git checkout -b <branch-name>
```

### 3. Code Simplifier Gate

**Skip if `--skip-simplify` flag provided.**

Check if code-simplifier has been run:
```bash
git log --oneline -20 | grep -i "simplif"
```

If no evidence found, inform user and **spawn `code-simplifier:code-simplifier` subagent** (built-in Claude Code agent; skip if unavailable) with prompt:
```
Review changes on this branch against the base branch. Focus on code smells, DRY violations, and unnecessary complexity. Make improvements directly.
```

Wait for completion before proceeding.

### 4. Verification Gate

**REQUIRED: Apply superpowers:verification-before-completion**

Identify project's verification commands by checking for:
- `package.json` -> `npm test`, `npm run lint`, `npm run typecheck`
- `Makefile` -> `make test`, `make lint`
- `build.gradle.kts` -> `./gradlew test`, `./gradlew spotlessCheck`
- `pyproject.toml` -> `pytest`, `make lint`

**Run ALL applicable verification commands.** Read full output. Check exit codes.

**Red Flags - STOP if any occur:**
- Tests failing
- Lint errors
- Type errors
- Build failures

Do NOT proceed to PR creation with failing verification. Fix issues first.

### 5. Commit & Push

If uncommitted changes exist:
```bash
git add <specific-files>
git commit -m "<type>: <description>"
```

If not pushed:
```bash
git push -u origin <branch-name>
```

### 6. Create PR

Determine the base branch from `--base` argument if provided, otherwise detect the repository default:
```bash
BASE="${BASE:-$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name)}"
git diff "$BASE"...HEAD
git log "$BASE"..HEAD --oneline
```

Create PR with `gh`:
```bash
gh pr create --title "<type>: <concise-title>" --body "$(cat <<'EOF'
## Summary

<1-3 sentences: WHY these changes matter - business/user impact, not code details>

## Changes

- <Category>: <What changed and why>
- <Category>: <What changed and why>

## Testing

- <How this was verified>
- <Edge cases considered>

---
*Created with `/pr:author`*
EOF
)"
```

### 7. Report Result

Output the PR URL and summary of what was done.

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Creating PR with failing tests | Run verification first, fix failures |
| Description lists code changes | Describe WHY and impact, not WHAT |
| Skipping code-simplifier | Only skip with explicit flag |
| Committing secrets | Check for .env, credentials before commit |
