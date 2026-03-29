---
description: Use after reflect to classify learnings, update metrics, and propose agent evolution, or when user says "evolve", "self-improve", "classify learnings"
---

# Evolve

Self-modification phase. Classifies learning scope, updates metrics from review results, proposes agent evolution based on evidence, and auto-applies safe changes. Run after `/harness:reflect`, before `/harness:complete`.

## Usage

```
/harness:evolve                    # Run full evolution cycle
```

## Prerequisites

Requires a `.harness/` runtime directory. Resolve it:
```bash
HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
```

If `HARNESS_DIR` is empty, STOP and print:
```
No .harness/ runtime found. Run /harness:init and choose option 1 or 2 to enable self-improvement.
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Scope Classification

1. Read `docs/LEARNINGS.md`. Filter to entries written in this session (match current date and current branch in `source:` and `branch:` fields).

2. For each learning from this session that does NOT already have a `scope:` field:

   Classify as `scope: repo` or `scope: universal` using these rules:

   **Default to `repo`** (conservative). Only promote to `universal` when ALL of:
   - Contains zero project-specific references (file paths, module names, domain concepts)
   - Matches a known general pattern category (error handling, testing strategy, review methodology, agent coordination, security, performance)
   - The recommendation is actionable without project-specific context

   Examples:
   - "When unit-testing Temporal activities that call `activity.RecordHeartbeat`, wrap in `recover()`" → `repo` (references Temporal, a specific dependency)
   - "Review agents should always check stderr capture in shell commands" → `universal` (general pattern)
   - "The pipeline engine has race conditions in concurrent node execution" → `repo` (references specific module)
   - "When embedding YAML in Go raw strings, backticks cannot be nested" → `universal` (general language pattern)

3. Write the `scope` tag into each learning entry as a new metadata line after `category`:
   ```
   - scope: {repo|universal}
   ```

4. Report classification results:
   ```
   Classified {N} learnings: {R} repo, {U} universal
   ```

### Phase 2: Metrics Update

5. Read `$HARNESS_DIR/review-results.json` (written by `/harness:review`). If the file doesn't exist, skip to Phase 3 — no review data to process.

6. Read `$HARNESS_DIR/metrics/review-effectiveness.json`. For each agent in review-results.json:

   Count findings that were accepted (led to code changes) vs dismissed:
   - `findings` = count of entries where `accepted: true`
   - `false_positives` = count of entries where `accepted: false`
   - `unique_catches` = count of entries where `unique: true`

   Update metrics using the persistence script:
   ```bash
   bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
     --harness-dir "$HARNESS_DIR" \
     --metric "review-effectiveness" \
     --agent "{agent-name}" \
     --findings {N} \
     --false-pos {N} \
     --unique {N}
   ```

7. If an active plan exists in `docs/exec-plans/active/`, update plan accuracy metrics:
   - Count `- [x]` and `- [ ]` in the Progress section
   - Count entries in Surprises & Discoveries and Plan Drift tables

   ```bash
   bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
     --harness-dir "$HARNESS_DIR" \
     --metric "plan-accuracy" \
     --plan-slug "{plan-slug}" \
     --tasks-planned {N} \
     --tasks-completed {N} \
     --drift {N} \
     --surprises {N}
   ```

8. Update `$HARNESS_DIR/metrics/learning-efficacy.json`:
   - For each new learning from this session, check if any bugs in this session match existing learning categories (recurrence detection)
   - If a learning's recommendation was supposed to prevent this class of bug but the bug occurred anyway, increment `recurrence_count`
   - If a learning's recommendation was consulted and the relevant bug class did NOT occur, increment `prevented_count`
   - Use python3 for JSON manipulation:
     ```bash
     python3 -c "
     import json
     with open('$HARNESS_DIR/metrics/learning-efficacy.json') as f:
         data = json.load(f)
     learnings = data.setdefault('learnings', {})
     # ... update per learning ID ...
     data['last_updated'] = '$(date -u +%Y-%m-%dT%H:%M:%SZ)'
     with open('$HARNESS_DIR/metrics/learning-efficacy.json', 'w') as f:
         json.dump(data, f, indent=2)
     "
     ```

### Phase 3: Agent Evolution Proposals

9. Read `$HARNESS_DIR/metrics/review-effectiveness.json`. Identify:
   - **Escapes**: check `docs/REVIEW_GUIDANCE.md` escape log for entries from this session
   - **Metric anomalies**: agents with false positive rate > 50%, or zero unique catches after 10+ runs
   - **Universal learnings**: entries from Phase 1 classified as `scope: universal`

10. If no signals found, skip to Phase 4 report. Otherwise, for each signal:

    <MANDATORY>
    You MUST use the Agent tool with `subagent_type: "harness:harness-evolver"` to generate proposals. The evolver agent has the semantic dedup check and line budget enforcement logic. Do NOT generate proposals inline — the evolver agent's methodology prevents bloated agents.

    Example invocation:
    ```
    Agent(
      subagent_type="harness:harness-evolver",
      prompt="Generate evolution proposals for these signals: {signal list}. Read agent definitions from $HARNESS_DIR/agents/. Read metrics from $HARNESS_DIR/metrics/. Output proposals in the structured Output Format — do NOT write files directly."
    )
    ```
    </MANDATORY>

11. For each proposal the evolver produces, write it to disk:
    ```bash
    bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-proposal.sh \
      --harness-dir "$HARNESS_DIR" \
      --slug "{slug}" \
      --scope "{repo|universal}" \
      --signal "{signal-source}" \
      --agent "{agent-name}" \
      --current-file "{temp-file}" \
      --proposed-file "{temp-file}" \
      --reasoning-file "{temp-file}"
    ```

### Phase 4: Auto-Apply Safe Proposals

12. Read `$HARNESS_DIR/config.yaml`. Check `evolve.auto_apply` and `evolve.min_runs_for_auto`.

13. For each proposal from Phase 3, determine auto-apply eligibility. A proposal is auto-applied when ALL criteria are met:
    - `evolve.auto_apply` is `true` in config.yaml
    - The signal is a review escape (concrete evidence of a miss)
    - The change is additive (adds a check, doesn't remove one)
    - The agent has run N+ times where N = `evolve.min_runs_for_auto` (default: 5)

14. For eligible proposals:
    - Read the agent definition from `$HARNESS_DIR/agents/{agent}.md`
    - Apply the proposed change
    - Update the proposal status from `pending` to `applied`
    - Log the change to `$HARNESS_DIR/memory/IMPROVEMENTS.md`:
      ```markdown

      ### {YYYY-MM-DD}: {one-line description of change}
      - **Agent:** {agent name}
      - **Signal:** {signal source}
      - **Change:** {what was added/modified}
      - **Scope:** {repo|universal}
      - **Auto-applied:** yes
      - **Rollback:** none

      {Reasoning from the proposal}

      ---
      ```

15. For non-eligible proposals: leave status as `pending`. These will be surfaced in the PR description by `/harness:complete`.

### Phase 5: Auto-Rollback Check

16. Read `$HARNESS_DIR/memory/IMPROVEMENTS.md`. For each auto-applied change from previous sessions (not the current session):
    - Check if the agent's metrics worsened after the change was applied:
      - False positive rate increased by more than 20%
      - Unique catches decreased
    - If metrics worsened, auto-revert:
      - Undo the change in the agent definition
      - Update the IMPROVEMENTS.md entry: `- **Rollback:** rolled-back-{YYYY-MM-DD}`
      - Update the proposal status to `rolled-back`
      - Log a new IMPROVEMENTS.md entry explaining the rollback

17. This phase only runs when there are at least 2 post-change review runs to compare against. Skip if insufficient data.

### Phase 6: Write Run Record

18. Write a run record for this evolve session:
    ```bash
    bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-run.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "evolve" \
      --branch "$(git branch --show-current)"
    ```

### Report

19. Output:
    ```
    ## Evolve Complete

    **Runtime:** {$HARNESS_DIR}

    ### Scope Classification
    - Learnings classified: {N} ({R} repo, {U} universal)

    ### Metrics Updated
    - Review effectiveness: {N} agents updated
    - Plan accuracy: {updated | no active plan}
    - Learning efficacy: {N} learnings tracked

    ### Evolution Proposals
    - Proposals generated: {N}
    - Auto-applied: {N} (signals: {list})
    - Pending review: {N}

    ### Auto-Rollback
    - Rollbacks: {N | none | skipped (insufficient data)}

    ## Next Step

    Run `/harness:complete` to archive the plan and create the PR (proposals will be listed in the PR description).
    ```
