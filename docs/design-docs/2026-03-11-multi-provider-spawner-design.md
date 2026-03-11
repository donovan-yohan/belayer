# Multi-Provider Agent Spawner

> Wire the existing `AgentSpawner` interface to support Codex CLI alongside Claude Code, controlled by the `agents.provider` config field.

## Goal

Belayer's `AgentSpawner` interface was designed as a vendor abstraction boundary, but only `ClaudeSpawner` exists and it's hardcoded at instantiation. The `agents.provider` config field already accepts `"claude"` or `"codex"` but is never read at dispatch time. This design closes the gap: a `CodexSpawner` implementation + a factory function that reads config to select the right spawner.

## Current State

| Component | Location | Status |
|-----------|----------|--------|
| `AgentSpawner` interface | `internal/lead/spawner.go` | Exists — `Spawn(ctx, SpawnOpts) error` |
| `ClaudeSpawner` | `internal/lead/claude.go` | Exists — builds `claude --dangerously-skip-permissions` command |
| `AgentsConfig.Provider` | `internal/belayerconfig/config.go` | Defined but unused at dispatch |
| Spawner instantiation | `internal/cli/belayer_cmd.go:122` | Hardcoded to `lead.NewClaudeSpawner(tm)` |
| Default config | `internal/defaults/belayer.toml` | `provider = "claude"` |

## Approach

### 1. CodexSpawner (`internal/lead/codex.go`)

New `AgentSpawner` implementation mirroring `ClaudeSpawner`'s structure.

```go
type CodexSpawner struct {
    tmux tmux.TmuxManager
}

func NewCodexSpawner(tm tmux.TmuxManager) *CodexSpawner {
    return &CodexSpawner{tmux: tm}
}

func (c *CodexSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
    var envExports string
    for k, v := range opts.Env {
        envExports += fmt.Sprintf("export %s=%s && ", k, shellQuote(v))
    }

    // Codex has no --append-system-prompt equivalent.
    // Prepend role instructions to the initial prompt.
    prompt := opts.InitialPrompt
    if opts.AppendSystemPrompt != "" {
        prompt = opts.AppendSystemPrompt + "\n\n---\n\n" + opts.InitialPrompt
    }

    cmd := fmt.Sprintf("%scd %s && codex --dangerously-bypass-approvals-and-sandbox %s 2>&1; echo 'Codex session exited'",
        envExports,
        shellQuote(opts.WorkDir),
        shellQuote(prompt))

    return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}
```

Key differences from Claude invocation:
- **Permissions flag**: `--dangerously-bypass-approvals-and-sandbox` (vs `--dangerously-skip-permissions`)
- **System prompt**: No `--append-system-prompt` flag — role instructions prepended to the prompt text. `AppendSystemPrompt` is placed _before_ `InitialPrompt` in the concatenation so role context comes first (matching the semantic intent of Claude's system prompt ordering).
- **Prompt argument**: Codex CLI accepts a bare positional `[PROMPT]` argument (`codex [OPTIONS] [PROMPT]`), same pattern as Claude CLI. Verified via `codex --help`.
- **Working directory**: Codex uses `-C <dir>` but we already `cd` into the workdir via shell, so this is unnecessary
- **`shellQuote`**: Reuses the existing package-level `shellQuote()` from `claude.go` — same `lead` package, no re-declaration needed.

### 2. Factory Function (`internal/lead/spawner.go`)

```go
func NewSpawner(provider string, tm tmux.TmuxManager) (AgentSpawner, error) {
    switch provider {
    case "claude":
        return NewClaudeSpawner(tm), nil
    case "codex":
        return NewCodexSpawner(tm), nil
    default:
        return nil, fmt.Errorf("unknown agent provider: %q (expected \"claude\" or \"codex\")", provider)
    }
}
```

### 3. Wire Config to Factory (`internal/cli/belayer_cmd.go`)

Replace:
```go
sp := lead.NewClaudeSpawner(tm)
```

With:
```go
sp, err := lead.NewSpawner(bcfg.Agents.Provider, tm)
if err != nil {
    return fmt.Errorf("creating agent spawner: %w", err)
}
```

## Scope Boundary

**In scope:**
- `CodexSpawner` for interactive lead/spotter/anchor sessions
- Factory function + config wiring
- Tests for `CodexSpawner` (mirroring `claude_test.go`)

**Out of scope:**
- Agentic nodes (`claude -p` for decomposition, sufficiency, PR body gen) — these are non-interactive single-shot calls, a separate abstraction. They remain Claude-only for now.
- Model selection (`lead_model`, `review_model`) — Codex uses its own model config; belayer doesn't need to pass model flags.
- Codex-specific config (sandbox permissions, search flags) — can be added later via `AgentsConfig` extension if needed.
- `AgentsConfig.Permissions` field — currently unused by both spawners. Claude hardcodes `--dangerously-skip-permissions` and Codex hardcodes `--dangerously-bypass-approvals-and-sandbox`. Wiring this field to control permission flags is a future concern.

## Key Decisions

| Decision | Rationale |
|----------|-----------|
| Prepend role instructions to prompt (not a separate flag) | Codex has no `--append-system-prompt` equivalent; prompt concatenation is the only option. Role instructions placed _before_ task prompt so context comes first. |
| Use `--dangerously-bypass-approvals-and-sandbox` | Matches Claude's `--dangerously-skip-permissions` — leads need unrestricted file access |
| Factory function on `spawner.go` (not a registry) | Two providers don't warrant a registry pattern; a switch is clearer |
| Leave agentic nodes as Claude-only | Different abstraction (single-shot `claude -p` vs interactive session); separate concern |
| `cd` into workdir via shell, not `-C` flag | Consistent with existing `ClaudeSpawner` pattern; works for both agents |

## Files Changed

| File | Change |
|------|--------|
| `internal/lead/codex.go` | New — `CodexSpawner` implementation |
| `internal/lead/codex_test.go` | New — tests mirroring `claude_test.go` |
| `internal/lead/spawner.go` | Add `NewSpawner` factory function |
| `internal/lead/spawner_test.go` | New — factory function tests |
| `internal/cli/belayer_cmd.go` | Wire factory to config |

## Testing

- `codex_test.go`: Verify command construction (env exports, prompt prepending, shell quoting)
- `spawner_test.go`: Verify factory returns correct type for each provider, errors on unknown
- Existing `claude_test.go`: Unchanged — confirms no regression
- Integration: Manual test with `provider = "codex"` in crag config
