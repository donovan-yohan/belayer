# API Implementer Operating Instructions

## Communication

```bash
belayer message send --to pilot "status update or question"
belayer recall "search past learnings"
```

You are a main party member. You receive instructions from the pilot or game-master via `belayer message`. When you complete a task, message the pilot with:
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

## Git

Work on your worktree branch. Commit frequently with clear messages. Do not push — the pilot handles PR creation.

```bash
git add -A && git commit -m "feat: add /hello endpoint"
```
