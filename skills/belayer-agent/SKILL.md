---
name: belayer-agent
description: Write or tighten any agents/<name>/system-prompt.md or agents/<name>/agents.md in belayer's roster. Use when authoring a new belayer agent identity, auditing an existing one, or reviewing roster PRs. Codifies the prompt-tightening discipline learned from the field.
---

## Purpose

Invoke this skill when authoring a new belayer agent identity, auditing an existing one, or reviewing roster PRs that touch `agents/<name>/`. It enforces a consistent skeleton across all agent files, applies 11 field-tested prompt-tightening rules, and provides a verification checklist so nothing ships with vague output formats, CAPS intensifiers, or fictional tool references.

---

## Base skeleton

Every `system-prompt.md` follows:

```
<one-sentence role>

## Scope
<what this agent does and explicitly does not — sets the lane>

## Playbook
<numbered per-task steps>

## Output format
<concrete schema, not adjective>

## What you are not
<anti-role + anti-categories — prevents lane overlap and overreach>
```

Every `agents.md` follows:

```
# <Agent Name> Operating Instructions

## Tools
<real, never fictional — must match agent.yaml + plugins/belayer/tools.py>

## Workflow
<numbered concrete steps using actual CLI commands>

## Lifecycle
<ephemeral vs persistent, exit signal>
```

---

## 11 prompt-tightening rules

1. Open with one-sentence role. "You are X, the Y that does Z."
2. Distinct sections via markdown headers, in the skeleton order.
3. Singular outcome-shaped goals. "Find what's wrong" not "be a good reviewer."
4. Explicit anti-role. Prevents drift into adjacent specialists' lanes.
5. State completion criteria. Name the handoff signal explicitly (`belayer_request_completion`, `belayer_report_status done`, verdict line, etc).
6. Positive instructions over negative. Boundaries via anti-role section; behavior via positive verbs.
7. No CAPS intensifiers. "MUST", "CRITICAL", "NEVER" in caps overtrigger on Claude 4.5+. Plain imperatives instead.
8. No pleasantries, preamble, or meta-commentary. System prompt is operating instructions, not a letter.
9. Output format = concrete schema. Show the shape (YAML/JSON template, markdown headers, expected sections). "Concise" is not a format.
10. Tool guidance behavioral, not exhaustive. The schema lives in the tool definition; the prompt says when and why to call, not how. Push detailed how-to into agents.md.
11. Shared base skeleton across the roster.

---

## Verification checklist

When applying the skill to an existing or new agent:

- [ ] system-prompt.md has all five skeleton sections in order
- [ ] agents.md has all three skeleton sections in order
- [ ] Tools listed in agents.md match `agent.yaml#belayer_tools` plus baseline tools from `plugins/belayer/tools.py`
- [ ] Every CLI command in either file exists in `belayer --help`
- [ ] No CAPS intensifiers ("MUST", "CRITICAL", "NEVER" in caps)
- [ ] No pleasantries / preamble
- [ ] Output format is a concrete schema, not "be concise"
- [ ] Anti-role section names what the agent is not (and what categories of finding it does not flag, where applicable)

---

## Sources

Skill content references:
- Anthropic prompt engineering docs (Claude Opus 4.7 reference): role framing, XML/section tags, CAPS warning, positive-over-negative
- Anthropic "Building effective agents": tool definitions deserve as much attention as prompts
- OpenAI Agents SDK best-practices: define what "done" means, single flexible base prompt
- CrewAI "Crafting Effective Agents": singular goals, specific over generic
- jujumilk3/leaked-system-prompts: structural skeleton across tight production prompts (Cursor, v0, Manus, Perplexity)
