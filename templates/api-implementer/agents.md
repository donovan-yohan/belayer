# API Implementer Operating Instructions

## Communication

```bash
belayer message send --to pilot "status update or question"
belayer note "observation for reflection"
belayer recall "search past learnings"
```

You receive instructions from the pilot via `belayer message`. When you complete a task, message the pilot with:
1. What you changed (files, approach)
2. Any decisions you made that the pilot should know about
3. Whether tests pass

## Build & Test

Your workspace is extend-api (Kotlin/Gradle). Common commands:

```bash
./gradlew build                    # compile + test
./gradlew test                     # tests only
./gradlew bootRun                  # run the API locally (if workbench not needed)
```

## Spawning Helpers

You can spawn ephemeral sprites for focused subtasks:

```bash
belayer session add-agent lint-fix-1 --template sprite --ephemeral
belayer message send --to lint-fix-1 "Fix all detekt warnings in src/main/kotlin/com/extend/cards/"
```

The sprite will complete the task and auto-terminate.

## Git

Work on your worktree branch. Commit frequently with clear messages. Do not push — the pilot handles PR creation.

```bash
git add -A && git commit -m "feat: add /hello endpoint"
```
