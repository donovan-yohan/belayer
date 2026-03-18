# Unified Agent Plugin Source Design

## Problem or Goal

Belayer currently treats the Claude Code marketplace tree as the source of truth for the `harness` and `pr` plugins. `belayer init` only installs Claude-facing assets by writing registry entries under `~/.claude/plugins/`. Codex is different: it uses native skill discovery from `~/.agents/skills/`, and Belayer does not currently install any Codex-facing harness or PR workflows there.

That leaves Belayer in an awkward state:

- Claude gets first-party plugin install support; Codex does not.
- Workflow content is authored in a Claude-shaped format (`/harness:*`, `/pr:*`), even though Belayer also runs Codex leads.
- Adding Codex support by hand would create a second copy of the same workflows and docs.
- There is no clear path to support future runtimes without repeating the same fork-per-agent pattern.

The goal is to move Belayer to a single authored source for harness/PR workflows, generate agent-specific outputs from it, have `belayer init` install Codex support when Codex is already available locally, and document the resulting behavior clearly.

## Constraints

- Belayer may be installed via `go install`, so `belayer init` cannot assume the user has a local checkout of this repo.
- Claude Code wants marketplace metadata plus plugin-shaped assets under `.claude-plugin/` and `plugins/`.
- Codex wants native `SKILL.md` directories discoverable under `~/.agents/skills/`.
- Existing Claude ergonomics should remain intact: users and Belayer prompts already rely on `/harness:*` and `/pr:*`.
- Codex integration should use native discovery rather than a custom bootstrap layer.
- Asset installation needs to be idempotent and safe to re-run.
- The repo should leave room for future targets such as Cursor or OpenCode without re-authoring the workflows again.

## Considered Approaches

### 1. Keep Claude as Canonical and Hand-Maintain Codex Copies

Add a `.codex/skills/` tree and copy each harness/PR workflow into Codex-native skills by hand.

**Pros**
- Lowest short-term implementation effort
- Minimal disruption to the current Claude marketplace setup

**Cons**
- Guaranteed drift between Claude and Codex content
- Claude-specific references stay baked into the source
- Every future runtime multiplies maintenance cost again

### 2. Keep the Current Claude Commands as Source and Script One-Way Codex Wrappers

Continue authoring under `plugins/*/commands/*.md`, then generate Codex wrappers from those files.

**Pros**
- Less migration than a full content model change
- Preserves current file locations for authors in the short term

**Cons**
- The source remains Claude-biased
- Command bodies contain runtime-specific references (`/harness:plan`, `/pr:author`) that do not map cleanly to Codex
- It solves Codex once, but does not create a good foundation for other runtimes

### 3. Introduce a Canonical Cross-Agent Source and Generate Runtime Outputs

Author harness/PR workflows once in a runtime-neutral source tree, then generate Claude marketplace assets and Codex skills from that source.

**Pros**
- True single source of truth
- Codex becomes a first-class target instead of a copy
- Future runtimes can add an adapter without rewriting the workflows
- Versioning, install logic, and docs can all read from the same manifest

**Cons**
- Requires an explicit generation/build step
- Requires migration away from the current Claude-first authoring layout

## Chosen Approach

Adopt approach 3.

Belayer should follow the same broad pattern that works in `superpowers`: keep workflow content canonical in one place, keep runtime-specific manifests and install docs thin, and let each agent consume a generated shape that matches its own discovery model.

The preferred structure is:

```text
agentpacks/
  manifest.yaml
  harness/
    pack.yaml
    entries/
      brainstorm/
        entry.md
      bug/
        entry.md
      plan/
        entry.md
      ...
    references/
      strangler-fig/
        SKILL.md
        references/
  pr/
    pack.yaml
    entries/
      author/
        entry.md
      review/
        entry.md
      ...

.claude-plugin/              # generated, committed
plugins/                     # generated, committed
.codex/INSTALL.md            # generated or templated
```

### Canonical Authoring Model

Each canonical workflow entry should hold:

- stable workflow ID, for example `harness.brainstorm` or `pr.author`
- human-facing title
- trigger/description text
- shared instruction body
- runtime alias metadata
- cross-workflow references

The source content should not hardcode runtime-facing invocation strings inside the body. Instead, it should use lightweight tokens for workflow references, for example:

- `{{ref:harness.plan}}`
- `{{ref:harness.orchestrate}}`
- `{{ref:pr.author}}`

The generator resolves these differently per target:

- **Claude**: `/harness:plan`, `/harness:orchestrate`, `/pr:author`
- **Codex**: `harness-plan`, `harness-orchestrate`, `pr-author`

This keeps the authored content neutral while preserving each runtime's native UX.

### Generated Claude Output

Claude output remains at the current published paths so the repo still works as a marketplace:

- `.claude-plugin/marketplace.json`
- `plugins/harness/.claude-plugin/plugin.json`
- `plugins/pr/.claude-plugin/plugin.json`
- `plugins/harness/commands/*.md`
- `plugins/pr/commands/*.md`
- `plugins/harness/skills/*` for any Claude-visible skills the commands invoke

The important change is authorship: these files become generated output, not the place humans edit directly.

Claude commands should be thin wrappers around the canonical workflows. That preserves slash-command ergonomics without making slash commands the source format.

### Generated Codex Output

Codex output should be a native skill pack generated from the same source. The generated installable tree should contain `SKILL.md` directories for:

- `harness-init`
- `harness-brainstorm`
- `harness-bug`
- `harness-refactor`
- `harness-refactor-status`
- `harness-plan`
- `harness-orchestrate`
- `harness-review`
- `harness-reflect`
- `harness-prune`
- `harness-complete`
- `harness-loop`
- `harness-batch`
- `pr-author`
- `pr-review`
- `pr-resolve`
- `pr-update`
- `pr-automate`

The Codex pack should be discoverable through a single stable link:

```text
~/.agents/skills/belayer -> ~/.belayer/agent-assets/codex/<version>/skills
```

This mirrors the superpowers pattern of using native skill discovery plus a single mounted skill tree, but adapts it to Belayer's binary-install model by materializing the source under `~/.belayer/agent-assets/` rather than assuming a git checkout exists.

## Key Decisions

### 1. Runtime-Neutral Source Lives Outside `plugins/`

`plugins/` stays as generated Claude output because that path matters to the marketplace. The canonical source moves elsewhere (`agentpacks/`) so contributors have an obvious place to edit.

### 2. `belayer init` Installs Codex from Embedded Generated Assets

Belayer should not require a repo clone just to install Codex support. The generated Codex skill pack should be embedded into the Go binary, then written out by `belayer init`.

That keeps install behavior aligned with the rest of Belayer's defaults system.

### 3. Codex Installation Is Opportunistic, Not Mandatory

`belayer init` should:

1. continue installing Claude marketplace data as it does today
2. detect Codex with `exec.LookPath("codex")`
3. if Codex is present, install/update the generated Codex skill pack
4. if Codex is absent, skip Codex install and print a short note explaining that re-running `belayer init` after installing Codex will wire it up

This matches the user's requirement: install for Codex if Codex is already installed locally.

### 4. Use a Stable Mount Point with Versioned Asset Directories

Write generated Codex assets to a versioned directory such as:

```text
~/.belayer/agent-assets/codex/2.3.0/skills/
```

Then repoint the stable discovery location:

```text
~/.agents/skills/belayer
```

This gives atomic-ish upgrades, easy rollback, and straightforward idempotency checks.

### 5. Workflow References Must Be Rendered Per Provider

Belayer's own prompts and docs must stop assuming Claude syntax for all providers.

That means:

- Claude lead/setter prompts keep `/harness:*` and `/pr:*`
- Codex lead/setter prompts should reference `harness-*` and `pr-*`
- plan/design templates that currently say `For Claude:` need a provider-aware companion line or generation token

Without this, Codex installation would still leave Belayer instructing Codex to use commands it does not have.

### 6. Versions Come from the Canonical Pack Manifest

The current hardcoded plugin version constants in `internal/plugins/registry.go` should be replaced by metadata generated from the canonical pack manifest. Claude registry writes, Codex asset install, and README snippets should all read from the same version source.

## README and Install Guidance

The repo should document two levels of installation guidance.

### Root README

The main README should say:

- `belayer init` always installs Belayer config
- it registers the Claude marketplace/plugins when Claude is installed
- it installs the Belayer Codex skill pack when `codex` is available locally
- users must restart Claude/Codex after first install so the new assets are discovered
- manual repair instructions live in `.codex/INSTALL.md` for Codex

### Codex-Specific Install Doc

Add a repo-tracked `.codex/INSTALL.md` following the same shape as the superpowers doc:

- quick install via `belayer init`
- manual reinstall/repair path
- where the Belayer Codex skill tree lives
- how Codex discovers it (`~/.agents/skills/belayer`)
- restart requirement
- troubleshooting for broken symlink/junction or stale skill tree

For contributors working from a checkout, the doc can also include a dev mode that symlinks the locally generated Codex output instead of the installed asset cache.

## Tooling and Verification

Add a small generator tool in Go so the repo stays self-hosted:

- `go run ./cmd/agentpack build` or similar to render runtime outputs
- `go run ./cmd/agentpack check` or a test helper to fail if generated files are stale

Verification should cover:

- every canonical entry has the required metadata
- all cross-reference tokens resolve for each target
- generated Claude manifests and command files are deterministic
- generated Codex skill names and descriptions are deterministic
- `belayer init` installs Codex assets correctly when `codex` is present
- `belayer init` skips Codex cleanly when `codex` is absent

Generated files should carry a short header comment saying they are generated and pointing back to `agentpacks/`.

## Open Questions

- Should Claude commands invoke generated Claude plugin skills, or should they render the full body directly from the canonical source? Thin wrappers are preferred, but either works if generation stays deterministic.
- Should Belayer add a dedicated repair command such as `belayer plugins install codex`, or should `belayer init` remain the only entry point at first?
- How much of the existing plan/design template language should become provider-tokenized immediately versus in a second pass?
- Do we want to add a second target right away (for example OpenCode) once the generator exists, or keep the first implementation scoped to Claude + Codex?

## Implementation Handoff Notes

1. Start by defining the canonical pack manifest and one migrated workflow (`harness-brainstorm`) to prove the rendering model.
2. Add generator output for Claude and Codex, commit generated files, and add a drift check.
3. Replace hardcoded plugin version constants with generated metadata.
4. Extend `belayer init` so Codex install writes the embedded skill pack to `~/.belayer/agent-assets/codex/<version>/skills` and mounts `~/.agents/skills/belayer`.
5. Update Belayer's provider-specific prompts so Codex no longer gets Claude slash-command instructions.
6. Update the README and add `.codex/INSTALL.md`.

Normal next step: run `harness-plan` to turn this design into an execution plan.
