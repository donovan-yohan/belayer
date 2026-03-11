# Multi-Provider Agent Spawner Implementation Plan

> **Status**: Complete | **Created**: 2026-03-11 | **Last Updated**: 2026-03-11
> **Design Doc**: `docs/design-docs/2026-03-11-multi-provider-spawner-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Wire the existing `AgentSpawner` interface to support Codex CLI alongside Claude Code, controlled by the `agents.provider` config field.

**Architecture:** Add `CodexSpawner` implementing the existing `AgentSpawner` interface, a `NewSpawner` factory function that selects the right spawner based on config, and wire the factory into `belayer_cmd.go` replacing the hardcoded `NewClaudeSpawner` call.

**Tech Stack:** Go, testify, tmux (via existing `TmuxManager` interface)

## File Structure

| File | Role | Action |
|------|------|--------|
| `internal/lead/codex.go` | `CodexSpawner` — builds Codex CLI command and sends to tmux | Create |
| `internal/lead/codex_test.go` | Tests for `CodexSpawner` command construction | Create |
| `internal/lead/spawner.go` | Add `NewSpawner` factory function | Modify |
| `internal/lead/spawner_test.go` | Tests for factory function | Create |
| `internal/cli/belayer_cmd.go` | Wire factory to `bcfg.Agents.Provider` | Modify |

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-11 | Design | Prepend `AppendSystemPrompt` before `InitialPrompt` for Codex | Codex has no `--append-system-prompt`; role context before task matches Claude's semantic ordering |
| 2026-03-11 | Design | Factory function (switch), not a registry | Two providers don't warrant a registry pattern |
| 2026-03-11 | Design | Agentic nodes remain Claude-only | Different abstraction (single-shot vs interactive); separate concern |

## Progress

- [x] Task 1: CodexSpawner implementation + tests
- [x] Task 2: NewSpawner factory function + tests
- [x] Task 3: Wire factory into belayer_cmd.go

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: CodexSpawner implementation + tests

**Files:**
- Create: `internal/lead/codex.go`
- Create: `internal/lead/codex_test.go`

**Context:** `internal/lead/claude.go` is the reference implementation. `codex.go` mirrors its structure. The `mockTmux` helper and `shellQuote` function are already defined in `claude_test.go` and `claude.go` respectively — both are package-level and available without re-declaration.

- [ ] **Step 1: Write the failing tests**

Create `internal/lead/codex_test.go`:

```go
package lead

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexSpawner_ImplementsInterface(t *testing.T) {
	var _ AgentSpawner = (*CodexSpawner)(nil)
}

func TestCodexSpawner_Spawn(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("test-session")
	tm.NewWindow("test-session", "api-climb-1")

	workDir := t.TempDir()
	spawner := NewCodexSpawner(tm)

	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:   "test-session",
		WindowName:    "api-climb-1",
		WorkDir:       workDir,
		InitialPrompt: "Do the thing\nwith multiple lines",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["test-session:api-climb-1"]
	assert.Contains(t, sentKeys, "cd '"+workDir+"'")
	assert.Contains(t, sentKeys, "codex --dangerously-bypass-approvals-and-sandbox")
	assert.Contains(t, sentKeys, "Do the thing")
	assert.NotContains(t, sentKeys, "claude")
}

func TestCodexSpawner_PrependSystemPrompt(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewCodexSpawner(tm)
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:        "s",
		WindowName:         "w",
		WorkDir:            t.TempDir(),
		InitialPrompt:      "Do the thing",
		AppendSystemPrompt: "You are a lead agent.",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	// Role instructions should be prepended, not passed as a flag
	assert.NotContains(t, sentKeys, "--append-system-prompt")
	// Both parts should appear in the command
	assert.Contains(t, sentKeys, "You are a lead agent.")
	assert.Contains(t, sentKeys, "Do the thing")
	// Role instructions must precede task prompt
	assert.Less(t,
		strings.Index(sentKeys, "You are a lead agent."),
		strings.Index(sentKeys, "Do the thing"),
		"role instructions must precede task prompt")
}

func TestCodexSpawner_NoSystemPrompt(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewCodexSpawner(tm)
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:   "s",
		WindowName:    "w",
		WorkDir:       t.TempDir(),
		InitialPrompt: "Do the thing",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	assert.Contains(t, sentKeys, "codex --dangerously-bypass-approvals-and-sandbox")
	assert.Contains(t, sentKeys, "Do the thing")
}

func TestCodexSpawner_ShellQuoting(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewCodexSpawner(tm)
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession:   "s",
		WindowName:    "w",
		WorkDir:       t.TempDir(),
		InitialPrompt: "Don't break",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	assert.Contains(t, sentKeys, "codex --dangerously-bypass-approvals-and-sandbox")
	assert.Contains(t, sentKeys, "Don")
}

func TestCodexSpawner_EnvInjection(t *testing.T) {
	setup := func(t *testing.T) (*mockTmux, *CodexSpawner) {
		t.Helper()
		tm := newMockTmux()
		tm.NewSession("s")
		tm.NewWindow("s", "w")
		return tm, NewCodexSpawner(tm)
	}

	t.Run("empty env produces no export prefix", func(t *testing.T) {
		tm, spawner := setup(t)
		err := spawner.Spawn(context.Background(), SpawnOpts{
			TmuxSession:   "s",
			WindowName:    "w",
			WorkDir:       t.TempDir(),
			InitialPrompt: "go",
			Env:           map[string]string{},
		})
		require.NoError(t, err)
		sentKeys := tm.keys["s:w"]
		assert.NotContains(t, sentKeys, "export ")
	})

	t.Run("single env var produces export prefix", func(t *testing.T) {
		tm, spawner := setup(t)
		err := spawner.Spawn(context.Background(), SpawnOpts{
			TmuxSession:   "s",
			WindowName:    "w",
			WorkDir:       t.TempDir(),
			InitialPrompt: "go",
			Env:           map[string]string{"MY_KEY": "my_value"},
		})
		require.NoError(t, err)
		sentKeys := tm.keys["s:w"]
		assert.Contains(t, sentKeys, "export MY_KEY='my_value' && ")
	})

	t.Run("multiple env vars all appear in prefix", func(t *testing.T) {
		tm, spawner := setup(t)
		err := spawner.Spawn(context.Background(), SpawnOpts{
			TmuxSession:   "s",
			WindowName:    "w",
			WorkDir:       t.TempDir(),
			InitialPrompt: "go",
			Env: map[string]string{
				"KEY_A": "val_a",
				"KEY_B": "val_b",
			},
		})
		require.NoError(t, err)
		sentKeys := tm.keys["s:w"]
		assert.Contains(t, sentKeys, "export KEY_A='val_a' && ")
		assert.Contains(t, sentKeys, "export KEY_B='val_b' && ")
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lead/ -run TestCodex -v`
Expected: FAIL — `CodexSpawner` type not defined

- [ ] **Step 3: Write minimal implementation**

Create `internal/lead/codex.go`:

```go
package lead

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// CodexSpawner implements AgentSpawner by launching interactive Codex CLI
// sessions in tmux windows.
type CodexSpawner struct {
	tmux tmux.TmuxManager
}

// NewCodexSpawner creates a CodexSpawner backed by the given TmuxManager.
func NewCodexSpawner(tm tmux.TmuxManager) *CodexSpawner {
	return &CodexSpawner{tmux: tm}
}

// Spawn launches an interactive Codex CLI session in the specified tmux window.
// Codex has no --append-system-prompt equivalent, so role instructions are
// prepended to the initial prompt text (role context before task).
func (c *CodexSpawner) Spawn(_ context.Context, opts SpawnOpts) error {
	// Build env exports for per-window isolation
	var envExports string
	for k, v := range opts.Env {
		envExports += fmt.Sprintf("export %s=%s && ", k, shellQuote(v))
	}

	// Prepend role instructions to the prompt since Codex has no system prompt flag
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/lead/ -run TestCodex -v`
Expected: All PASS

- [ ] **Step 5: Run full package tests for regression**

Run: `go test ./internal/lead/ -v`
Expected: All existing Claude tests + new Codex tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/lead/codex.go internal/lead/codex_test.go
git commit -m "feat: add CodexSpawner implementing AgentSpawner interface"
```

---

### Task 2: NewSpawner factory function + tests

**Files:**
- Modify: `internal/lead/spawner.go`
- Create: `internal/lead/spawner_test.go`

**Context:** The factory function goes in `spawner.go` alongside the `AgentSpawner` interface. It reads a provider string and returns the correct spawner.

- [ ] **Step 1: Write the failing tests**

Create `internal/lead/spawner_test.go`:

```go
package lead

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpawner_Claude(t *testing.T) {
	tm := newMockTmux()
	sp, err := NewSpawner("claude", tm)
	require.NoError(t, err)
	assert.IsType(t, &ClaudeSpawner{}, sp)
}

func TestNewSpawner_Codex(t *testing.T) {
	tm := newMockTmux()
	sp, err := NewSpawner("codex", tm)
	require.NoError(t, err)
	assert.IsType(t, &CodexSpawner{}, sp)
}

func TestNewSpawner_Unknown(t *testing.T) {
	tm := newMockTmux()
	sp, err := NewSpawner("gemini", tm)
	assert.Nil(t, sp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent provider")
	assert.Contains(t, err.Error(), "gemini")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lead/ -run TestNewSpawner -v`
Expected: FAIL — `NewSpawner` function not defined

- [ ] **Step 3: Add factory function to spawner.go**

Replace the full contents of `internal/lead/spawner.go` with:

```go
package lead

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/tmux"
)

// AgentSpawner launches agent sessions in tmux windows.
// This is the vendor abstraction boundary — implementations exist for
// Claude Code, Codex, or any other agent runtime.
type AgentSpawner interface {
	// Spawn launches an agent in the given tmux window with the given prompt.
	Spawn(ctx context.Context, opts SpawnOpts) error
}

// SpawnOpts contains everything needed to spawn a lead session.
type SpawnOpts struct {
	TmuxSession        string
	WindowName         string
	WorkDir            string
	InitialPrompt      string
	AppendSystemPrompt string            // Role-specific instructions appended to default system prompt via --append-system-prompt
	Env                map[string]string // Per-window environment variables (injected via export, not tmux session env)
}

// NewSpawner creates an AgentSpawner for the given provider.
// Supported providers: "claude", "codex".
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/lead/ -run TestNewSpawner -v`
Expected: All PASS

- [ ] **Step 5: Run full package tests for regression**

Run: `go test ./internal/lead/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/lead/spawner.go internal/lead/spawner_test.go
git commit -m "feat: add NewSpawner factory function for provider selection"
```

---

### Task 3: Wire factory into belayer_cmd.go

**Files:**
- Modify: `internal/cli/belayer_cmd.go:122`

**Context:** Line 122 currently reads `sp := lead.NewClaudeSpawner(tm)`. Replace with the factory call using `bcfg.Agents.Provider`. The `bcfg` variable is already available in scope (loaded earlier in the function).

- [ ] **Step 1: Verify existing tests pass before modification**

Run: `go test ./internal/cli/ -v`
Expected: PASS (or no test files — that's fine, this is a wiring change)

- [ ] **Step 2: Replace hardcoded spawner with factory call**

In `internal/cli/belayer_cmd.go`, replace:

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

- [ ] **Step 3: Verify build succeeds**

Run: `go build ./cmd/belayer`
Expected: Build succeeds with no errors

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/belayer_cmd.go
git commit -m "feat: wire agent provider config to spawner factory"
```

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Clean interface boundary — adding a second provider was straightforward because `AgentSpawner` was already well-designed
- Parallel task execution — Tasks 1 and 2 had no file conflicts

**What didn't:**
- Nothing significant — scope was narrow and well-defined

**Learnings to codify:**
- Env key quoting is a pre-existing gap in both spawners (keys not shell-quoted). Track as tech debt.
