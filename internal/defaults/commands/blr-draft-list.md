---
description: List draft problems with phase, order, and dependency hints
allowed-tools: ["Bash", "Read", "Glob", "Grep"]
---

Use this command to inspect the current draft queue before review or publication.

## Resolve the draft root

Prefer the draft root explicitly named in the session context:

- In setter sessions, `CLAUDE.md` defines the current draft root.
- In explorer sessions, `CLAUDE.md` defines the explorer workspace and project naming context.

If the session does not explicitly name the draft root, detect the draft namespace in this order:

1. If `BELAYER_CRAG` is set, use that as the namespace.
2. If the current workspace lives under `~/.belayer/explorer/<name>/`, use that workspace directory name.
3. Otherwise use the strongest stable project or crag name already present in the session.

Use this root:

- `~/.belayer/drafts/<draft-namespace>/problems`

If the namespace is still empty or unresolved, explain that the draft queue cannot be listed until the project or crag name is known.

## Workflow

1. Read every draft directory under the drafts root, sorted by draft ID.
2. For each draft, read the `spec.md` frontmatter and the document title or first heading.
3. Present at least:
   - `draft_id`
   - title
   - `phase`
   - `order`
   - `depends_on`
4. For each dependency in `depends_on`:
   - show it as-is when that draft directory still exists
   - render it as `<id> (published)` when the referenced draft directory no longer exists
5. Missing dependency directories are not automatically an error. They usually mean the dependency was already published and its draft directory was deleted.
6. If no drafts exist yet, say so clearly and point the user to `/blr-draft-create`.

## Output

Render the queue as a readable table or list and state exactly which drafts root was inspected.

## Next Steps (say this exactly)

> Next Steps:
>
> 1. `/blr-draft-review <draft-id>` — open a draft, edit it, and publish when ready
> 2. `/blr-draft-create` — add another draft to the queue
> 3. `/blr-phase-plan` — refresh `phases.md` if the sequence or dependencies need to change
