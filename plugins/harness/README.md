# harness

Claude Code plugin for structured documentation lifecycle management. Transforms monolithic CLAUDE.md files into a 3-tier progressive disclosure system with persistent learnings across sessions.

## Workflow

```
brainstorm -> plan -> orchestrate -> review -> reflect -> evolve -> complete
```

| Command | Purpose |
|---------|---------|
| `/harness:init` | Initialize 3-tier documentation structure |
| `/harness:brainstorm` | Design through collaborative dialogue, produce design docs |
| `/harness:bug` | Systematic bug investigation with architecture review |
| `/harness:refactor` | Plan incremental refactoring with strangler fig patterns |
| `/harness:plan` | Create living execution plans from design docs |
| `/harness:orchestrate` | Execute plans with agent teams and micro-reflects |
| `/harness:batch` | Execute plans via worktree-isolated parallel batch |
| `/harness:review` | Quality review with adversarial code review |
| `/harness:reflect` | Capture learnings and update docs |
| `/harness:evolve` | Classify learnings, update metrics, and propose agent evolution based on review evidence |
| `/harness:complete` | Archive plan, update docs, create PR |
| `/harness:prune` | Audit docs for staleness, broken links, bloat |

## Agents

- **harness-pruner** - Audits documentation health, finds stale/orphaned guides, checks CLAUDE.md bloat
- **harness-evolver** - Generates agent evolution proposals from signals (escapes, metric anomalies, universal learnings)
- **learnings-reviewer** - Checks code changes against accumulated project learnings for violations

## Skills

- **strangler-fig** - Incremental refactoring patterns for breaking up large modules

## Documentation Tiers

1. **CLAUDE.md** (60-120 lines) - Map with Documentation Map table
2. **docs/*.md** - Domain summaries (ARCHITECTURE, DESIGN, PLANS, QUALITY)
3. **docs/design-docs/**, **docs/exec-plans/** - Deep docs, versioned plans

## Installation

Add to your Claude Code project as a plugin:

```json
{
  "plugins": ["path/to/harness"]
}
```
