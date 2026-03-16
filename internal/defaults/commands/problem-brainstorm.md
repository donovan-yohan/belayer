---
description: Design a problem through collaborative dialogue before handing off to belayer
argument-hint: "[description of work]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep", "Skill"]
---

Route to the appropriate harness design skill based on the type of request:

| Request Type | Skill | When to use |
|-------------|-------|-------------|
| New feature | `/harness:brainstorm` | User wants to build something new, add functionality, create a component |
| Bug fix | `/harness:bug` | User reports a bug, error, unexpected behavior, or something broken |
| Refactor | `/harness:refactor` | User wants to restructure, rename, extract, or clean up existing code |

If the request type is ambiguous, ask the user before routing.

After the design phase completes and before handing off to `/problem-create`, verify the following checklist:

1. **Test Contract** — spec.md includes a `## Test Contract` section with an acceptance test table (T-1, T-2, etc.). If it's missing, go back and complete it with the user before proceeding.
2. **Prerequisite climbs** — if the user mentioned missing test infrastructure (no test framework, no CI, no test helpers), climbs.json includes prerequisite climbs with `id: <repo>-0` and `depends_on: []` that set up the infrastructure before feature climbs run.
3. **Learnings** — if `belayer learnings list` returns active learnings relevant to this work, incorporate the recommendations into the spec or climbs before publishing.

After the design phase completes and a design doc is saved, override the default next steps. Do NOT suggest `/harness:plan`, `/harness:orchestrate`, or the standard harness workflow. Instead:

## Next Steps (say this exactly)

> Design saved. To turn this into a belayer problem:
>
> 1. `/problem-create` — Write spec.md + climbs.json and publish to belayer
>
> Or if you want to refine the plan before handing off:
>
> 2. `/harness:plan` → then `/problem-create` when the plan is solid

**Important:** In a setter session, execution happens through belayer leads, not through `/harness:orchestrate`. The harness skills are for design and planning only. The handoff to execution is always `belayer problem create`.
