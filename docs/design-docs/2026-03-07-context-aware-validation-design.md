# Context-Aware Validation Pipeline

**Date:** 2026-03-07
**Status:** Approved

## Problem

Leads produce code that builds but doesn't actually work. In testing with the VCT Fantasy League, leads scaffolded a full TanStack Start app that compiled successfully, but the deployed site had broken text wrapping, unresolved Tailwind classes, and non-functional buttons. The current spotter only reviews git diffs for cross-repo alignment — it never runs the project or verifies runtime behavior.

## Solution

Three-layer validation with LLM-driven project detection and a config system for customization.

## Agent Hierarchy

| Agent | Scope | When | What it checks |
|-------|-------|------|----------------|
| **Lead** | Single goal | During execution | Build passes, tests pass (self-check) |
| **Spotter** | Single goal | After lead writes DONE.json | Runtime verification — dev server, browser rendering, console errors, endpoint responses |
| **Anchor** | All goals | After all goals pass spotting | Cross-repo alignment — API contracts, shared types, integration compatibility |

The spotter is the new agent. The anchor is the current spotter, renamed. "Spotter" fits the validator role better (climbing metaphor: the person who watches you and catches problems). "Anchor" fits the cross-repo role (connects all independent lines together).

## Lifecycle

```
Task Created (pending)
    |
    v
Setter picks up task, builds DAG
    |
    v
Lead spawned per goal (parallel, respects deps)
    |-- Reads lead.md prompt template
    |-- Implements the goal
    |-- Self-check: runs build + tests
    |-- Commits, pushes, writes DONE.json
    |
    v
Setter detects DONE.json
    |
    v
Spotter spawned in same tmux window (fresh context)
    |-- Reads spotter.md prompt template
    |-- Looks at the repo, determines project type
    |-- Reads matching profile checklist (frontend.toml, etc.)
    |-- Executes checks (build, dev server, browser via Chrome DevTools MCP, etc.)
    |-- Writes SPOT.json: pass or fail with issues
    |
    v
Pass?
  |         |
 YES       NO --> Goal goes back to lead with spotter feedback
  |               Lead retries (up to max_retries)
  |
  v
All goals spotted and passing?
    |
    v
Anchor spawned (once, cross-repo)
    |-- Reads anchor.md prompt template
    |-- Reviews all repo diffs together
    |-- Checks cross-repo alignment
    |-- Writes VERDICT.json: approve or reject with correction goals
    |
    v
Approve?
  |         |
 YES       NO --> Correction goals dispatched as new leads
  |               Back to lead -> spotter cycle
  |
  v
Create PRs, task complete
```

## Config System

### Directory Structure

```
~/.belayer/
  config/                          # Global defaults (written by `belayer init`)
    belayer.toml                   # Agent provider, concurrency, timeouts
    prompts/
      lead.md                      # Lead execution prompt template
      spotter.md                   # Spotter validation prompt template
      anchor.md                    # Anchor cross-repo review prompt template
    profiles/
      frontend.toml                # Frontend validation checklist
      backend.toml                 # Backend API validation checklist
      cli.toml                     # CLI tool validation checklist
      library.toml                 # Library/package validation checklist

~/.belayer/instances/<name>/
  config/                          # Per-instance overrides (optional)
    belayer.toml                   # Overrides global settings
    prompts/                       # Overrides global prompts
    profiles/                      # Overrides global profiles
```

**Resolution order:** instance config > global config > embedded defaults.

Defaults are embedded in the Go binary (like SQL migrations today). `belayer init` writes them to disk so users can edit. Instances inherit global config but can override anything.

### belayer.toml

```toml
[agents]
provider = "claude"                # "claude" | "codex"
lead_model = "opus"                # model for lead execution
review_model = "sonnet"            # model for spotter + anchor (cheaper)
permissions = "dangerously-skip"   # "dangerously-skip" | "interactive"

[execution]
max_leads = 8                      # max concurrent lead sessions
poll_interval = "5s"               # setter polling interval
stale_timeout = "30m"              # goal stale detection threshold
max_retries = 3                    # max attempts per goal before stuck

[validation]
enabled = true                     # enable spotter validation
auto_detect_project = true         # LLM determines project type
fallback_profile = "library"       # profile if LLM can't determine type
browser_tool = "chrome-devtools"   # "chrome-devtools" | "playwright" | "none"

[anchor]
enabled = true                     # enable cross-repo anchor review
max_attempts = 2                   # max anchor review rounds before stuck
```

### Validation Profiles

Profiles are **human-readable checklists** that the spotter LLM interprets, not rigid automation. The LLM determines the project type by examining the repo, then reads the matching profile as a guide. It can adapt checks based on the actual project setup.

**frontend.toml:**
```toml
# Validation checklist for frontend/web projects

[checks]
build = "Run the project's build command and verify it succeeds with exit code 0"
dev_server = "Start the dev server and verify it's reachable on localhost"
browser = "Navigate to the running app with Chrome DevTools and verify the page renders"
console_errors = "Check the browser console for errors or warnings"
screenshot = "Take a screenshot of the main page and include in the report"
click_navigation = "Click primary navigation links and verify they load without errors"
visual_quality = "Verify text wraps properly, layout isn't broken, styles are applied correctly"
```

**backend.toml:**
```toml
# Validation checklist for backend/API projects

[checks]
build = "Run the project's build command and verify it succeeds"
test_suite = "Run the full test suite and verify all tests pass"
start_server = "Start the server and verify it binds to a port"
smoke_endpoints = "Hit health and root endpoints, verify expected status codes"
```

**cli.toml:**
```toml
# Validation checklist for CLI tools

[checks]
build = "Build the binary and verify it succeeds"
test_suite = "Run the full test suite and verify all tests pass"
help_check = "Run the binary with --help and verify it exits with code 0"
happy_path = "Exercise 1-2 basic commands with simple inputs and verify output"
```

**library.toml:**
```toml
# Validation checklist for libraries and packages (fallback)

[checks]
build = "Run the project's build command and verify it succeeds"
test_suite = "Run the full test suite and verify all tests pass"
typecheck = "Run the type checker if available and verify no errors"
```

## Key Decisions

- **LLM detects project type, not code** — Rigid signal matching is worse than an LLM that can look at the repo and understand what it is. Profiles are guides, not rules.
- **Spotter reuses lead's tmux window** — Simpler than creating a new window. Fresh `claude -p` session ensures clean context.
- **Spotter feedback flows to lead on retry** — Instead of re-running blind, the lead sees what the spotter found wrong (issues from SPOT.json injected into prompt).
- **Chrome DevTools MCP for browser checks** — Already in our toolchain, doesn't require installing dependencies into the target project.
- **Prompt templates in editable .md files** — Moved out of Go source code. Same Go template variables, but users can customize without recompiling.

## New Signals

| File | Written by | Contents |
|------|-----------|----------|
| `DONE.json` | Lead | `{ "status": "complete"/"failed", "summary": "...", "files_changed": [...] }` |
| `SPOT.json` | Spotter | `{ "pass": true/false, "project_type": "frontend", "issues": [...], "screenshots": [...] }` |
| `VERDICT.json` | Anchor | `{ "verdict": "approve"/"reject", "repos": { ... } }` |

## Backwards Compatibility

- `validation.enabled = false` skips the spotter entirely — leads go straight to anchor review (old behavior)
- `anchor.enabled = false` skips anchor review — leads go straight to PR creation
- Existing prompt templates in Go code become the embedded defaults
