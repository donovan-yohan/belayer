# Talent Catalog Examples

This directory is the planned local-first catalog surface for organization mode.
Categories are intentionally separate so software-development identities do not
pollute story-world prompts, and story identities do not leak into development
runs.

The first implementation should install a category into `.belayer/agents/` by
copying identity directories. Until that installer exists, these README files
define the category boundaries only.

## Categories

- `development/` - software-company talents and gate presets
- `story/` - interactive-world talents and continuity gates

Each talent directory should eventually contain the standard Belayer identity
files plus optional `talent.yaml` metadata:

```text
<category>/<talent>/
├── agent.yaml
├── system-prompt.md
├── agents.md
└── talent.yaml
```
