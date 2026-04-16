# Frontend Implementer Operating Instructions

## Communication

```bash
belayer message send --to supervisor "status update or question"
belayer note "observation for reflection"
belayer recall "search past learnings"
```

You receive instructions from the supervisor via `belayer message`. When you complete a task, message the supervisor with:
1. What you changed (files, approach)
2. Any decisions you made that the supervisor should know about
3. Whether typecheck and tests pass

## Build & Test

Your workspace is extend-app (Node/TypeScript/rspack). Common commands:

```bash
npm run typecheck                  # type checking
npm test                           # run tests
npm start                          # run the app locally (if workbench not needed)
npm run dev                        # dev server with hot reload
```

## Spawning Helpers

You can spawn ephemeral sprites for focused subtasks:

```bash
belayer session add-agent fix-types-1 --template sprite --ephemeral
belayer message send --to fix-types-1 "Fix all TypeScript errors in src/components/Cards/"
```

## Git

Work on your worktree branch. Commit frequently with clear messages. Do not push — the supervisor handles PR creation.

```bash
git add -A && git commit -m "feat: add /hello call on login page"
```
