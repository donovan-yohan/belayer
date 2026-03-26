# Installing Belayer for Codex

Belayer uses native Codex skill discovery, following the same basic pattern as Superpowers: expose a single `skills/` tree and mount it into `~/.agents/skills/`.

## Quick Install from a Repo Checkout

If you are working from a local clone of this repository:

1. Refresh the generated Codex skill pack from the plugin source:

   ```bash
   go run ./cmd/gencodexskills
   ```

2. Mount the generated `skills/` tree into Codex's discovery directory:

   ```bash
   mkdir -p ~/.agents/skills
   ln -s /absolute/path/to/belayer/skills ~/.agents/skills/belayer
   ```

3. Restart Codex.

This gives you a repo-local install that updates whenever you regenerate `skills/`.

## Quick Install from the Belayer CLI

If you installed Belayer as a binary:

```bash
belayer init
```

When `codex` is on your `PATH`, `belayer init`:

- writes generated Belayer skills to `~/.belayer/agent-assets/codex/<version>/skills/`
- mounts that tree at `~/.agents/skills/belayer`
- uses a symlink when possible and falls back to a copy if the platform blocks symlinks

Restart Codex after the install completes.

## Single Source of Truth

- `plugins/` is the authored workflow source shared with Claude Code
- `skills/` is the generated Codex projection of that source
- command `description:` frontmatter in `plugins/*/commands/*.md` becomes the Codex `SKILL.md` description field
- static skills under `plugins/*/skills/` are copied into the Codex pack with local reference paths rewritten automatically

Do not hand-edit `skills/`. Regenerate it from `plugins/`.

## Updating

After changing any file under `plugins/`:

```bash
go run ./cmd/gencodexskills
go test . -run 'TestCodexSkillFiles|TestTrackedCodexSkillsSnapshot|TestPluginVersion'
```

If you installed from a repo checkout through the symlink, Codex will pick up the refreshed content after restart.

If you installed through `belayer init`, rerun `belayer init`.

## Repair / Reinstall

Repo checkout install:

```bash
rm -rf ~/.agents/skills/belayer
ln -s /absolute/path/to/belayer/skills ~/.agents/skills/belayer
```

Belayer CLI install:

```bash
belayer init
```

If you need to clear the cached Belayer Codex pack first:

```bash
rm -rf ~/.agents/skills/belayer
rm -rf ~/.belayer/agent-assets/codex
```

Then run `belayer init` again and restart Codex.

## Troubleshooting

### Skills do not appear

1. Verify `codex` is installed:

   ```bash
   command -v codex
   ```

2. Verify the mount exists:

   ```bash
   ls -la ~/.agents/skills/belayer
   ```

3. If using the Belayer CLI install, verify the cached pack exists:

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
