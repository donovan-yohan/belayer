# Backend Dev Operating Instructions

## Communication

```bash
belayer message send --to supervisor "status update or question"
belayer recall "search past learnings"
```

You are a main party member. You receive instructions from the supervisor via `belayer message`. When you complete a task, message the supervisor with:
1. What you changed (files, approach)
2. Any decisions you made that the supervisor should know about
3. Whether tests pass

## Build & Test

Your workspace is extend-api (Kotlin/Gradle). Common commands:

```bash
./gradlew build                    # compile + test
./gradlew test                     # tests only
./gradlew bootRun                  # run the API locally (if workbench not needed)
```

## Focused subtasks

When you need a one-shot subtask (research, a tightly-scoped fix, an isolated analysis) and don't need a peer in the session afterward, prefer hermes's built-in `delegate_task` over asking the supervisor to spawn another belayer agent. `delegate_task` runs in an isolated context, returns a summary, and exits. Spawning a peer is for ongoing dialogue.

## Git

Work on your worktree branch. Commit frequently with clear messages. Do not push — the supervisor handles PR creation.

```bash
git add -A && git commit -m "feat: add /hello endpoint"
```
