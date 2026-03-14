# Belayer Marketplace Design

## Problem

Belayer's lead execution loop hard-depends on the harness plugin (`/harness:init` → `/harness:plan` → `/harness:orchestrate` → `/harness:complete`) and uses the pr plugin for PR lifecycle management. Both currently live in the `llm-agents` work repo — a separate codebase that belayer has no control over. They should ship alongside belayer as first-party plugins.

## Decision

Fork and vendor harness and pr into the belayer repo from the current local branch of `work/llm-agents` (which has the latest updated harness commands in active use). Belayer becomes the canonical source for both plugins going forward. Changes flow from belayer back to llm-agents, not the other way around.

## Marketplace Structure

The belayer repo becomes a Claude Code marketplace. A `.claude-plugin/marketplace.json` at the repo root registers the available plugins, and plugin source lives under `plugins/`.

```
belayer/
├── .claude-plugin/
│   └── marketplace.json
├── plugins/
│   ├── harness/
│   │   ├── .claude-plugin/
│   │   │   └── plugin.json
│   │   ├── agents/
│   │   │   └── harness-pruner.md
│   │   ├── commands/
│   │   │   ├── init.md
│   │   │   ├── brainstorm.md
│   │   │   ├── bug.md
│   │   │   ├── refactor.md
│   │   │   ├── refactor-status.md
│   │   │   ├── plan.md
│   │   │   ├── orchestrate.md
│   │   │   ├── review.md
│   │   │   ├── complete.md
│   │   │   ├── reflect.md
│   │   │   ├── prune.md
│   │   │   └── loop.md
│   │   └── skills/
│   │       └── strangler-fig/
│   │           ├── SKILL.md
│   │           └── references/
│   └── pr/
│       ├── .claude-plugin/
│       │   └── plugin.json
│       └── commands/
│           ├── author.md
│           ├── automate.md
│           ├── review.md
│           ├── resolve.md
│           └── update.md
```

### marketplace.json

```json
{
  "$schema": "https://anthropic.com/claude-code/marketplace.schema.json",
  "name": "belayer",
  "owner": { "name": "donovanyohan" },
  "metadata": {
    "description": "Belayer marketplace — plugins shipped with the multi-repo coding agent orchestrator",
    "version": "1.0.0"
  },
  "plugins": [
    {
      "name": "harness",
      "source": "./plugins/harness",
      "description": "3-tier documentation system with living execution plans"
    },
    {
      "name": "pr",
      "source": "./plugins/pr",
      "description": "PR lifecycle management — author, review, resolve, update"
    }
  ]
}
```

## Auto-Install During `belayer init`

After creating global config, `belayer init` registers the belayer GitHub repo as a marketplace and installs both plugins into the user's Claude Code plugin system.

### Marketplace Registration

Writes to `~/.claude/plugins/known_marketplaces.json`:

```json
{
  "belayer": {
    "source": {
      "source": "github",
      "repo": "donovanyohan/belayer"
    },
    "installLocation": "~/.claude/plugins/marketplaces/belayer",
    "lastUpdated": "<timestamp>",
    "autoUpdate": true
  }
}
```

### Plugin Installation

Writes to `~/.claude/plugins/installed_plugins.json`:

```json
{
  "harness@belayer": [{
    "scope": "user",
    "installPath": "~/.claude/plugins/cache/belayer/harness/2.1.0",
    "version": "2.1.0",
    "installedAt": "<timestamp>",
    "lastUpdated": "<timestamp>"
  }],
  "pr@belayer": [{
    "scope": "user",
    "installPath": "~/.claude/plugins/cache/belayer/pr/1.2.0",
    "version": "1.2.0",
    "installedAt": "<timestamp>",
    "lastUpdated": "<timestamp>"
  }]
}
```

### How It Works

`belayer init` only writes JSON entries to the two registry files — it does not clone or cache anything itself. Claude Code lazily handles the GitHub clone on its next launch or marketplace update cycle. The flow:

1. `belayer init` writes marketplace entry to `known_marketplaces.json` and plugin entries to `installed_plugins.json`
2. On next Claude Code session start, Claude Code sees the new marketplace, clones `donovanyohan/belayer` into `~/.claude/plugins/marketplaces/belayer/`, and caches plugins under `~/.claude/plugins/cache/belayer/`
3. Subsequent updates happen via Claude Code's normal auto-update (git pull)

**Note:** The registry file formats are based on observed structure of existing Claude Code installations. If the format changes upstream, this logic will need to be updated.

### Idempotency

If `belayer init` is run again and the marketplace/plugins are already registered, skip writing those entries. Check by key (`"belayer"` in marketplaces, `"harness@belayer"` / `"pr@belayer"` in plugins).

### Versioning

The version string in each plugin's `plugin.json` is the source of truth. When plugin content changes in the belayer repo, bump the version in `plugin.json`. The `installed_plugins.json` entries written by `belayer init` use the version read from `plugin.json` at build time (embedded via Go `embed.FS`).

## Scope

**In scope:**
- Create marketplace structure (`.claude-plugin/marketplace.json`, `plugins/` directory)
- Vendor harness plugin — copy from current local branch of llm-agents, update author
- Vendor pr plugin — copy from current local branch of llm-agents, update author
- `belayer init` auto-install — register GitHub marketplace, install both plugins

**Not in scope:**
- Migration from existing llm-agents installs (assumes clean install)
- Changes to lead execution loop (already uses `/harness:*` commands)
- Plugin content modification — forked content is identical at time of copy

## Testing

- Unit test for marketplace registration logic in `belayer init`
- Manual verification: run `belayer init`, confirm plugins appear in Claude Code
