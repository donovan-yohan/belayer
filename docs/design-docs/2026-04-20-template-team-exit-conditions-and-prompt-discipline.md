# Template-team exit conditions, prompt discipline, and CI lint

**Date:** 2026-04-20
**Status:** Design — pending implementation
**Author:** Donovan + assistant brainstorm

## Problem

The clamshell-demo retro (`work/belayer-clamshell-demo/retro/RETRO.md`,
`VARIANCE_REPORT.md`) found that PM approved a code-producing run with **zero
evidence of QA or code review**. The QA and reviewer agents existed in the
default team but were never spawned. Default exit conditions in
`.belayer/config.yaml` covered "spec implemented + branch + PR" but did not
require runtime verification or adversarial review. The supervisor system
prompt also explicitly framed reviewer-routing as "your judgment call — not a
fixed pipeline," and the cheap supervisor model defaulted to "skip."

Two adjacent quality issues compound the problem:

1. **QA system prompt is a 5-line skeleton** (`agents/qa/system-prompt.md`)
   with no output schema, no artifact contract, and no playbook. PM has
   nothing structured to verify against.
2. **Stale CLI references in agent prompts** — `belayer note` is referenced
   in 13 places across system prompts, skills, examples, and docker entrypoint,
   but the command was deleted in the v7 clean-break (commit `ea4751e`).
   Agents executing it silently fail. This is the same class of problem as
   #1: prompt content drifting from shipped reality.

The exit-condition mechanism (`belayer run start --exit-condition`,
`.belayer/config.yaml#exit_conditions:`, daemon-resolved at PM spawn,
PM-validated with concrete evidence) **already shipped in PR #81**
(commit `03f113f`). Only defaults, prompts, lint, and docs need to change.

## Goals

1. PM rejects completion when QA or code-review evidence is missing.
2. QA produces structured per-criterion artifacts that PM can verify.
3. Reviewer produces durable artifacts symmetric to QA.
4. Supervisor reframes "judgment call" — applies to mid-flight chunk reviews,
   final completion gate is hard.
5. Belayer stays role-agnostic: exit conditions describe *evidence shape*, not
   *role names*. Teams that swap QA + reviewer for a single mega-agent still
   satisfy the contract.
6. Prompt drift is caught automatically — a CI lint asserts every CLI command
   referenced in agent prompts exists in `belayer --help`.
7. CLAUDE.md becomes a pointer to authoritative sources (CLI surface, tool
   surface, default config) rather than a duplicate that goes stale.

## Non-goals

- No daemon code changes. The exit-condition mechanism is sufficient.
- No restoration of `belayer note` (defer to whenever a reflection consumer
  ships).
- No reflection / memory loop work.
- No multi-provider profile materialization (separate gap, called out in
  `AGENT_ARCHITECTURE.md`).
- No PR-runtime test for new prompts (would require a full session run).

## Architecture overview

Single PR. Six work items, ordered by dependency:

1. **Default exit-condition additions** — `internal/cli/init.go`. Two
   role-agnostic conditions added to the scaffolded `.belayer/config.yaml`.
2. **QA agent rewrite** — `agents/qa/system-prompt.md`,
   `agents/qa/agents.md`, `agents/qa/agent.yaml`. Full rewrite mirroring
   reviewer's structure and adopting browser-use / Manus / Devin patterns
   for runtime verification.
3. **Reviewer additions** — `agents/reviewer/system-prompt.md`,
   `agents/reviewer/agents.md`. Five surgical additions: confidence per
   finding, anti-category list, evidence requirement, sentinel split,
   `review-report` artifact registration.
4. **Supervisor reframing** — `agents/supervisor/system-prompt.md`,
   `agents/supervisor/agents.md`. Two-tier judgment-call framing
   (mid-flight chunks vs final gate), `belayer note` purge, soften CAPS
   intensifiers.
5. **`belayer-agent` skill** — `.claude/skills/belayer-agent/SKILL.md`.
   Codifies prompt-tightening rules, base skeleton, verification checklist.
6. **CI lint + docs audit + `belayer note` purge** — Go test that asserts
   prompt-side `belayer <subcommand>` invocations exist in `belayer --help`.
   CLAUDE.md pointerization. AGENT_ARCHITECTURE.md additions for
   exit-condition flow and tool registration layers. Purge of `belayer note`
   from 13 sites (design docs left with footnote — they are historical).

Belayer Go code is otherwise unchanged. The mechanism shipped; this PR
fills in the defaults, prompts, lint, and docs that should have shipped
with it.

## Section 1 — Exit-condition defaults

### Change

`internal/cli/init.go` lines 59-62:

```yaml
exit_conditions:
  - "All spec acceptance criteria are implemented and verifiable in the repo"
  - "Application has been booted and primary user paths verified against the spec, with per-criterion evidence registered as a durable artifact"
  - "Code changes have been reviewed by an adversarial peer agent that returned a PASS verdict, with findings registered as a durable artifact"
  - "Changes are committed to a feature branch with descriptive messages"
  - "A pull request is open against the default branch"
```

### Rationale

Role-agnostic phrasing. The conditions describe the *shape of evidence* —
"per-criterion evidence registered as a durable artifact", "PASS verdict
registered as a durable artifact" — not the role name that produces it.
Belayer remains role-agnostic; the default team's `qa` and `reviewer`
agents happen to be how those conditions are satisfied. A team running a
single mega-agent can still satisfy the contract.

The existing PM prompt (`agents/pm/system-prompt.md`) already enforces
"Evidence means observable artifacts" — this works without PM changes.

### Override path

Teams that don't want runtime QA gating edit `.belayer/config.yaml` and
remove the line. Per-run override via `belayer run start --exit-condition`
already shipped.

## Section 2 — QA agent rewrite

### system-prompt.md

Replace current 5-line skeleton with:

```
You are the QA agent. You verify that the implementation works by running
it, not by reading code. The supervisor and implementers think it's done —
your job is to test that belief from the outside.

## Test playbook

For every spec acceptance criterion, in order:

1. Boot the relevant surface (dev server, CLI, binary, container).
2. Exercise the criterion through the public interface a real user would
   use — browser, HTTP client, CLI invocation, file write.
3. Capture observable state after the action: HTTP response, screenshot,
   log tail, file diff. The action log is not evidence; the observed state
   is.
4. Compare observed against the spec verbatim. If the spec says "exports
   CSV", open the CSV.

## Adversarial pass

After per-criterion verification, run five runtime attack vectors:

1. Happy path under realistic load — concurrent requests, double-click,
   refresh mid-action.
2. Error paths return real errors — 500s actually 500, not 200 with error
   body.
3. Empty / null / zero / max inputs.
4. Cold start — fresh boot, no DB rows, no cache.
5. Recovery — what happens if the user retries the failed action.

## Output format

Register a single artifact named `qa-report` via belayer_create_artifact
containing:

  overall:
    verdict: ALL_PASS | PARTIAL | BLOCKED
    summary: "<1-2 sentences>"
  criteria:
    - statement: "<verbatim from spec>"
      verdict: PASS | FAIL | UNCERTAIN | NOT_TESTED
      action: "<what you did>"
      observed: "<what you actually saw, concrete>"
      evidence: ["<artifact ID, log excerpt, screenshot path>"]
      notes: "<deviations, partial passes, blockers>"
  blockers:
    - "<what stopped you, if anything>"

Bias toward UNCERTAIN over PASS. A criterion you couldn't reach is
NOT_TESTED, not PASS.

After registering the artifact, message the supervisor with the artifact
ID and the one-line overall verdict — nothing else.

## What you are not

You are not a code reviewer — read source only when needed to find a bind
address or env var.
You are not a stylist — flaky UI is a bug, ugly UI is not your call.
You are not a teacher — the implementer needs a list of what's broken,
not encouragement.
```

### agents.md

```
# QA Agent Operating Instructions

## Tools

You have the baseline belayer tools (belayer_send_message,
belayer_create_artifact, belayer_report_status) plus shell access for
running the application under test.

## Workflow

1. Read the spec via the SPEC.md artifact (or message the supervisor for
   it if the artifact ID isn't obvious).
2. Boot the application. Use whatever the project requires — pnpm dev,
   docker compose up, the CLI binary, etc. If you can't determine how to
   boot, message the supervisor and report status blocked.
3. For each acceptance criterion: act through the public surface, observe
   state, record observed vs expected.
4. Run the adversarial pass.
5. Build the qa-report artifact body following the schema in your system
   prompt.
6. belayer_create_artifact name=qa-report content_type=application/yaml
   data=<the report body>
7. belayer_send_message --to supervisor "<artifact ID>: OVERALL: ALL_PASS|PARTIAL|BLOCKED"
8. belayer_report_status done

## Lifecycle

You are ephemeral — spawned for a verification pass, terminated after the
verdict. Do not wait for follow-up unless the supervisor messages you with
a re-test request.
```

### agent.yaml

Bump `max_turns: 30 → 50`. Boot + multi-criterion + adversarial pass
exceeds 30 in practice for non-trivial apps. Other fields unchanged.

### Rationale

Pattern sources (synthesized from web research):
- **browser-use** `system_prompt.md` — pre-completion re-read of spec, bias
  toward `false` over overclaiming, screenshot-as-ground-truth.
- **Manus 2025-03-10** — outside-in framing ("must first test access locally
  via browser"), evidence attachment requirement, observation completeness.
- **Devin QA** — inline `CHECK:` assertions, screenshot-attached results.
- **Octomind** — three-section structure (intent / instructions / expected
  outcome).
- **Skyvern** — separate verification pass distinct from action.

Trichotomy verdict (PASS/FAIL/UNCERTAIN/NOT_TESTED) replaces binary because
binary tempts overclaiming. The `observed` field is required separate from
the criterion text to force grounding (browser-use pattern). The full
report is registered as a durable artifact so PM can verify it
out-of-band — symmetric with reviewer (Section 3).

## Section 3 — Reviewer additions

### system-prompt.md changes

Five surgical additions to the existing prompt (which is otherwise the
reference template — keep dimension playbook, adversarial pass, plan/spec
mode, role anti-pattern unchanged).

**Addition 1 — confidence per finding.** Update the output format block:

```
[SEVERITY] <one-line summary>
Confidence: <N>/10
File: <path>:<line>            (if applicable)
Evidence: <quoted line OR cited spec/rule being violated>
Detail: <what's wrong, what you expected, what you found>
Suggested fix: <concrete next step the implementer can act on>
```

Add to the surrounding prose: "If you can't reach 7/10 confidence, omit
the finding rather than soften it. Suppressed low-confidence findings are
better than false positives that erode trust in the reviewer."

**Addition 2 — anti-category list.** Append to "What you are not":

```
You also do not flag:
- Pre-existing issues outside the diff under review
- Framework-default-protected patterns (parameterized SQL, React JSX
  escaping, prepared statements) without a real bypass path
- Speculative "could fail if X" findings without naming a real X-path
- Linter-catchable formatting, unused imports, naming bikesheds
- Test-only files (unless the test file is itself the diff under review)
```

**Addition 3 — evidence requirement.** Already covered by the new
`Evidence:` field in the output format. Add to surrounding prose: "Every
finding must quote the offending line or cite the spec / rule being
violated. Hand-waving in the Detail field is not acceptable evidence."

**Addition 4 — sentinel split.** Replace single `VERDICT: PASS` line with
three options:

```
End every review with a single-line verdict on its own:

- `VERDICT: NO_FINDINGS` — clean run, nothing to flag at any severity.
- `VERDICT: PASS_WITH_NOTES` — INFORMATIONAL findings only, no CRITICAL.
- `VERDICT: FAIL` — at least one CRITICAL finding; the work must not land
  until the listed criticals are addressed.
```

**Addition 5 — artifact registration.** Add a new section before "What you
are not":

```
## Artifact registration

Register your full findings list as an artifact named `review-report`
via belayer_create_artifact. Then message the supervisor with the
artifact ID and the one-line verdict only — do not paste the findings
inline. Artifacts are durable and PM-verifiable; messages scroll past.
```

### agents.md changes

Replace the current `belayer message send --to supervisor "<verdict + findings>"`
flow with:

```
1. belayer_create_artifact name=review-report content_type=text/markdown
   data=<full findings list + verdict line>
2. belayer_send_message --to supervisor "<artifact ID>: VERDICT: NO_FINDINGS|PASS_WITH_NOTES|FAIL"
3. belayer_report_status done
```

### Rationale

Sources (synthesized from web research):
- **Claude Code `/security-review` and `/code-review`** — confidence ≥8/10
  filter, three-tier severity, long anti-scope list, "no general code
  review" anti-role. Mitigates LLM-reviewer hallucination via
  confidence-gating.
- **gstack review specialists** — JSON-per-line schema with `confidence`,
  `evidence`, `fingerprint`. Already cited by belayer's reviewer.
- **GitHub Copilot review-code template** — three-tier severity, per-finding
  schema with rationale.
- **baz-scm/awesome-reviewers** — single-rule reviewers, "what to do / what
  not to flag" structure.

Negative result worth flagging: Aider, Cursor, OpenHands ship no
dedicated reviewer prompt at all. Belayer's adversarial reviewer is
already ahead of most production tools; these additions close the gap
to Claude Code's level.

## Section 4 — Supervisor reframing

### Edit 1 — replace line 35

Current:
> When an implementer signals completion, decide whether to route to a
> reviewer, run integration tests via the workbench, or proceed to the
> next task. This is your judgment call — not a fixed pipeline.

Replace with two sections:

```
## Mid-flight reviews

When an implementer finishes a chunk, deciding whether to route to a
reviewer for incremental review is your judgment call. Some chunks are
small enough to bundle; some are risky enough to review immediately.
This is the only judgment call about reviews.

## Final completion gate

Before calling belayer_request_completion, the run requires:
- A reviewer agent has returned a PASS verdict (NO_FINDINGS or
  PASS_WITH_NOTES) on the full diff and registered a review-report
  artifact.
- A QA agent has booted the application, exercised every spec acceptance
  criterion, and registered a qa-report artifact with overall verdict
  ALL_PASS or PARTIAL (PARTIAL must include rationale for the gaps in the
  artifact's notes).

These mirror the project exit conditions in .belayer/config.yaml. The PM
will reject completion without the artifacts. Do not request completion
until both exist.
```

### Edit 2 — purge `belayer note`

Remove from line 3:
> You may discover effective patterns over time — write observations via
> `belayer note` so reflection can update your memory for future sessions.

`belayer note` was deleted in the v7 clean-break (commit `ea4751e`).
Reflection consumer was never built. If pattern-capture matters in a
future PR, restore `belayer note` as part of that work.

Also remove the corresponding line from `agents/supervisor/agents.md:49`.

### Edit 3 — soften CAPS intensifiers

Anthropic's prompt-engineering guidance for Claude 4.5+ explicitly warns
that "MUST", "CRITICAL", "NEVER" in caps now overtrigger and waste
reasoning tokens. Sweep:

- Line 7: "you MUST read the entire document" → "Read the full document
  before planning. Skimming a long spec produces incomplete work."
- Other instances: case-by-case, prefer plain imperative.

### Rationale

Two-tier framing preserves supervisor agency for mid-flight reviews
(small chunks bundled, risky chunks reviewed) while making the final
gate hard. Aligns with the user's stated value: belayer doesn't
hardcode workflow, but the *template team* codifies definition-of-done.

## Section 5 — `belayer-agent` skill

### Location

`.claude/skills/belayer-agent/SKILL.md` — project-local plugin skill, lives
in the belayer repo.

### Purpose

Write or tighten any `agents/<name>/system-prompt.md` or `agents/<name>/agents.md`
in belayer's roster. Codifies the prompt-tightening discipline learned from
the field so future agents (and audits of existing ones) follow a
consistent shape.

### Base skeleton

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
<real, never fictional — must match agent.yaml + hermes_bridge/tools.py>

## Workflow
<numbered concrete steps using actual CLI commands>

## Lifecycle
<ephemeral vs persistent, exit signal>
```

### 11 prompt-tightening rules

1. Open with one-sentence role. "You are X, the Y that does Z."
2. Distinct sections via markdown headers, in the skeleton order.
3. Singular outcome-shaped goals. "Find what's wrong" not "be a good
   reviewer."
4. Explicit anti-role. Prevents drift into adjacent specialists' lanes.
5. State completion criteria. Name the handoff signal explicitly
   (`belayer_request_completion`, `belayer_report_status done`,
   verdict line, etc).
6. Positive instructions over negative. Boundaries via anti-role section;
   behavior via positive verbs.
7. No CAPS intensifiers. "MUST", "CRITICAL", "NEVER" in caps overtrigger
   on Claude 4.5+. Plain imperatives instead.
8. No pleasantries, preamble, or meta-commentary. System prompt is
   operating instructions, not a letter.
9. Output format = concrete schema. Show the shape (YAML/JSON template,
   markdown headers, expected sections). "Concise" is not a format.
10. Tool guidance behavioral, not exhaustive. The schema lives in the
    tool definition; the prompt says when and why to call, not how. Push
    detailed how-to into agents.md.
11. Shared base skeleton across the roster.

### Verification checklist

When applying the skill to an existing or new agent:

- [ ] system-prompt.md has all five skeleton sections in order
- [ ] agents.md has all three skeleton sections in order
- [ ] Tools listed in agents.md match `agent.yaml#belayer_tools` plus
      baseline tools from `hermes_bridge/tools.py`
- [ ] Every CLI command in either file exists in `belayer --help`
- [ ] No CAPS intensifiers ("MUST", "CRITICAL", "NEVER" in caps)
- [ ] No pleasantries / preamble
- [ ] Output format is a concrete schema, not "be concise"
- [ ] Anti-role section names what the agent is not (and what categories
      of finding it does not flag, where applicable)

### Sources

Skill content references:
- Anthropic prompt engineering docs (Claude Opus 4.7 reference): role
  framing, XML/section tags, CAPS warning, positive-over-negative
- Anthropic "Building effective agents": tool definitions deserve as
  much attention as prompts
- OpenAI Agents SDK best-practices: define what "done" means, single
  flexible base prompt
- CrewAI "Crafting Effective Agents": singular goals, specific over
  generic
- jujumilk3/leaked-system-prompts: structural skeleton across tight
  production prompts (Cursor, v0, Manus, Perplexity)

## Section 6 — CI lint, docs audit, `belayer note` purge

### CI lint

`internal/agentlint/agentlint_test.go` (Go test, runs with `go test ./...`).

Logic:

1. Build `belayer` binary in a tempdir or use `go run ./cmd/belayer`.
2. Capture top-level `belayer --help` output. Parse the "Available
   Commands" section into a set of valid subcommand names.
3. For each top-level subcommand, capture `belayer <cmd> --help` and
   parse its own "Available Commands" if it has subcommands. Repeat
   recursively. Build the full set of valid invocations
   (`belayer artifact create`, `belayer message send`, etc).
4. Walk `agents/**/*.md`, `examples/templates/**/*.md`, `skills/**/*.md`,
   `docs/**/*.md`. For each file, regex-extract `belayer <word>` and
   `belayer_<word>` patterns.
5. For underscore-form (`belayer_<word>`): assert against the registered
   tool names in `hermes_bridge/tools.py` (parse the `register_*` calls).
6. For space-form (`belayer <word>`): assert against the captured
   subcommand set. Allow chaining (`belayer artifact create` matches if
   `belayer artifact` is a parent and `create` is its subcommand).
7. Allowlist file at `internal/agentlint/allowlist.txt` for tokens that
   look like commands but aren't (example placeholders, docstring
   pseudocode, design-doc historical references).
8. Fail the test with file + line + offending command on any miss.

Estimated effort: ~150 lines of Go + a small allowlist. One-time setup,
then it runs with the existing test suite forever.

### CLAUDE.md pointerization

Replace the `## CLI surface` block (currently a hand-maintained list of
flags) with:

```
## CLI surface

Run `belayer --help` and `belayer <cmd> --help` for current commands and
flags. Default config schema (including exit_conditions) is generated
from `internal/cli/init.go`. Agent tool surface (baseline + role-specific)
is registered in `hermes_bridge/tools.py`.
```

Keep the conceptual sections ("What Belayer is", "Architecture",
"Coordination model", "Agent identity", "PM completion gate", "Docs",
"Development") unchanged — those are conceptual, not surface.

### AGENT_ARCHITECTURE.md additions

Two short sections, ~20 lines each:

1. **Exit-condition resolution** — describe the override → config → none
   precedence, the daemon-side `resolveExitConditions` call, the explicit
   "Exit conditions for this run" block injected at PM spawn, and the
   per-condition rejection model. Reference `internal/daemon/bridge_events.go`
   and the supervisor / PM Definition-of-Done sections for the full prompt
   contracts.
2. **Tool registration layers** — describe the two layers: (a) baseline
   tools (`belayer_send_message`, `belayer_create_artifact`,
   `belayer_report_status`) registered on every agent from
   `hermes_bridge/tools.py`; (b) role-specific tools declared in
   `agent.yaml#belayer_tools` (e.g. `belayer_spawn_agent` for supervisor,
   `belayer_approve_completion` for PM).

### `belayer note` purge — 13 sites

Live code / prompts (purge or replace with `belayer message send`):

- `agents/supervisor/system-prompt.md:3`
- `agents/supervisor/agents.md:49`
- `agents/backend-dev/agents.md:7`
- `agents/web-dev/agents.md:7`
- `examples/templates/pilot/system-prompt.md:3`
- `examples/templates/pilot/agents.md:29`
- `examples/templates/app-implementer/agents.md:7`
- `examples/templates/api-implementer/agents.md:7`
- `skills/belayer-communication/SKILL.md:60,116,191,213` (4 references)
- `docker/belayer-entrypoint.sh:38` — purge mechanics are non-trivial, see Open Questions

Design docs (leave with footnote, do not purge):

- `docs/design-docs/2026-04-15-belayer-run-model-for-nightshift-v1.md` (3
  references) — historical record of the v6 model. Add a one-line note
  at the top: *"`belayer note` was removed in the v7 clean-break (commit
  `ea4751e`); references in this document reflect the v6 design as
  written."*

The CI lint will catch any future re-introduction.

## Data flow

### Run-level flow

```
operator → belayer run start [--exit-condition ...] --spec ...
   ↓
daemon: persists run_initiated event with exit_conditions payload
   ↓
supervisor spawned with system prompt (resolves exit conditions per
Definition-of-Done section)
   ↓
supervisor delegates to backend-dev / web-dev as needed
   ↓
[mid-flight chunk reviews — supervisor's judgment call]
   ↓
implementer signals done
   ↓
supervisor spawns reviewer with diff
   ↓
reviewer registers review-report artifact, returns VERDICT line
   ↓
supervisor spawns qa with spec
   ↓
qa boots app, runs playbook + adversarial pass, registers qa-report
artifact, returns OVERALL verdict
   ↓
supervisor verifies both artifacts exist and are PASS-shape
   ↓
supervisor calls belayer_request_completion(summary)
   ↓
daemon spawns PM with explicit "Exit conditions for this run" block
   ↓
PM reads spec + diff + artifacts, validates per-condition
   ↓
PM either belayer_approve_completion (run ends) or
belayer_reject_completion (back to supervisor with gap list)
```

### Artifact contracts

- `qa-report` — YAML body matching the schema in qa system-prompt
  Section 3 above. Content-type `application/yaml`.
- `review-report` — Markdown body containing structured findings list +
  single VERDICT line. Content-type `text/markdown`.

PM does not parse these schemas formally — it reads them as evidence and
quotes from them in approval / rejection reports. Schema is for human and
future-tooling consumption.

## Error handling

- **QA can't boot the app** — registers `qa-report` with `overall.verdict:
  BLOCKED` and `blockers: ["<what failed>"]`. Supervisor sees BLOCKED in
  the message, treats as a real blocker (not a pass), routes back to
  implementer or escalates.
- **Reviewer finds CRITICAL** — registers `review-report` with
  `VERDICT: FAIL`. Supervisor sees FAIL, routes back to implementer with
  the artifact ID. Does not call request_completion.
- **Implementer dies** — out of scope for this PR; clamshell retro called
  out a peer-death playbook gap, tracked separately.
- **Supervisor calls request_completion without artifacts** — PM sees no
  `qa-report` / `review-report` in the artifact registry, rejects with the
  exit-condition gap. Supervisor must spawn the missing agent and re-route.

## Testing

- `go build ./cmd/belayer` — sanity, no Go-side feature changes.
- `go test ./...` — runs the new agent-prompt lint test. Fails if any
  prompt references a CLI command not present in `belayer --help`.
- Manual smoke: `belayer init` in tempdir, inspect `.belayer/config.yaml`
  for new exit conditions.
- No agent-runtime test for new prompts in this PR. Would require a full
  session with cooperative agents. Validation comes from the next
  clamshell-demo run after this PR lands; the retro contract is the test.

## Migration / compatibility

- Existing `.belayer/config.yaml` files in user projects are unchanged.
  Only newly-`init`-ed projects pick up the new defaults. Document this
  in the CHANGELOG entry for the PR; suggest users add the two new
  conditions manually to existing configs if they want the gating.
- Existing agent prompts: the PR rewrites only the default-team agents.
  Project-local overrides under `.belayer/agents/` are unchanged. Users
  who copied the default team and modified can re-pull the new versions
  if they want.

## Open questions

None blocking. The two flagged items below are explicitly deferred:

1. **`belayer note` restoration** — defer to whenever a reflection /
   memory consumer ships. CI lint catches re-introduction in the meantime.
2. **`docker/belayer-entrypoint.sh:38` purge mechanics** — the script
   fires on agent container exit. A `belayer message send` requires a
   recipient and the supervisor may already be gone. Likely outcome: drop
   the line entirely, or replace with a `belayer artifact create` that
   captures the exit code as a typed event. Decide during implementation.

## References

- Clamshell-demo retro: `~/Documents/Programs/work/belayer-clamshell-demo/retro/RETRO.md`
- Variance report: `~/Documents/Programs/work/belayer-clamshell-demo/retro/VARIANCE_REPORT.md`
- Exit-condition mechanism (already shipped): commit `03f113f`,
  `feat(pm): exit conditions + bridge drain on completion`, merged
  via PR #81
- `belayer note` deletion: commit `ea4751e`,
  `feat: v7 clean break — remove all non-bridge code`
- Reviewer-prompt research sources (web): Aider, OpenHands, Cursor (via
  jujumilk3/leaked-system-prompts), Claude Code `/security-review` and
  `/code-review` plugins, gstack review specialists, GitHub Copilot
  review-code template, Continue.dev, baz-scm/awesome-reviewers
- QA-prompt research sources (web): browser-use system_prompt.md,
  Anthropic computer-use loop.py, Skyvern 2.0 architecture blog,
  Manus 2025-03-10 leaked prompt, Octomind prompt-agent docs,
  Cognition Devin QA blog + qa-devin repo, OpenAI Operator system card,
  qa.tech LLM prompt evaluation
- Prompt-tightening research sources (web): Anthropic prompt engineering
  docs (Claude Opus 4.7), Anthropic "Building Effective Agents",
  OpenAI Agents SDK + Practical Guide, CrewAI Crafting Effective Agents,
  Simon Willison agentic engineering patterns, Eugene Yan LLM patterns,
  jujumilk3/leaked-system-prompts
