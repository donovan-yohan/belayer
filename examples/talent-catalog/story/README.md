# Story Talent Catalog

Story talents model a living world for interactive storytelling. The catalog is
separate from `../development/` so story prompts do not affect software-delivery
runs.

Expected first identities:

- `storyteller` - lead talent; frames scenes and manages the E2R loop
- `lorekeeper` - maintains durable world state
- `continuity-editor` - gate talent for canon, timeline, character, and tone
- `protagonist` - example character talent
- `antagonist` - example character talent

The first proof should produce `world-state` and `continuity-report` artifacts
using the same gate contract as development runs.
