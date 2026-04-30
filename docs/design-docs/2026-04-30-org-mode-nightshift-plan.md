# Org Mode Nightshift Plan

**Date:** 2026-04-30
**Epic:** #111
**Base PR:** #117

This plan turns the organization-mode discussion into a stacked implementation
sequence. The work should stay evidence-driven: build the local substrate, run
the two proofs, then decide which runtime concepts deserve first-class support.

## Stack

```text
master
  -> issue/112-org-growth-contract        # PR #117
    -> issue/113-org-filesystem-contract  # stack A
      -> issue/114-org-cli                # stack B
        -> issue/115-org-proof-examples   # stack C
```

## Stack A: #113 Filesystem Contract

Goal: lock the local filesystem shape before CLI behavior exists.

Deliverables:

- Document `~/.belayer/crags/<crag>/`.
- Document `~/.belayer/talent-catalog/<category>/`.
- Document repo-local `.belayer/config.yaml` crag linking.
- Define `crag.yaml` minimal fields.
- Define precedence: repo-local override, linked crag default, shipped fallback.
- Include generated NPC/talent storage at contract level.

Review focus:

- Are the names and directory responsibilities understandable?
- Does the design avoid hidden cross-project mutation?
- Is the contract simple enough for a boring CLI implementation?

## Stack B: #114 CLI Implementation

Goal: implement local-only crag and team commands against the contract from
stack A.

Deliverables:

- `belayer team list`
- `belayer team add <category>`
- `belayer team add <category>/<team>`
- `belayer team remove <category>`
- `belayer team remove <category>/<team>`
- `belayer crag list`
- `belayer crag init <name>`
- `belayer crag link <name>`

Implementation constraints:

- Adds copy selected team identities into `.belayer/agents/`.
- Existing files are skipped by default.
- `--force` overwrites installed files.
- Path traversal and unknown category/talent inputs are rejected.
- `crag link` writes only a repo-local config pointer; it does not import global
  learnings into the repo.

Review focus:

- Is the CLI predictable and local-first?
- Are skip/force counts clear enough for operators?
- Are config edits minimal and reversible?

## Stack C: #115 Proof And Evaluation

Goal: prove the model in two different domains and produce a reviewable
evaluation packet.

Software-company proof:

- Test bed: `/Users/donovanyohan/Documents/Programs/personal/relay-ide`.
- Use a real relay-ide GitHub issue as the PRD.
- Prefer a small scoped issue, initially #320 if still appropriate.
- Produce `org-plan`, implementation evidence, gate evidence,
  `talent-evaluation`, and `org-retro`.
- Open a relay-ide PR if the selected issue is small and shippable; otherwise
  preserve a local evaluated change and explain why no PR was opened.

Story-world proof:

- Test bed: `/Users/donovanyohan/Documents/Programs/personal/belayer-athenaeum`.
- Scene: tavern celebration after the party's successful adventure.
- Use existing athenaeum agents as characters/world talents where possible.
- Generate short-lived NPC talents for the tavern scene.
- Persist at least one NPC into a rediscoverable local story/hiring pool.
- Produce `org-plan`, `world-state`, `continuity-report`,
  `talent-evaluation`, `org-retro`, generated NPC metadata, and a final
  three-act story artifact.

Evaluation packet:

- Did the same E2R loop work in both domains?
- Which artifacts felt natural versus forced?
- Were direct `org:*` events sufficient for observability?
- Did file artifacts provide enough queryability?
- Did the run expose a need for typed org tables?
- Did `talent-evaluation` capture useful growth signals?
- Did the software proof remain compatible with the existing PM gate?
- Did the story proof avoid software-company assumptions?
- Did generated NPC talent work as a short-lived worker model?
- When did a talent need to be running versus merely defined or assigned?
- What should #116, #118, #119, and #120 preserve or change?

Review focus:

- The output should be easy to review without replaying the whole run.
- The evaluation should include concrete artifact paths, session IDs if
  available, command/test outputs, and a go/no-go recommendation for promotion
  tooling.

## Developer Experience Note

The planning conversation that produced this stack is itself part of the product
surface Belayer should eventually support. Today the workflow is close to
"make a spec and ship it to Belayer." Organization mode should support a more
collaborative path:

```text
brainstorm with the operator
  -> converge on a plan the operator accepts
  -> persist the plan as artifacts/issues
  -> hand off to the organization to implement
  -> return reviewable evidence and decisions
```

That means proof runs should not only test execution. They should also record
which handoff instructions were easy for agents to consume, which questions were
missing, and what review surfaces made the result easy for the operator to
judge the next morning.
