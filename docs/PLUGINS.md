# Plugin Development

Patterns, conventions, and authoring rules for the `plugins/harness/` and `plugins/pr/` Claude Code plugins shipped with belayer.

## Current State

Belayer ships three plugins: **harness** (documentation lifecycle), **pr** (pull request lifecycle), and **explorer** (spec submission). All are embedded via `embed.FS` and installed by `belayer init`.

## Key Patterns

### Skill & Agent Invocation Mandate

When a command delegates to another skill or agent, it MUST specify the invocation mechanism explicitly:

- **Cross-skill delegation**: Use `Skill("plugin:skill-name")` — never say "invoke X" without mandating the Skill tool
- **Agent spawning**: Use `subagent_type: "plugin:agent-name"` — never spawn generic agents for work that has a specialized plugin agent
- **Rationale**: Without explicit invocation, models interpret "invoke" as "produce the output from memory," skipping the loaded skill's systematic methodology. This caused a loop failure (2026-03-25) where deliverables were missed.

Example patterns:
```
# Good — mandates Skill tool
4. **Invoke `superpowers:writing-plans`** using the Skill tool: `Skill("superpowers:writing-plans")`.

# Good — mandates subagent_type
Agent(subagent_type="pr-review-toolkit:code-reviewer", prompt="...", run_in_background=true)

# Bad — ambiguous, model may skip loading the skill
4. **Invoke `superpowers:writing-plans`** with the design context.

# Bad — spawns generic agent without specialized system prompt
Agent(description="Code review", prompt="Review the code...")
```

### HARNESS_OVERRIDES / LOOP_OVERRIDES

When harness commands delegate to superpowers skills, they wrap the delegation with override blocks:

```markdown
<HARNESS_OVERRIDES>
These overrides REPLACE conflicting instructions from superpowers:writing-plans.
- **Save location:** Save to `docs/exec-plans/active/`, NOT `docs/superpowers/plans/`.
- **Execution handoff:** Do NOT invoke execution skills after planning.
</HARNESS_OVERRIDES>
```

Rules:
- Overrides take ABSOLUTE PRECEDENCE over the delegated skill's instructions
- Always specify what NOT to do (negative constraints prevent the delegated skill from running its default handoff)
- Place overrides immediately after the invocation line, before any subsequent steps

### MANDATORY Blocks

For critical constraints that models tend to shortcut, use `<MANDATORY>` blocks:

```markdown
<MANDATORY>
You MUST use the Skill tool to invoke `/harness:plan`.
Do NOT write the plan from memory or conversation context.
</MANDATORY>
```

Use sparingly — only for constraints where shortcutting has caused actual failures.

### Merge-Friendly Document Formats

All harness-generated documents use formats designed for concurrent multi-agent work:

| Format | Use | Why |
|--------|-----|-----|
| `L-YYYYMMDD-slug` | Learning IDs | No sequential scan needed; unique across branches |
| Bullet lists | Index files | Git merges non-adjacent additions cleanly |
| Append-only | LEARNINGS.md | No reordering, no renumbering |
| Section headers | Design doc index | Entries grouped by status, not in a single table |

Legacy formats (sequential `L-NNN` IDs, markdown tables in indexes) are detected by `/harness:prune` Step 23-24 and auto-migrated via `--fix`.

### Version Sync

Plugin versions must be updated in 3 locations simultaneously:

1. `plugins/{name}/.claude-plugin/plugin.json` → `"version"` field
2. `internal/plugins/registry.go` → `{Name}Version` constant
3. `agentassets_test.go` → `TestPluginVersion` expected values

`go test . -run TestPluginVersion` catches mismatches. Bump on every plugin change:
- **Major**: Breaking format changes (e.g., learning ID format migration)
- **Minor**: New features, behavioral fixes
- **Patch**: Doc-only fixes, typo corrections

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| Mandate Skill tool for cross-skill delegation | Models skip methodology when delegating from memory (L-20260325-mandate-skill-invocation) |
| Mandate subagent_type for plugin agents | Generic agents lack specialized system prompts (L-20260325-subagent-type-required) |
| Date-slug learning IDs over sequential | Prevents merge conflicts in concurrent work (L-20260325-merge-friendly-formats) |
| Bullet lists over tables for indexes | Tables cause adjacent-line merge conflicts |
| Prune detects legacy formats | No separate migration tooling needed (L-20260325-prune-as-migration) |
| `/simplify` over code-simplifier:code-simplifier | Built-in command, always available, no plugin dependency |

## Deep Docs

| Document | When to look here |
|----------|-------------------|
| `plugins/harness/commands/_learnings-format.md` | Learning ID format, scaffold, consulting algorithm |
| `plugins/harness/agents/harness-pruner.md` | Audit checks, legacy format detection, fix procedures |
| `plugins/harness/commands/loop.md` | Autonomous execution, MANDATORY blocks, deliverable traceability |
| `plugins/pr/commands/review.md` | PR review agent orchestration, subagent_type mandate |
| `docs/debug-logs/2026-03-25-loop-planning-failure.md` | Root cause analysis of the invoke-from-memory failure |
