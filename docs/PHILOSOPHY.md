# Philosophy

Foundational thinking behind belayer's architecture. This document captures the WHY, not the HOW. Reference this when brainstorming new features, evaluating competing approaches, or explaining belayer's position in the agentic coding space.

Status: `active` — last updated 2026-03-29
Source: Slate binary analysis session + adversarial review debate

---

## The Three Roles

Agent, Harness, and Orchestrator are separate concerns. Belayer is the Orchestrator.

**Agent** = raw model capability. Claude, Codex, Slate, Gemini, whatever comes next. The agent reads code, writes code, runs commands, solves problems. Agent capability is improving rapidly. Belayer does not own this. Belayer does not compete with agents.

**Harness** = memory and code context. AGENTS.md, CLAUDE.md, embedded docs, project-specific knowledge that makes an agent effective in a specific codebase. Today this might be a documentation map with .md files. Tomorrow models might just read the code directly. The need for a harness may drop toward zero as agents improve. Belayer has a harness plugin but harness is not belayer's core identity.

**Orchestrator** = the layer responsible for getting work done end-to-end. Three sub-concerns:

- **Intake**: How work gets from idea to agent-ready specification. Scoping, planning, design docs.
- **Implementation**: Prompting the agent to do the work. Often a single-node passthrough.
- **Output**: Everything after code is written. Review, quality gates, PR creation, deployment.

Belayer IS the orchestrator. It survives agent improvements because it doesn't compete with agents. If Claude gets better at coding, belayer benefits. If Codex gets better at review, belayer benefits. Belayer orchestrates when and how agents are invoked, not what they do.

---

## Where the Value Lives

**Intake** (high value, unsolved): Nobody in the agent space is working on "how does an idea become a well-specified task that an agent can execute." Agents assume they'll receive a good prompt. Belayer can own the before.

**Implementation** (lower value, decreasing over time): For most tasks, implementation is one prompt to one agent. The trend is toward LESS orchestration of implementation, not more. Models are learning to self-plan, self-review, self-correct. Don't force ceremony on simple tasks. The pipeline is valuable for complex multi-step work, but the single-node passthrough should be the default, not the exception.

**Output** (high value, undersolved): The space between "code is written" and "PR is approved" is poorly served. Is this code safe to merge? Does it actually solve the stated problem? Should a human look at this, or can it be auto-merged? This is where adversarial review, quality gates, and deployment confidence live. Deterministic pipeline rigidity is a FEATURE here, not a bug.

**The refined pitch**: Belayer handles what happens before and after agents code. Before: turning ideas into agent-ready work. After: verifying that work is trustworthy enough to ship. In between, use whatever agent you want.

---

## H-as-Feature: Why Context Loss Between Nodes Is Desirable

In the D > 4H framing:
- **D** = information loss from a single long context (dumb zone degradation)
- **H** = information loss per node handoff in a pipeline
- The architecture is worth it when D > (N-1)H

But for adversarial review, H is not a cost. It is a feature.

**The research**: LLM sycophancy studies show models perform better at review/critique when they DON'T share context with the implementer. Isolated reviewers catch more issues because they lack implementation bias. They evaluate the artifact on its own merits rather than validating the author's reasoning.

**What shared context does to review**: When a reviewer inherits the implementer's reasoning (even compressed), they inherit the framing (which primes what gets scrutinized), the justifications (which anchor toward agreement), and the blind spots (if the implementer didn't consider an edge case, the reviewer won't be primed to look for it).

**Belayer's position**: Node handoffs are intentionally context-free. The implementation node gets "implement this design." The validate node gets "validate the changes on this branch." No episode passing, no context sharing, no compression needed. Each node bootstraps its own understanding from the codebase. The orchestrator trusts agent competency.

**Real-world evidence**: gstack ships adversarial review with codex challenge mode. Mixed model usage (Claude planning + Codex implementing) inherently requires external handoffs between separate invocations. These work well precisely because of isolation.

**The implication**: Belayer doesn't need to solve episode compression (which Slate's own blog admits is "largely unsolved"). The architecture sidesteps the problem entirely by making isolation the default.

---

## The Orchestrator Trusts Agent Competency

The orchestrator is NOT a product manager micromanaging implementation. It moves work between phases.

"Implement this design.md" is enough. The agent gathers its own context. The agent decides how to break down the work. The agent chooses what files to read, what tests to write, what approach to take.

If the orchestrator tries to dictate implementation details, it becomes a rigid task decomposition tree (which Slate's blog correctly critiques for killing expressivity and adaptability). The orchestrator's job is to set up the right conditions for success (quality learnings injected into prompts, adversarial review after implementation), not to direct the implementation itself.

---

## The Learning Loop: Belayer's One Harness Opinion

Belayer is orchestration-only with ONE exception: quality pattern memory.

When gates fail, belayer captures WHY and prevents recurrence. This is NOT codebase knowledge. It is quality knowledge: what makes work fail review in this repo.

| Harness knowledge (NOT this) | Quality pattern knowledge (THIS) |
|---|---|
| "The auth module uses JWT" | "Auth route changes must update middleware registry, 3 PRs failed for this" |
| "We use React with TypeScript" | "New components without Storybook stories fail review 80% of the time" |
| "Deploy target is AWS" | "PRs touching infra without updating Terraform get kicked back" |

The harness tells agents how the code works. The learning loop tells the orchestrator what patterns of work succeed or fail. The agent doesn't need this knowledge (it's competent at coding). The orchestrator needs it to set up the right conditions.

Without learning loop: "Implement design.md"

With learning loop: "Implement design.md. Quality notes from past reviews in this repo: (1) auth changes require middleware registry updates, (2) new React components need Storybook stories, (3) database migrations must include rollback scripts."

This creates a flywheel no single-agent tool has: every failed gate makes future runs better. Agents are ephemeral (fresh session each time). Harnesses are manually maintained. Quality patterns are orchestration-level knowledge that belongs to belayer.

---

## Why Not Slate's Thread Model

Slate (Random Labs) proposes "threads" as a primitive that supersedes subagents. Threads share context via compressed "episodes" that compose across worker threads back to an orchestrator.

**Binary analysis revealed**: Slate has a `passContext` boolean toggle that switches between shared context (thread mode) and isolated context (subagent mode). The implementation hedges on its own thesis. The UI still calls everything "subagents."

**Where threads work**: Implementation tasks where the orchestrator needs to adapt to discoveries mid-execution. Research tasks where context flows between exploration steps.

**Where threads fail**: Adversarial review. Any task where independent judgment is the point. The orchestrator itself accumulates implementation bias through episodes, coloring how it dispatches review work even if episodes are withheld from the reviewer.

**Belayer's position**: Context isolation is not a limitation to overcome. It is sometimes the mechanism of quality. A complete architecture needs both primitives (shared context for implementation, isolated context for review) and deterministic rules about when to use which. Belayer's YAML pipeline makes this static and enforceable. Slate's model makes it dynamic and emergent (which means the model can get it wrong).

---

## Concurrent Branch Safety

Learnings are repo-level knowledge but gate results happen on branches. With multiple pipelines running simultaneously, mutable counters on learning files create race conditions.

**Solution**: Append-only signal files. The learning file is immutable after creation. Each gate result writes its own signal file. Status (active, dormant, stale) is computed at read time from signal history, never stored. No write conflicts, no stale counters, richer data for free.

---

## What Belayer Is Not

- Not an agent (doesn't read/write code)
- Not a harness (doesn't own codebase knowledge, except quality patterns)
- Not competing with Claude Code, Codex, or Slate (sits above them)
- Not a task decomposition tree (doesn't dictate how agents work)
- Not an episode compression system (intentionally avoids the problem)

## What Belayer Is

- The orchestration layer between idea and shipped code
- A pipeline runner that enforces context boundaries deterministically
- A quality pattern memory that makes every agent smarter about THIS repo over time
- Agent-agnostic: swap Claude for Codex for Gemini per-node
- The substrate for agentic work in a repo
