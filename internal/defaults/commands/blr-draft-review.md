---
description: Review a draft problem, iterate on it, and publish it via belayer problem create
argument-hint: "[draft-id]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep"]
---

Use this command to display a draft in the conversation, apply iterative edits, and publish it when the user approves.

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

If the namespace is still empty or unresolved, explain that the draft can be edited but not published until the project or crag name is known.

## Workflow

1. Select the target draft:
   - if the user named a draft ID, use it
   - if there is only one draft, use it automatically
   - otherwise show the available drafts and ask the user which one to review
2. Read both `spec.md` and `climbs.json`.
3. Display the draft metadata, spec summary, and climb breakdown in the conversation so the user can review the actual content.
4. When the user asks for changes, update the files in place and briefly restate the revised draft after each edit.
5. Before publishing, surface `depends_on` as an ordering hint only. If a dependency draft directory is missing, show it as `<id> (published)` and keep going.
6. Publish with `belayer problem create --spec <draft-dir>/spec.md --climbs <draft-dir>/climbs.json`.
7. Add `--crag <draft-namespace>` only when `BELAYER_CRAG` is not already set and the target crag can be resolved confidently.
8. If no crag can be resolved yet, stop cleanly and explain that the draft is ready but publication requires a crag first.
9. Delete the draft directory only after a successful publish. If publishing fails, leave the draft directory intact.
10. Report the created problem ID and the removed draft path on success.

## Output

Summarize the review result, including whether the draft was edited, published, or left in the queue.

## Next Steps (say this exactly, substituting the real problem ID when one was created)

> Next Steps:
>
> 1. `/blr-status` — monitor the crag after the draft is published
> 2. `/blr-draft-list` — inspect the remaining draft queue
> 3. `/blr-draft-create` — draft the next problem in the sequence
