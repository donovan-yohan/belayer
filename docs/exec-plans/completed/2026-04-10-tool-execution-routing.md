# Tool Execution Routing — Completion Plan

> **Status**: Complete | **Created**: 2026-04-10 | **Last Updated**: 2026-04-10
> **Design Doc**: `PROMPT.md` (Phase 2: Tool Execution Routing, Issue #44)
> **Consulted Learnings**: L-20260320-json-marshal-escaping (shell quoting patterns), L-20260325-merge-friendly-formats (test design)
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-04-10 | Design | Tool definitions live in environment YAML, parsed into `agent.ToolSpec` | Collocates tool config with the environment it targets |
| 2026-04-10 | Design | All `{{.variable}}` template values shell-quoted via `shell.Quote()` | Agent input is the attack surface — pre-quoting prevents injection |
| 2026-04-10 | Design | In-memory per-session tool registry with RWMutex | Matches daemon's existing pattern; persistence not needed since tools come from environment config |
| 2026-04-10 | Design | Four execution targets: agent, workbench, infra, host | Each maps to a docker compose service or direct host shell |
| 2026-04-10 | Assessment | Core implementation exists — plan focuses on gaps | Executor, handlers, CLI commands, and client methods already pass tests |
| 2026-04-10 | Implementation | Added duplicate tool name detection to ValidateEnvironment | Code review caught that duplicate names would silently shadow — validate at load time |
| 2026-04-10 | Implementation | RegisterToolsForSession documented as single-call contract | Not idempotent by design — callers must not invoke twice per session |

## Progress

- [x] Task 1: Validate tools in environment config _(completed 2026-04-10)_
- [x] Task 2: Auto-register environment tools on session creation _(completed 2026-04-10)_
- [x] Task 3: CLI tool command tests _(completed 2026-04-10)_
- [x] Task 4: Handler-level timeout test _(completed 2026-04-10)_
- [x] Task 5: Environment tool YAML parsing tests _(completed 2026-04-10)_
- [x] Task 6: Verify build, all tests pass, `go vet` _(completed 2026-04-10)_

## Surprises & Discoveries

| Date | What | Plan Impact | Action Taken |
|------|------|-------------|--------------|
| 2026-04-10 | Code review caught duplicate tool name gap | Added validation not in original plan | Added `seen` map + duplicate name test to Task 1 |

## Plan Drift

| Task | Plan Said | Actually Happened | Why |
|------|-----------|-------------------|-----|
| Task 5 | Only modify `environment_test.go` | Also fixed `executor.go` context cancellation ordering | `captureCommand` checked `ExitError` before `ctx.Err()` — context deadline surfaced as exit code -1 instead of `DeadlineExceeded`, causing Task 4's timeout test to fail. Fix: check `ctx.Err()` first. |

---

## Existing Code Assessment

The core Phase 2 implementation is already in place and passing tests:

- **`internal/agent/toolspec.go`** — `ToolSpec`, `ToolExec`, `ToolConstraints`, `ValidTargets`, `EffectiveTimeout()`
- **`internal/agent/executor.go`** — `Executor.Execute()`, `renderCommand()`, `runCompose()`, `runHost()`, `RenderError`
- **`internal/agent/executor_test.go`** — 10 render tests (including 4 injection variants), executor validation, host execution, exit code, timeout default
- **`internal/daemon/tools.go`** — `handleRegisterTool`, `handleListTools`, `handleExecuteTool` with full event logging
- **`internal/daemon/tools_test.go`** — Registration (success, not found, missing name, invalid target, idempotent), listing (empty, with tools, not found), execution (host echo, injection prevention, not found, session not found, event logging, exit code)
- **`internal/cli/tool.go`** — `tool run` and `tool list` commands with flags and env var fallbacks
- **`internal/cli/client.go`** — `RegisterTool()`, `ExecuteTool()`, `ListTools()` client methods
- **`internal/docker/environment.go`** — `EnvironmentConfig.Tools []agent.ToolSpec` field with YAML tags

**What's missing (this plan fills these gaps):**

1. `ValidateEnvironment()` doesn't validate tools — invalid tool names/targets in YAML pass silently
2. No auto-registration path: environment tools never reach the daemon's in-memory registry
3. No CLI tool command tests (`internal/cli/tool_test.go` doesn't exist)
4. No handler-level timeout enforcement test
5. No test proving environment YAML with tools parses correctly

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/docker/environment.go` | Add tool validation to `ValidateEnvironment()` |
| Modify | `internal/docker/environment_test.go` | Test tool validation + YAML parsing |
| Modify | `internal/daemon/daemon.go` | Expose method to load environment tools into session |
| Modify | `internal/daemon/tools.go` | Add `LoadEnvironmentTools()` for bulk tool registration |
| Modify | `internal/daemon/tools_test.go` | Test auto-registration + timeout behavior |
| Create | `internal/cli/tool_test.go` | CLI command tests for `tool run` and `tool list` |

---

### Task 1: Validate tools in environment config

**Goal:** `ValidateEnvironment()` rejects environment YAML with invalid tool definitions (empty name, bad target, empty command).

**Files:**
- Modify: `internal/docker/environment.go:301-324` (add tool validation to `ValidateEnvironment()`)
- Modify: `internal/docker/environment_test.go` (if exists, otherwise create test file)

- [ ] **Step 1: Write failing tests for tool validation**

Check if `internal/docker/environment_test.go` exists. Add these tests:

```go
func TestValidateEnvironment_ToolMissingName(t *testing.T) {
	cfg := &EnvironmentConfig{
		Tools: []agent.ToolSpec{
			{Exec: agent.ToolExec{Target: "host", Command: "echo hi"}},
		},
	}
	err := ValidateEnvironment(cfg)
	if err == nil {
		t.Fatal("expected error for tool with missing name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention name, got: %v", err)
	}
}

func TestValidateEnvironment_ToolInvalidTarget(t *testing.T) {
	cfg := &EnvironmentConfig{
		Tools: []agent.ToolSpec{
			{Name: "bad", Exec: agent.ToolExec{Target: "nowhere", Command: "echo hi"}},
		},
	}
	err := ValidateEnvironment(cfg)
	if err == nil {
		t.Fatal("expected error for tool with invalid target")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error should mention target, got: %v", err)
	}
}

func TestValidateEnvironment_ToolMissingCommand(t *testing.T) {
	cfg := &EnvironmentConfig{
		Tools: []agent.ToolSpec{
			{Name: "bad", Exec: agent.ToolExec{Target: "host"}},
		},
	}
	err := ValidateEnvironment(cfg)
	if err == nil {
		t.Fatal("expected error for tool with missing command")
	}
}

func TestValidateEnvironment_ValidTools(t *testing.T) {
	cfg := &EnvironmentConfig{
		Tools: []agent.ToolSpec{
			{
				Name: "echo",
				Exec: agent.ToolExec{Target: "host", Command: "echo {{.msg}}"},
			},
			{
				Name: "build",
				Exec: agent.ToolExec{Target: "workbench", Command: "make build"},
			},
		},
	}
	if err := ValidateEnvironment(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/docker/ -run TestValidateEnvironment_Tool -v`
Expected: FAIL — validation doesn't check tools yet.

- [ ] **Step 3: Implement tool validation**

Add tool validation to `ValidateEnvironment()` in `internal/docker/environment.go`, after the existing workbench validation block (after line 323):

```go
for i, tool := range cfg.Tools {
	if tool.Name == "" {
		return fmt.Errorf("docker: environment: tools[%d]: name is required", i)
	}
	if tool.Exec.Target == "" {
		return fmt.Errorf("docker: environment: tools[%d] (%s): exec.target is required", i, tool.Name)
	}
	if !agent.ValidTargets[tool.Exec.Target] {
		return fmt.Errorf("docker: environment: tools[%d] (%s): invalid exec.target %q", i, tool.Name, tool.Exec.Target)
	}
	if tool.Exec.Command == "" {
		return fmt.Errorf("docker: environment: tools[%d] (%s): exec.command is required", i, tool.Name)
	}
}
```

This requires adding `"github.com/donovan-yohan/belayer/internal/agent"` to the imports if not already present. Check — it IS already imported (line 10).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/docker/ -run TestValidateEnvironment_Tool -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/docker/environment.go internal/docker/environment_test.go
git commit -m "feat(docker): validate tool definitions in environment config

Closes partial #44 — ValidateEnvironment now rejects tools with empty
name, invalid exec.target, or missing exec.command."
```

---

### Task 2: Auto-register environment tools on session creation

**Goal:** When the daemon creates a session that references an environment with tools, those tools are automatically loaded into the daemon's in-memory tool registry. This bridges the gap between `EnvironmentConfig.Tools` and the daemon's `d.tools` map.

**Files:**
- Modify: `internal/daemon/tools.go` — Add `RegisterToolsForSession()` method
- Modify: `internal/daemon/tools_test.go` — Test bulk registration

- [ ] **Step 1: Write failing test for bulk tool registration**

Add to `internal/daemon/tools_test.go`:

```go
func TestRegisterToolsForSession(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	tools := []agent.ToolSpec{
		{
			Name:        "echo-tool",
			Description: "Echoes a message",
			Exec:        agent.ToolExec{Target: "host", Command: "echo {{.msg}}", Timeout: 10},
		},
		{
			Name:        "build-check",
			Description: "Run build",
			Exec:        agent.ToolExec{Target: "workbench", Command: "make build"},
		},
	}

	d.RegisterToolsForSession(sessID, tools)

	// Verify tools are listed.
	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tools", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list tools: got %d", rr.Code)
	}
	listed := decodeJSON[[]agent.ToolSpec](t, rr)
	if len(listed) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(listed))
	}
	if listed[0].Name != "echo-tool" {
		t.Errorf("tools[0].Name = %q, want echo-tool", listed[0].Name)
	}
	if listed[1].Name != "build-check" {
		t.Errorf("tools[1].Name = %q, want build-check", listed[1].Name)
	}
}

func TestRegisterToolsForSession_Empty(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	d.RegisterToolsForSession(sessID, nil)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tools", nil)
	listed := decodeJSON[[]agent.ToolSpec](t, rr)
	if len(listed) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(listed))
	}
}

func TestRegisterToolsForSession_LogsEvents(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	tools := []agent.ToolSpec{
		{Name: "t1", Exec: agent.ToolExec{Target: "host", Command: "echo 1"}},
		{Name: "t2", Exec: agent.ToolExec{Target: "host", Command: "echo 2"}},
	}
	d.RegisterToolsForSession(sessID, tools)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/events", nil)
	events := decodeJSON[[]store.SessionEvent](t, rr)
	count := 0
	for _, e := range events {
		if e.Type == "tool_registered" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 tool_registered events, got %d", count)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/daemon/ -run TestRegisterToolsForSession -v`
Expected: FAIL — `RegisterToolsForSession` doesn't exist.

- [ ] **Step 3: Implement `RegisterToolsForSession`**

Add to `internal/daemon/tools.go`:

```go
// RegisterToolsForSession loads a slice of ToolSpecs into the daemon's in-memory
// registry for the given session. This is called when a session starts with an
// environment that declares tools. Each tool is logged as a tool_registered event.
func (d *Daemon) RegisterToolsForSession(sessionID string, tools []agent.ToolSpec) {
	if len(tools) == 0 {
		return
	}

	d.toolsMu.Lock()
	d.tools[sessionID] = append(d.tools[sessionID], tools...)
	d.toolsMu.Unlock()

	for _, tool := range tools {
		d.store.LogEvent(store.SessionEvent{
			SessionID: sessionID,
			Type:      "tool_registered",
			Data:      mustJSON(map[string]string{"tool": tool.Name, "target": tool.Exec.Target, "source": "environment"}),
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon/ -run TestRegisterToolsForSession -v`
Expected: PASS

- [ ] **Step 5: Run full daemon test suite**

Run: `go test ./internal/daemon/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/tools.go internal/daemon/tools_test.go
git commit -m "feat(daemon): add RegisterToolsForSession for environment tool loading

Exposes a method for the session creation flow to bulk-register tools
from environment config. Each tool gets a tool_registered event with
source=environment for audit trail differentiation."
```

---

### Task 3: CLI tool command tests

**Goal:** Test the `tool run` and `tool list` CLI commands. These tests validate flag parsing, session requirement enforcement, and output formatting — not daemon communication (that's tested in `tools_test.go`).

**Files:**
- Create: `internal/cli/tool_test.go`

- [ ] **Step 1: Write CLI tool tests**

Create `internal/cli/tool_test.go`. Follow the pattern from `internal/cli/workbench_test.go` which tests Cobra commands:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestToolRunCmd_RequiresSession(t *testing.T) {
	cmd := newToolRunCmd()
	cmd.SetArgs([]string{"my-tool", "--input", `{"key":"val"}`})
	// Clear env so BELAYER_SESSION_ID doesn't leak in.
	t.Setenv("BELAYER_SESSION_ID", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --session is missing")
	}
	if !strings.Contains(err.Error(), "--session is required") {
		t.Errorf("error = %q, want mention of --session", err.Error())
	}
}

func TestToolRunCmd_RequiresToolName(t *testing.T) {
	cmd := newToolRunCmd()
	cmd.SetArgs([]string{"--session", "abc"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when tool name is missing")
	}
}

func TestToolRunCmd_InvalidJSON(t *testing.T) {
	cmd := newToolRunCmd()
	cmd.SetArgs([]string{"my-tool", "--session", "abc", "--input", "not-json"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("error = %q, want mention of valid JSON", err.Error())
	}
}

func TestToolRunCmd_SessionFromEnv(t *testing.T) {
	t.Setenv("BELAYER_SESSION_ID", "env-session")
	cmd := newToolRunCmd()
	// Provide tool name but no --session; should pick up from env.
	// This will fail at the client level (no daemon running), but should NOT
	// fail with "--session is required".
	cmd.SetArgs([]string{"my-tool"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		// Unexpected success means a daemon is actually running — skip.
		t.Skip("daemon appears to be running")
	}
	if strings.Contains(err.Error(), "--session is required") {
		t.Errorf("should have read session from BELAYER_SESSION_ID, got: %v", err)
	}
}

func TestToolListCmd_RequiresSession(t *testing.T) {
	cmd := newToolListCmd()
	cmd.SetArgs([]string{})
	t.Setenv("BELAYER_SESSION_ID", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --session is missing")
	}
	if !strings.Contains(err.Error(), "--session is required") {
		t.Errorf("error = %q, want mention of --session", err.Error())
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestTool -v`
Expected: PASS — these test validation logic that's already implemented.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/tool_test.go
git commit -m "test(cli): add tool command unit tests

Tests flag validation, session requirement, JSON parsing, and env var
fallback for tool run and tool list commands."
```

---

### Task 4: Handler-level timeout enforcement test

**Goal:** Verify that the daemon handler returns 504 Gateway Timeout when a tool exceeds its configured timeout.

**Files:**
- Modify: `internal/daemon/tools_test.go`

- [ ] **Step 1: Write the timeout test**

Add to `internal/daemon/tools_test.go`:

```go
func TestExecuteTool_Timeout(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	// Register a tool with a very short timeout that will exceed it.
	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "slow-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "sleep 10",
			Timeout: 1, // 1 second timeout
		},
	})

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/slow-tool",
		map[string]string{},
	)
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d, body=%s", rr.Code, rr.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestExecuteTool_Timeout -v -timeout 30s`
Expected: PASS — the handler already maps `context.DeadlineExceeded` to 504.

- [ ] **Step 3: Write test for RenderError → 400**

Add to `internal/daemon/tools_test.go`:

```go
func TestExecuteTool_MissingInputKey(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "template-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "echo {{.required_key}}",
			Timeout: 5,
		},
	})

	// Execute without providing the required key.
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/template-tool",
		map[string]string{"wrong_key": "value"},
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing template key, got %d, body=%s", rr.Code, rr.Body.String())
	}
}
```

- [ ] **Step 4: Run both new tests**

Run: `go test ./internal/daemon/ -run "TestExecuteTool_Timeout|TestExecuteTool_MissingInputKey" -v -timeout 30s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/tools_test.go
git commit -m "test(daemon): add timeout and render error handler tests

Verifies that tool timeout returns 504 and missing template key returns
400 through the full daemon handler path."
```

---

### Task 5: Environment tool YAML parsing tests

**Goal:** Verify that environment YAML with a `tools:` section parses correctly into `EnvironmentConfig.Tools`.

**Files:**
- Modify: `internal/docker/environment_test.go`

- [ ] **Step 1: Write the YAML parsing test**

Add to `internal/docker/environment_test.go` (check the file exists first, create if needed):

```go
func TestLoadEnvironment_ToolsParsed(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.yaml")
	content := `
name: test-env
type: docker-compose
networking:
  type: none
tools:
  - name: db-query
    description: "Read-only SQL query"
    input:
      query: string
    exec:
      target: infra
      command: 'psql "$DATABASE_URL" -c {{.query}}'
      timeout: 30
    constraints:
      read_only: true
      audit: true
  - name: build-check
    description: "Compile the project"
    exec:
      target: workbench
      command: "make build 2>&1"
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadEnvironment(envPath)
	if err != nil {
		t.Fatalf("LoadEnvironment: %v", err)
	}

	if len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(cfg.Tools))
	}

	// First tool: db-query
	tool := cfg.Tools[0]
	if tool.Name != "db-query" {
		t.Errorf("tools[0].Name = %q, want db-query", tool.Name)
	}
	if tool.Description != "Read-only SQL query" {
		t.Errorf("tools[0].Description = %q", tool.Description)
	}
	if tool.Exec.Target != "infra" {
		t.Errorf("tools[0].Exec.Target = %q, want infra", tool.Exec.Target)
	}
	if tool.Exec.Timeout != 30 {
		t.Errorf("tools[0].Exec.Timeout = %d, want 30", tool.Exec.Timeout)
	}
	if !tool.Constraints.ReadOnly {
		t.Error("tools[0].Constraints.ReadOnly should be true")
	}
	if !tool.Constraints.Audit {
		t.Error("tools[0].Constraints.Audit should be true")
	}
	if tool.InputSchema == nil || tool.InputSchema["query"] != "string" {
		t.Errorf("tools[0].InputSchema = %v, want {query: string}", tool.InputSchema)
	}

	// Second tool: build-check
	tool2 := cfg.Tools[1]
	if tool2.Name != "build-check" {
		t.Errorf("tools[1].Name = %q, want build-check", tool2.Name)
	}
	if tool2.Exec.Target != "workbench" {
		t.Errorf("tools[1].Exec.Target = %q, want workbench", tool2.Exec.Target)
	}
}

func TestLoadEnvironment_NoTools(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.yaml")
	content := `
name: bare-env
type: docker-compose
networking:
  type: none
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadEnvironment(envPath)
	if err != nil {
		t.Fatalf("LoadEnvironment: %v", err)
	}
	if len(cfg.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(cfg.Tools))
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/docker/ -run "TestLoadEnvironment_Tools|TestLoadEnvironment_NoTools" -v`
Expected: PASS — YAML parsing is already wired via struct tags.

- [ ] **Step 3: Commit**

```bash
git add internal/docker/environment_test.go
git commit -m "test(docker): verify environment YAML tool parsing

Confirms that tools: section in environment YAML parses into
EnvironmentConfig.Tools with all fields including constraints and
input schema."
```

---

### Task 6: Final verification

**Goal:** Ensure everything builds, all tests pass, no vet warnings.

**Files:** None (verification only)

- [ ] **Step 1: Run go vet**

Run: `go vet ./...`
Expected: No output (clean)

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`
Expected: All packages PASS except the pre-existing CLI smoke test failures (which depend on a missing local environment file — not related to our changes).

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/belayer`
Expected: Clean build, no errors.

- [ ] **Step 4: Verify our specific tests**

Run: `go test ./internal/daemon/ ./internal/docker/ ./internal/agent/ ./internal/cli/ -run "TestValidateEnvironment_Tool|TestRegisterTools|TestExecuteTool_Timeout|TestExecuteTool_MissingInput|TestLoadEnvironment_Tool|TestLoadEnvironment_NoTools|TestTool" -v`
Expected: All PASS

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
