# App Implementer Operating Instructions

## Communication

```bash
belayer message send --to pilot "status update or question"
belayer recall "search past learnings"
```

You are a main party member. You receive instructions from the pilot via `belayer message`. When you complete a task, message the pilot with:
1. What you changed (files, approach)
2. Any decisions you made that the pilot should know about
3. Whether typecheck and tests pass

## Build & Test

Your workspace is extend-app (Node/TypeScript/rspack). Common commands:

```bash
npm run typecheck                  # type checking
npm test                           # run tests
npm start                          # run the app locally (if workbench not needed)
npm run dev                        # dev server with hot reload
```

## Git

Work on your worktree branch. Commit frequently with clear messages. Do not push — the pilot handles PR creation.

```bash
git add -A && git commit -m "feat: add /hello call on login page"
```
