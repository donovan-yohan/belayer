# Installing Belayer for Codex

Belayer installs native Codex skills through `belayer init`.

## Quick Install

1. Install the Codex CLI so `codex` is on your `PATH`.
2. Run:

   ```bash
   belayer init
   ```

3. Restart Codex.

## What `belayer init` Does

- Writes generated Belayer skills to:

  ```text
  ~/.belayer/agent-assets/codex/<version>/skills/
  ```

- Mounts that skill tree at:

  ```text
  ~/.agents/skills/belayer
  ```

- Installs:
  - `harness-init`
  - `harness-brainstorm`
  - `harness-bug`
  - `harness-refactor`
  - `harness-refactor-status`
  - `harness-plan`
  - `harness-orchestrate`
  - `harness-review`
  - `harness-reflect`
  - `harness-prune`
  - `harness-complete`
  - `harness-loop`
  - `harness-batch`
  - `pr-author`
  - `pr-review`
  - `pr-resolve`
  - `pr-update`
  - `pr-automate`
  - `strangler-fig`

## Repair / Reinstall

Re-run:

```bash
belayer init
```

That is the supported repair path.

If you need to clear the installed Belayer Codex pack first:

```bash
rm -rf ~/.agents/skills/belayer
rm -rf ~/.belayer/agent-assets/codex
```

Then run `belayer init` again and restart Codex.

## Troubleshooting

### Codex skills do not appear

1. Verify `codex` is installed:

   ```bash
   command -v codex
   ```

2. Verify the mounted skill tree exists:

   ```bash
   ls -la ~/.agents/skills/belayer
   ```

3. Verify the generated versioned tree exists:

   ```bash
   ls -la ~/.belayer/agent-assets/codex
   ```

4. Restart Codex. Skill discovery happens on startup.

### Harness skills mention missing workflow skills

Belayer's current harness wrappers still compose with Superpowers-style skills such as:

- `brainstorming`
- `writing-plans`
- `systematic-debugging`
- `verification-before-completion`

For the full workflow behavior in Codex, install Superpowers alongside Belayer.
