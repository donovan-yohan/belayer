# Org Mode Proof Evaluation

## Summary

The artifact-first model works well enough to proceed with the next runtime
designs, but the proofs expose two important follow-ups:

- The PM path should become the default `acceptance` gate preset inside a
  configurable gate framework.
- Large team catalogs need lazy lifecycle. Most teams should be definitions or
  assigned workers until a task actually needs a running process.

## Proofs

| Proof | Test bed | Output |
|-------|----------|--------|
| Software company | relay-ide issue #320 | Planning, review, QA, acceptance, retro, talent evaluation |
| Story world | belayer-athenaeum tavern celebration | Three-act story, world-state, continuity report, generated NPC |

## E2R Fit

Both proofs used the same loop:

1. Explore: create `org-plan`.
2. Execute: assign teams and produce domain output.
3. Review: produce gate evidence.
4. Retro: produce `org-retro` and `talent-evaluation`.

The loop is clearer than the current "make a spec and ship to Belayer" flow
because it gives the operator review points before execution and after proof.

## Artifact Fit

Natural artifacts:

- `org-plan`
- `gate-result`
- `world-state`
- `continuity-report`
- `org-retro`

More awkward artifacts:

- `talent-evaluation` is useful, but it wants aggregation over time. One climb is
  only a sample, not a maturity signal.
- Software runtime evidence wants links to real command output or PR checks.
  A static packet can describe the expected evidence, but real runs should
  attach command logs or PR URLs.

## Event Fit

Direct `org:*` events are enough for first-pass observability. The missing piece
is not event shape; it is policy. Consumers will need to know which gate events
are required for a given task. That belongs in #118 workflow discovery.

## Queryability

File artifacts are enough for #115 review. Typed tables become useful when we
need cross-session queries:

- Which teams repeatedly fail a specific gate?
- Which generated NPCs are candidates for promotion?
- Which SOP changes were accepted or rejected?
- Which task traits usually require a security or continuity gate?

That argues for #116/#118 after proof, not before it.

## Generated NPC Result

The tavern scene generated `mara-underbough`, a tavernkeep. Persisting her as
compact metadata worked better than creating a full `.belayer/agents/` identity
immediately. She is rediscoverable without dragging the whole story transcript
into future context.

## Lazy Lifecycle Result

The proofs separate three states:

- available: team identity exists in catalog or crag metadata
- assigned: team identity is selected for a task or scene beat
- active: a running agent process is needed

The story proof especially benefits from this. Townsfolk and guards should not
be idle processes. They should be generated, used, evaluated, and either
discarded or persisted.

## Recommendation

Proceed with:

1. #118 gate framework and workflow discovery.
2. #119 lazy talent lifecycle.
3. #120 generated team persistence.
4. #116 reviewed promotion flow after those contracts are clearer.

Do not add crag database tables yet. Keep the next implementation local-first
and artifact-driven until real sessions produce repeated query pressure.
