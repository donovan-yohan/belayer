# Philosophy

Foundational thinking behind belayer's architecture. This document captures the WHY, not the HOW. Reference this when brainstorming new features, evaluating competing approaches, or explaining belayer's position in the agentic coding space.

Status: `active` — last updated 2026-03-30
Source: Slate binary analysis session + adversarial review debate + carabiner separation + contracts refinement

---

## The Three Roles

Agent, Harness, and Orchestrator are separate concerns. Belayer is the Orchestrator.

**Agent** = raw model capability. Claude, Codex, Slate, Gemini, whatever comes next. The agent reads code, writes code, runs commands, solves problems. Agent capability is improving rapidly. Belayer does not own this. Belayer does not compete with agents.

**Harness** = memory and code context. AGENTS.md, CLAUDE.md, quality patterns, domain knowledge that makes an agent effective in a specific codebase. This is **carabiner** (separate repo: github.com/donovan-yohan/carabiner). Belayer does not own the harness. Carabiner does not own orchestration. Frameworks compose both.

**Orchestrator** = the layer responsible for getting work done end-to-end. Three concerns connected by two contracts:

- **Intake**: How work gets from idea to agent-ready specification. Scoping, planning, design docs. Any tool can do intake (gstack /office-hours, a Jira board, a human writing a spec).
- **Implementation**: The full build loop from design to ready-to-ship. Code, tests, review, fix, PR, resolve comments. Gates live INSIDE implementation as self-resolving loops. The agent owns the entire journey from "here's a design" to "this is ready to ship."
- **Output**: Shipping and verifying. Is the risk level acceptable? Merge. Watch CI. Run canary. Monitor production. Roll back if something breaks. Output is the ops concern, not the dev concern.

Belayer IS the orchestrator. It survives agent improvements because it doesn't compete with agents. If Claude gets better at coding, belayer benefits. If Codex gets better at review, belayer benefits. Belayer orchestrates when and how agents are invoked, not what they do.

---

## Two Contracts

The three phases connect through two contracts. These are belayer's only opinions about workflow. Everything else is framework-specific.

**Trigger contract** (Intake → Implementation): "Here's an artifact. Is it ready to build?"

A framework-defined script receives an artifact path, validates it meets readiness criteria, and returns exit 0 (ready) or exit 1 (not ready). What "ready" means is the framework's decision. For gstack, it might mean an APPROVED design doc. For a trunk-based workflow, it might mean a ticket moved to "Ready." For a solo dev, it might mean `belayer submit --spec design.md`.

**Ready-to-ship contract** (Implementation → Output): "Implementation is done. Here's what to ship."

Implementation declares ready-to-ship when its internal loops (code, review, fix, resolve) are complete. What "ready-to-ship" means is the framework's decision. For gstack, it might mean a PR that passed review. For trunk-based, it might mean tests pass on main. For a package, it might mean a build artifact exists.

Both contracts are the same shape: a shell script that receives structured input, returns a structured result. Belayer doesn't care what runs inside. The framework defines that.

**What belayer does NOT opine on:**
- What tool does intake (gstack, Jira, a human)
- What agent implements (Claude, Codex, Slate)
- What review tool gates quality (codex challenge, gstack /review, a custom linter)
- What "ship" means (PR, trunk push, package publish, container deploy)

**What belayer DOES opine on:**
- Nodes execute commands and produce outcomes (PASS/RETRY/FAIL)
- Gates produce numeric scores with rationale, routed by YAML thresholds
- The trigger contract connects intake to implementation
- The ready-to-ship contract connects implementation to output

---

## Gates Are Implementation Primitives

Gates are NOT phase boundaries. They live inside implementation as self-resolving loops.

```
Implementation:
  implement → review (gate) → FAIL → implement (retry) → review → PASS → ship
```

The agent writes code, the gate catches issues, the agent fixes them, the gate passes. All within implementation. By the time work reaches output, the code quality question is settled. Output's question is operational: "can we safely ship this?"

This means output doesn't need adversarial code review. It needs risk assessment, CI verification, canary monitoring. Different concern, different tools.

---

## Where the Value Lives

**Intake** (high value, unsolved): Nobody in the agent space is working on "how does an idea become a well-specified task that an agent can execute." Agents assume they'll receive a good prompt. Belayer can own the before.

**Implementation** (lower value, decreasing over time): For most tasks, implementation is one prompt to one agent. The trend is toward LESS orchestration of implementation, not more. Models are learning to self-plan, self-review, self-correct. Don't force ceremony on simple tasks. The pipeline is valuable for complex multi-step work, but the single-node passthrough should be the default, not the exception.

**Output** (high value, undersolved): The space between "ready to ship" and "safely running in production" is poorly served. Should this auto-merge or wait for a human? Is CI green? Did the deploy succeed? Is the canary healthy? This is the ops concern. Deterministic pipeline rigidity is a FEATURE here.

**The refined pitch**: Belayer handles the transitions. The trigger contract gets work from intake to implementation. The ready-to-ship contract gets work from implementation to output. The phases themselves use whatever tools the framework wires up.

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

## The Learning Loop Lives in the Harness, Not the Orchestrator

Quality pattern memory (the flywheel where every failed gate makes future runs better) is a harness concern, not an orchestration concern. It belongs to **carabiner**, not belayer.

Belayer's gate nodes produce structured failure output (gate-result.json + rationale.md). What happens with that output is the framework's decision. A framework that uses carabiner calls `carabiner quality record` on gate failure and `carabiner quality check` before implementation. A framework without carabiner just retries.

This separation matters because quality patterns are repo knowledge ("auth routes need middleware registry updates"), and belayer explicitly does not own repo knowledge. The orchestrator just knows: something failed, route to retry. The harness knows WHY it failed and prevents recurrence.

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
- Not a harness (doesn't own codebase knowledge — that's carabiner)
- Not competing with Claude Code, Codex, or Slate (sits above them)
- Not a task decomposition tree (doesn't dictate how agents work)
- Not an episode compression system (intentionally avoids the problem)
- Not prescriptive about what "ready" or "ship" means (frameworks define that)

## What Belayer Is

- The orchestration layer between idea and shipped code
- Two contracts (trigger + ready-to-ship) connecting three phases
- A pipeline runner that enforces context boundaries deterministically
- Agent-agnostic: swap Claude for Codex for Gemini per-node
- The substrate for agentic work in a repo
