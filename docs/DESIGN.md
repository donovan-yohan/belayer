# Design

Status: `active redesign` — Nightshift v1 / Extend-first direction (2026-04-15)

This file summarizes the practical design direction now that Belayer is being reshaped around Nightshift.

## What changed

Belayer started as a more generic session runtime for autonomous coding agents.

The current direction is narrower and more concrete:

- Extend-first before general abstraction
- one worker, one request for v1
- Belayer as the **run-local agent control plane**
- Hermes as the default harness
- tmux as the default transport adapter
- artifacts/events/messages as the coordination substrate

This is a deliberate scope correction, not a retreat.

---

## Qualities we now care most about

- **Run-local clarity**: Belayer should clearly own a single Nightshift run, not the whole worker fleet
- **Observable coordination**: messages, events, roster state, and artifacts must make progress inspectable
- **Low-ceremony execution**: simple run primitives (`run start`, `spawn`, `message`, `finish`, `artifact`) beat workflow DSLs
- **Harness ownership**: Hermes is attractive because we can own profiles, skills, plugins, and behavior over time
- **Transport pragmatism**: tmux is good enough for v1 because it is simple, inspectable, and harness-agnostic
- **Explicit completion**: agents must call `belayer finish` or be marked blocked on exit

---

## Current key patterns

### Two control planes

- **Worker control plane** — outer Nightshift layer that assigns a worker
- **Agent control plane** — Belayer inside the worker run

Belayer only owns the second one.

### Three Belayer layers

1. **Session bus / control plane**
2. **Harness driver**
3. **Transport adapter**

For v1 these map to:

- session bus → Belayer daemon + store + broker
- harness driver → Hermes launcher/profile binding
- transport adapter → tmux

### Communication split

- **messages** for direct coordination
- **events** for orchestration state
- **artifacts** for durable shared outputs

### Exit discipline

Hermes runs are launched with Belayer env vars and a project-local plugin/skill.

If an agent explicitly calls `belayer finish`, a finish marker is written.
If the harness exits without that marker, Belayer marks the run `blocked`.

This is now one of the most important reliability patterns in the stack.

---

## Current implemented behaviors

Implemented today:

- run-local session creation
- planner spawn
- api/planner message delivery through Belayer
- run roster tracking
- explicit `finish` / `blocked`
- artifact registration and listing
- Hermes project-local plugin + Belayer communication skill
- exit-without-finish detection

These are the real foundations of Nightshift v1.

---

## Planned behaviors

Still planned / not complete yet:

- idle nudge for agents that stay alive but do not call `belayer finish`
- richer planner profile / specialist profiles
- stronger artifact conventions and artifact viewing
- Extend-localenv-first workbench flows
- outer worker control-plane integration
- canonical identity materialization beyond local profiles

---

## Practical design rule

Belayer should not try to hide the fact that agent harnesses are real terminal programs.

Instead:

- keep the coordination surface explicit
- keep the transport simple
- make the system debuggable by humans
- teach the agents the correct coordination protocol through profiles/skills/plugins

This is why `belayer` CLI + Hermes skill/plugin remains the right v1 approach.
