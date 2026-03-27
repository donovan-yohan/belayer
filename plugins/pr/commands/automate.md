---
description: Use when automating the full PR lifecycle, when wanting hands-off PR creation through merge, or when asked to automate a pull request
---

# Automate PR

Orchestrates the full PR lifecycle: author → review → resolve → merge, with automated decision points. Stops and reports to user when human judgment is needed.

## Usage

```
/pr:automate                    # Full automated flow
/pr:automate --skip-simplify    # Skip code-simplifier gate
/pr:automate --base develop     # Target different base branch
/pr:automate --dry-run          # Author + review only, no resolve or merge
```

## Hard Stops

**STOP and report to the user immediately if ANY of these occur:**
1. Verification gate fails (tests, lint, typecheck)
2. CI checks fail after PR creation
3. Review agents report critical-severity findings
4. Resolve categorizes any comment as **Discussion** or **Question** (needs human judgment)
5. `gh pr merge` fails (branch protection, required human approvals)

## Prerequisites

This command requires the **pr-review-toolkit** plugin. The code-simplifier gate uses the built-in `/simplify` command (skip with `--skip-simplify` if unavailable).

## Invocation

**IMMEDIATELY execute this workflow. All phases run sequentially.**

---

### Phase 0: Check Prerequisites

Verify the `pr-review-toolkit` plugin is installed by checking if its agents are available.

**If not installed, STOP and print:**
```
ERROR: Missing required plugin: pr-review-toolkit

Install it:
  /plugins add pr-review-toolkit
```

---

### Phase 1: Author

Follow the `pr:author` workflow with automated decisions.

**1a. Detect current state:**
```bash
git status --porcelain
git branch --show-current
git log -1 --format="%H %s"
```

Determine: on default branch? uncommitted changes? branch pushed?

**1b. Branch creation (if on default branch):**

Extract initials from `git config user.name` (first letter of each word).

**Automated decision:** Infer type (`feat`, `fix`, `refactor`, `docs`, `chore`) and branch name from the changes. Do NOT ask the user.

```bash
git checkout -b <initials>/<type>/<inferred-name>
```

**1c. Code-simplifier gate:**

**Skip if `--skip-simplify` flag provided.**

Check if code-simplifier has been run:
```bash
git log --oneline -20 | grep -i "simplif"
```

If no evidence found, run the built-in `/simplify` command to review changed code for reuse, quality, and efficiency. Wait for completion before proceeding.

**1d. Verification gate:**

Identify project verification commands by checking for `package.json`, `Makefile`, `build.gradle.kts`, `pyproject.toml`.

**Run ALL applicable verification commands.** Read full output. Check exit codes.

**HARD STOP if tests, lint, or typecheck fail.** Do NOT proceed. Report failures to user.

**1e. Commit & push:**

```bash
git add <specific-files>
git commit -m "<type>: <description>"
git push -u origin <branch-name>
```

**1f. Create PR:**

Determine the base branch from `--base` argument if provided, otherwise detect the repository default:
```bash
BASE="${BASE:-$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name)}"
git diff "$BASE"...HEAD
git log "$BASE"..HEAD --oneline
```

Create PR with `gh`. Include `--base "$BASE"` if not the default:
```bash
gh pr create --title "<type>: <concise-title>" --body "$(cat <<'EOF'
## Summary

<1-3 sentences: WHY these changes matter>

## Changes

- <Category>: <What changed and why>

## Testing

- <How this was verified>

---
*Created with `/pr:automate`*
EOF
)"
```

Capture the PR number and URL for subsequent phases.

---

### Phase 2: Review

Invoke `/pr:review` using the Skill tool: `Skill("pr:review", args="<PR number>")`. This loads the review command which runs all 6 pr-review-toolkit agents in parallel with proper `subagent_type` parameters and posts inline review comments. You MUST use the Skill tool — do not manually spawn the review agents.

Read the posted review to check for findings.

**HARD STOP if any agent reports critical-severity findings.** Report to user.

**If `--dry-run` flag provided, STOP here.** Report PR URL and review findings to user.

---

### Phase 3: Wait for CI

Poll for CI check completion:
```bash
gh pr checks <number> --watch
```

**HARD STOP if any required check fails.** Report failing checks to user with details.

---

### Phase 4: Resolve

Fetch all PR comments in parallel using `--jq` with **single-quoted** filters (prevents zsh `!` expansion errors):
```bash
gh api repos/{owner}/{repo}/pulls/<number>/comments --paginate --jq '.[] | {id, path, line, body, user: .user.login}'
gh api repos/{owner}/{repo}/pulls/<number>/reviews --paginate --jq '[.[] | select(.body != "") | {id, state, body, user: .user.login}]'
gh api repos/{owner}/{repo}/issues/<number>/comments --paginate --jq '.[] | {id, body, user: .user.login}'
```

**Categorize each comment:**

**Default bias: implement everything actionable.** Agents are fast — the cost of implementing is low, the cost of deferring is high. Never decline actionable feedback as "better in a follow-up PR."

| Category | Criteria | Automated Action |
|----------|----------|-----------------|
| **Actionable** | Requests a code change or suggests an improvement | Make the change — no exceptions, no deferring |
| **Question** | Asks for clarification | **HARD STOP** |
| **Discussion** | Debate about approach | **HARD STOP** |
| **Resolved** | Already addressed | Skip |

**If any Question or Discussion items exist → STOP.** Report to user with a summary of what needs human input.

**For Actionable items (implement ALL of them):**
1. Read each referenced file
2. Make the requested change
3. Run project tests to verify no regressions
4. Commit and push:
   ```bash
   git add <changed-files>
   git commit -m "fix: address review feedback"
   git push
   ```
5. Reply to each resolved comment with fix details:
   ```bash
   gh api repos/{owner}/{repo}/pulls/<number>/comments/<comment_id>/replies \
     -f body="Fixed: <description of change>"
   ```

---

### Phase 5: Merge

Verify merge conditions:
```bash
gh pr checks <number>
gh pr view <number> --json reviewDecision,mergeStateStatus,mergeable
```

If all checks pass and PR is mergeable:
```bash
gh pr merge <number> --delete-branch
```

Uses the repo's default merge strategy (no `--squash`, `--merge`, or `--rebase` flag).

**HARD STOP if merge fails** (branch protection, required approvals, merge conflicts). Report to user.

### Report Result

Output final summary:
```markdown
## PR Automated Successfully

**PR:** #<number> - <title>
**URL:** <url>
**Status:** Merged ✓

### Phases completed:
1. ✓ Authored PR from branch changes
2. ✓ Review posted (N findings)
3. ✓ CI checks passed
4. ✓ Resolved N actionable comments
5. ✓ Merged via repo default strategy
```

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Proceeding after verification failure | STOP immediately, report to user |
| Trying to resolve Discussion comments | STOP, these need human judgment |
| Merging with failing CI | Wait for all checks to pass first |
| Using --squash or --merge flags | Use no flag — let repo default decide |
| Proceeding when review agents find critical issues | STOP, report critical-severity findings to user |
| Skipping test run after resolve changes | Always verify before pushing fixes |
| Running without pr-review-toolkit | Install: `/plugins add pr-review-toolkit` |
| jq filters with `!=` in double quotes | Always use **single quotes** around jq expressions, or use `gh api --jq` flag |
