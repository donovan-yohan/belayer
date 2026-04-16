# Tool Catalog & Identity Co-location Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Co-locate belayer tool declarations with agent identity templates, enforce role-based tool gating, and rename planner to supervisor.

**Architecture:** Each agent template's `agent.yaml` declares which belayer tools it receives. The daemon reads this at spawn time and passes the list to the bridge via `BELAYER_TOOLS` env var. The bridge filters tool registration to only the declared set. The hardcoded agent name `"planner"` is renamed to `"supervisor"` throughout.

**Tech Stack:** Go (daemon/CLI), Python (hermes_bridge), YAML (templates)

---

### Task 1: Rename templates to match actual agent roster

**Files:**
- Remove: `templates/pilot/`
- Remove: `templates/app-implementer/`
- Remove: `templates/api-implementer/`
- Create: `templates/supervisor/agent.yaml`
- Create: `templates/supervisor/system-prompt.md`
- Create: `templates/supervisor/agents.md`
- Create: `templates/frontend/agent.yaml`
- Create: `templates/frontend/system-prompt.md`
- Create: `templates/frontend/agents.md`
- Create: `templates/backend/agent.yaml`
- Create: `templates/backend/system-prompt.md`
- Create: `templates/backend/agents.md`
- Create: `templates/qa/agent.yaml`
- Create: `templates/qa/system-prompt.md`
- Create: `templates/qa/agents.md`

- [ ] **Step 1: Create supervisor template from pilot**

```bash
cp -r templates/pilot templates/supervisor
```

Edit `templates/supervisor/agent.yaml`:

```yaml
schema_version: "1"
description: "Supervisor — orchestrates implementation across repos"
vendor: claude
model: opus
max_turns: 200
max_duration: "4h"
ephemeral: false
workspace: none
belayer_tools:
  - belayer_spawn_agent
  - belayer_request_completion
```

Edit `templates/supervisor/system-prompt.md` — replace first line:

```markdown
You are the supervisor agent for this belayer session. You orchestrate — you do NOT write code.
```

(Rest of the system prompt content stays the same, with any `pilot` references changed to `supervisor`.)

- [ ] **Step 2: Create frontend template from app-implementer**

```bash
cp -r templates/app-implementer templates/frontend
```

Edit `templates/frontend/agent.yaml`:

```yaml
schema_version: "1"
description: "Frontend implementer — writes code in the frontend/web app"
vendor: opencode
model: kimi-2.5
max_turns: 100
max_duration: "2h"
ephemeral: false
workspace: inherit
belayer_tools: []
```

- [ ] **Step 3: Create backend template from api-implementer**

```bash
cp -r templates/api-implementer templates/backend
```

Edit `templates/backend/agent.yaml`:

```yaml
schema_version: "1"
description: "Backend implementer — writes code in the backend/API server"
vendor: opencode
model: kimi-2.5
max_turns: 100
max_duration: "2h"
ephemeral: false
workspace: inherit
belayer_tools: []
```

- [ ] **Step 4: Create qa template**

Create `templates/qa/agent.yaml`:

```yaml
schema_version: "1"
description: "QA — validates implementation against spec via browser and CLI testing"
vendor: claude
model: sonnet
max_turns: 30
max_duration: "30m"
ephemeral: true
workspace: inherit
belayer_tools: []
```

Create `templates/qa/system-prompt.md`:

```markdown
You are the QA agent. Your job is to verify that the implementation works correctly by testing it — not by reading code.

Run the application. Use it. Try the happy path. Try edge cases. Compare what you see against the spec. Report what works and what doesn't.

You are not a code reviewer. You test from the outside.
```

Create `templates/qa/agents.md`:

```markdown
# QA Agent

## Tools
You have access to browser automation, terminal commands, and file reading. Use them to verify the implementation against the spec.

## Workflow
1. Read the spec to understand what was requested.
2. Start the application (dev server, build, etc.).
3. Test each acceptance criterion from the spec.
4. Report findings to the supervisor via belayer_send_message.
5. Register your QA report as an artifact via belayer_create_artifact.
6. Report your status as done via belayer_report_status.
```

- [ ] **Step 5: Add belayer_tools to existing templates**

Edit `templates/pm/agent.yaml`:

```yaml
schema_version: "1"
description: "PM — adversarial spec-vs-reality verification before run completion"
vendor: claude
model: sonnet
max_turns: 30
max_duration: "30m"
ephemeral: true
workspace: inherit
belayer_tools:
  - belayer_approve_completion
  - belayer_reject_completion
```

Edit `templates/reviewer/agent.yaml`:

```yaml
schema_version: "1"
description: "Reviewer — evaluates diffs for correctness, style, and completeness"
vendor: claude
model: sonnet
max_turns: 20
max_duration: "30m"
ephemeral: true
workspace: none
belayer_tools: []
```

Edit `templates/sprite/agent.yaml`:

```yaml
schema_version: "1"
description: "Sprite — ephemeral worker for a single focused subtask"
vendor: claude
model: haiku
max_turns: 10
max_duration: "10m"
ephemeral: true
workspace: inherit
belayer_tools: []
```

- [ ] **Step 6: Remove old template directories**

```bash
rm -rf templates/pilot templates/app-implementer templates/api-implementer
```

- [ ] **Step 7: Verify template structure**

```bash
ls templates/
# Expected: backend  frontend  pm  qa  reviewer  sprite  supervisor

for d in templates/*/; do echo "--- $d ---"; cat "$d/agent.yaml"; done
# Verify each has belayer_tools field
```

- [ ] **Step 8: Commit**

```bash
git add templates/
git commit -m "refactor: rename agent templates to match actual roster

Rename pilot→supervisor, app-implementer→frontend, api-implementer→backend.
Add qa template. Add belayer_tools field to all agent.yaml files.

Old names reflected v6 design; new names match what nightshift runs
actually spawn."
```

---

### Task 2: Daemon reads belayer_tools from agent.yaml and passes to bridge

**Files:**
- Modify: `internal/bridge/bridge.go:31-49` (Config struct)
- Modify: `internal/daemon/agents.go:290-320` (bridgeLaunchAgent)

- [ ] **Step 1: Add BelayerTools field to bridge.Config**

In `internal/bridge/bridge.go`, add a field to the `Config` struct:

```go
type Config struct {
	SessionID       string
	AgentID         string
	Role            string
	Profile         string
	Workdir         string
	SocketPath      string
	RunDir          string
	Model           string
	Message         string
	SystemPrompt    string
	HermesSessionID string
	BelayerRoot     string
	Ephemeral       bool
	BelayerTools    []string // role-specific belayer tools from agent.yaml

	Cmd []string
}
```

- [ ] **Step 2: Pass BELAYER_TOOLS env var in bridge.Spawn**

In `internal/bridge/bridge.go`, in the `Spawn` function, after the existing env var block (after line 115), add:

```go
if len(cfg.BelayerTools) > 0 {
	env = appendEnv(env, "BELAYER_TOOLS", strings.Join(cfg.BelayerTools, ","))
}
```

Add `"strings"` to the import block.

- [ ] **Step 3: Read agent.yaml in daemon's bridgeLaunchAgent**

In `internal/daemon/agents.go`, in `bridgeLaunchAgent`, after the system prompt loading block (after line 304), add code to read `belayer_tools` from `agent.yaml`:

```go
// Load belayer_tools from templates/<name>/agent.yaml if it exists.
var belayerTools []string
for _, base := range []string{workdir, d.config.BelayerRoot} {
	if base == "" {
		continue
	}
	yamlPath := filepath.Join(base, "templates", req.Name, "agent.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		continue
	}
	// Simple line-based parse — avoid YAML library dependency.
	// Looks for "belayer_tools:" then collects "  - tool_name" lines.
	inTools := false
	for _, line := range splitLines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "belayer_tools:" || trimmed == "belayer_tools: []" {
			inTools = true
			if trimmed == "belayer_tools: []" {
				break // explicit empty list
			}
			continue
		}
		if inTools {
			if strings.HasPrefix(trimmed, "- ") {
				tool := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				belayerTools = append(belayerTools, tool)
			} else {
				break // end of list
			}
		}
	}
	log.Printf("Loaded belayer_tools from %s for agent %s: %v", yamlPath, req.Name, belayerTools)
	break
}
```

Then pass the tools to the bridge config (modify the existing `cfg := bridge.Config{...}` block):

```go
cfg := bridge.Config{
	// ... existing fields ...
	BelayerTools:    belayerTools,
}
```

- [ ] **Step 4: Run existing tests to verify no regressions**

```bash
go test ./internal/bridge/ -v
go test ./internal/daemon/ -v
```

Expected: All existing tests pass. No tests exercise `BelayerTools` yet — we add those in Task 4.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/bridge.go internal/daemon/agents.go
git commit -m "feat: daemon reads belayer_tools from agent.yaml and passes to bridge

The daemon reads the belayer_tools list from templates/<name>/agent.yaml
at spawn time and passes it to the bridge via BELAYER_TOOLS env var.
This is the Go side of role-based tool gating."
```

---

### Task 3: Bridge filters tool registration by allowed list

**Files:**
- Modify: `hermes_bridge/tools.py`
- Modify: `hermes_bridge/__main__.py:211-217`

- [ ] **Step 1: Add BASELINE_TOOLS and filtering to register_belayer_tools**

Replace the `register_belayer_tools` function and the `_HANDLER_FACTORIES` list in `hermes_bridge/tools.py`:

```python
BASELINE_TOOLS = {
    "belayer_send_message",
    "belayer_report_status",
    "belayer_create_artifact",
}

_HANDLER_FACTORIES = {
    "belayer_send_message": (SEND_MESSAGE_SCHEMA, make_send_message_handler),
    "belayer_report_status": (REPORT_STATUS_SCHEMA, make_report_status_handler),
    "belayer_create_artifact": (CREATE_ARTIFACT_SCHEMA, make_create_artifact_handler),
    "belayer_spawn_agent": (SPAWN_AGENT_SCHEMA, make_spawn_agent_handler),
    "belayer_request_completion": (REQUEST_COMPLETION_SCHEMA, make_request_completion_handler),
    "belayer_approve_completion": (APPROVE_COMPLETION_SCHEMA, make_approve_completion_handler),
    "belayer_reject_completion": (REJECT_COMPLETION_SCHEMA, make_reject_completion_handler),
}


def register_belayer_tools(agent, agent_id: str, session_id: str, socket_path: str, allowed_tools: list[str] | None = None) -> None:
    """Register Belayer coordination tools on an AIAgent instance.

    Baseline tools (send_message, report_status, create_artifact) are always
    registered. Additional tools are only registered if they appear in
    allowed_tools (read from BELAYER_TOOLS env var, set by the daemon from
    the agent template's agent.yaml).
    """
    try:
        from tools.registry import registry  # type: ignore[import]
    except ImportError as exc:
        raise RuntimeError(
            "Hermes 'tools' package not found. Ensure hermes-agent is installed."
        ) from exc

    tools_to_register = set(BASELINE_TOOLS)
    if allowed_tools:
        tools_to_register |= set(allowed_tools)

    registered = 0
    for tool_name, (schema, make_handler) in _HANDLER_FACTORIES.items():
        if tool_name not in tools_to_register:
            continue
        handler = make_handler(agent_id, session_id, socket_path)
        registry.register(
            name=schema["name"],
            toolset="belayer",
            schema=schema,
            handler=handler,
        )
        tool_def = {
            "type": "function",
            "function": {
                "name": schema["name"],
                "description": schema["description"],
                "parameters": schema["parameters"],
            },
        }
        agent.tools.append(tool_def)
        agent.valid_tool_names.add(schema["name"])
        registered += 1

    log.info(
        "Registered %d/%d Belayer tools for agent=%s session=%s (allowed: %s)",
        registered,
        len(_HANDLER_FACTORIES),
        agent_id,
        session_id,
        tools_to_register,
    )
```

Also remove the old `_ALL_SCHEMAS` list (line 200-204 in the original) — it's unused now.

- [ ] **Step 2: Read BELAYER_TOOLS in __main__.py and pass to registration**

In `hermes_bridge/__main__.py`, replace the tool registration block (lines 211-217):

```python
    # --- Register Belayer tools --------------------------------------------
    allowed_tools_env = os.environ.get("BELAYER_TOOLS", "")
    allowed_tools = [t.strip() for t in allowed_tools_env.split(",") if t.strip()] if allowed_tools_env else None
    try:
        register_belayer_tools(agent, agent_id, session_id, socket_path, allowed_tools=allowed_tools)
    except Exception as exc:
        log.error("Failed to register Belayer tools: %s", exc)
        post_event(socket_path, session_id, agent_id, "bridge:failed", {"error": str(exc)})
        sys.exit(1)
```

- [ ] **Step 3: Update module docstring in tools.py**

Replace the module docstring at the top of `hermes_bridge/tools.py`:

```python
"""Belayer coordination tools for Hermes agents.

Baseline tools (always registered on every agent):
  - belayer_send_message        — agent-to-agent messaging via session bus
  - belayer_report_status       — publish agent status events (working/blocked/done)
  - belayer_create_artifact     — register a durable output with the artifact registry

Role-specific tools (only registered when declared in agent.yaml):
  - belayer_spawn_agent         — supervisor spawns specialists into the session
  - belayer_request_completion  — supervisor signals "work is done, verify before closing"
  - belayer_approve_completion  — PM approves the run after spec verification
  - belayer_reject_completion   — PM rejects the run with a gap list for remediation

Tool schemas follow the OpenAI function-calling format used by Hermes.
Handlers receive kwargs matching schema property names (Hermes calling convention).
"""
```

- [ ] **Step 4: Commit**

```bash
git add hermes_bridge/tools.py hermes_bridge/__main__.py
git commit -m "feat: bridge filters tool registration by allowed list

register_belayer_tools now takes an allowed_tools parameter. Baseline
tools (send_message, report_status, create_artifact) are always
registered. Role-specific tools are only registered when the agent's
template declares them via belayer_tools in agent.yaml.

This prevents the supervisor from self-approving (it never sees
belayer_approve_completion) and prevents specialists from spawning
agents or requesting completion."
```

---

### Task 4: Add BELAYER_TOOLS to bridge env var test

**Files:**
- Modify: `internal/bridge/bridge_test.go:252-295`

- [ ] **Step 1: Add test for BELAYER_TOOLS env var injection**

Add to `internal/bridge/bridge_test.go`:

```go
// TestBelayerToolsEnvVarInjected verifies that BelayerTools are passed as
// a comma-separated BELAYER_TOOLS env var.
func TestBelayerToolsEnvVarInjected(t *testing.T) {
	cfg := Config{
		SessionID:    "sess-abc",
		AgentID:      "supervisor",
		Role:         "supervisor",
		Profile:      "default",
		Workdir:      t.TempDir(),
		SocketPath:   "/tmp/test.sock",
		RunDir:       t.TempDir(),
		BelayerTools: []string{"belayer_spawn_agent", "belayer_request_completion"},
		Cmd:          []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	expected := "BELAYER_TOOLS=belayer_spawn_agent,belayer_request_completion"
	if !strings.Contains(output, expected) {
		t.Errorf("expected %q in env output\ngot:\n%s", expected, output)
	}
}

// TestBelayerToolsEnvVarOmittedWhenEmpty verifies that BELAYER_TOOLS is not
// set when the tool list is empty (baseline-only agents).
func TestBelayerToolsEnvVarOmittedWhenEmpty(t *testing.T) {
	cfg := Config{
		SessionID:    "sess-abc",
		AgentID:      "worker",
		Role:         "implementer",
		Profile:      "default",
		Workdir:      t.TempDir(),
		SocketPath:   "/tmp/test.sock",
		RunDir:       t.TempDir(),
		BelayerTools: nil,
		Cmd:          []string{"env"},
	}

	p, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	<-p.Done()

	logData, err := os.ReadFile(cfg.RunDir + "/bridge-stdout.log")
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	output := string(logData)

	if strings.Contains(output, "BELAYER_TOOLS=") {
		t.Errorf("expected BELAYER_TOOLS to be absent from env, but found it in output")
	}
}
```

- [ ] **Step 2: Run the bridge tests**

```bash
go test ./internal/bridge/ -v -run TestBelayerTools
```

Expected: Both new tests pass.

- [ ] **Step 3: Run full test suite**

```bash
go test ./internal/bridge/ -v
```

Expected: All tests pass including the new ones.

- [ ] **Step 4: Commit**

```bash
git add internal/bridge/bridge_test.go
git commit -m "test: verify BELAYER_TOOLS env var injection in bridge"
```

---

### Task 5: Rename planner to supervisor in daemon and CLI

**Files:**
- Modify: `internal/daemon/agents.go:197,240,242,244,282-288,357,363,367`
- Modify: `internal/daemon/bridge_events.go:60,64,70,85,92,97,104,108,139`
- Modify: `internal/daemon/bridge_events.go:225,226,238,320,328,332,333,358`
- Modify: `internal/cli/run.go:22,25,30-31,75,78,82-83,93,100`
- Modify: `internal/daemon/bridge_events_test.go` (all `"planner"` references)
- Modify: `internal/bridge/bridge_test.go:113,133` (interrupt test uses `"planner"`)

- [ ] **Step 1: Rename in daemon/agents.go**

Replace all occurrences of `"planner"` with `"supervisor"` in `internal/daemon/agents.go`:

Line 197: `if name == "planner"` → `if name == "supervisor"`
Line 240: `if name != "planner"` → `if name != "supervisor"`
Line 242: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 244: `Interrupt(sessionID, "planner"` → `Interrupt(sessionID, "supervisor"`
Line 246: `Send(sessionID, "planner"` → `Send(sessionID, "supervisor"`
Line 286: `req.Role == "planner"` → `req.Role == "supervisor"`
Line 282 (comment): `Planners stay alive` → `Supervisors stay alive`
Line 357: `run.Name != "planner"` → `run.Name != "supervisor"`
Line 363: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 367: `Interrupt(run.SessionID, "planner"` → `Interrupt(run.SessionID, "supervisor"`

- [ ] **Step 2: Rename in daemon/bridge_events.go**

Replace all occurrences of `"planner"` with `"supervisor"` in `internal/daemon/bridge_events.go`:

Line 60 (comment): `message to the planner` → `message to the supervisor`
Line 64 (comment): `messages that the planner dismisses` → `messages that the supervisor dismisses`
Line 70: `agentName == "planner"` → `agentName == "supervisor"`
Line 85: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 92 (comment): `bridge-based planners` → `bridge-based supervisors`
Line 97: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 104: `Interrupt(sessionID, "planner"` → `Interrupt(sessionID, "supervisor"`
Line 108: `agentName == "planner"` → `agentName == "supervisor"`
Line 139: `Send(sessionID, "planner"` → `Send(sessionID, "supervisor"`

In `handleBridgeCompletionRequested` (line 180): `The planner has signaled` → `The supervisor has signaled`
Line 225: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 226: `You may call belayer_request_completion` stays as-is (tool name unchanged)
Line 233: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 238: `Interrupt(sessionID, "planner"` → `Interrupt(sessionID, "supervisor"`
Line 320: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 328-332: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 333: `Interrupt(sessionID, "planner"` → `Interrupt(sessionID, "supervisor"`
Line 339 (comment): `gap list to planner` → `gap list to supervisor`
Line 344: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 353: `RecipientID: "planner"` → `RecipientID: "supervisor"`
Line 358: `Interrupt(sessionID, "planner"` → `Interrupt(sessionID, "supervisor"`
Line 360 (log): `gap list sent to planner` → `gap list sent to supervisor`

- [ ] **Step 3: Rename in CLI run.go**

In `internal/cli/run.go`:

Line 22: `plannerProfile` → `supervisorProfile`
Line 25: `Short: "Create a run, spawn planner` → `Short: "Create a run, spawn supervisor`
Line 30-31: `plannerProfile` checks → `supervisorProfile`
Line 59: `plannerWorkdir` → `supervisorWorkdir`
Line 66: `plannerWorkdir` → `supervisorWorkdir`
Line 75: `Name: "planner", Role: "planner", Profile: plannerProfile, Workdir: plannerWorkdir` → `Name: "supervisor", Role: "supervisor", Profile: supervisorProfile, Workdir: supervisorWorkdir`
Line 78: `SendMessage(sess.ID, "planner"` → `SendMessage(sess.ID, "supervisor"`
Line 83: `"Planner: planner"` → `"Supervisor: supervisor"`
Line 100: `--planner-profile` → `--supervisor-profile`

- [ ] **Step 4: Rename in bridge_events_test.go**

In `internal/daemon/bridge_events_test.go`, replace all `"planner"` with `"supervisor"`:

Line 53: `setupSessionWithAgents(t, d, "worker", "planner")` → `setupSessionWithAgents(t, d, "worker", "supervisor")`
Line 77: same
Line 102: same
Line 131: same
Line 140: `PendingMessages(sessionID, "planner"` → `PendingMessages(sessionID, "supervisor"`
Line 149: `m.RecipientID == "planner"` → `m.RecipientID == "supervisor"`
Line 162-163: `setupSessionWithAgents(t, d, "planner")`, `postBridgeEvent(... "planner" ...)` → `"supervisor"`
Line 170: `PendingMessages(sessionID, "planner"` → `PendingMessages(sessionID, "supervisor"`
Line 179: `GetAgentRun(sessionID, "planner")` → `GetAgentRun(sessionID, "supervisor")`
Line 244: same
Line 249: `PendingMessages(sessionID, "planner"` → `PendingMessages(sessionID, "supervisor"`
Line 258: `m.RecipientID == "planner"` → `m.RecipientID == "supervisor"`

- [ ] **Step 5: Rename in bridge_test.go**

In `internal/bridge/bridge_test.go`:

Line 113: `p.Interrupt("planner", ...)` → `p.Interrupt("supervisor", ...)`
Line 133: `decoded["from"] != "planner"` → `decoded["from"] != "supervisor"`

- [ ] **Step 6: Run all tests**

```bash
go test ./... -v
```

Expected: All tests pass with the renamed references.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/agents.go internal/daemon/bridge_events.go internal/cli/run.go internal/daemon/bridge_events_test.go internal/bridge/bridge_test.go
git commit -m "refactor: rename planner to supervisor throughout daemon and CLI

The orchestrating agent is now called 'supervisor' to better reflect
its role. All hardcoded 'planner' references in the daemon, CLI, and
tests are updated. The --planner-profile flag becomes --supervisor-profile."
```

---

### Task 6: Improve belayer_request_completion description

**Files:**
- Modify: `hermes_bridge/tools.py:134-156`

- [ ] **Step 1: Replace the REQUEST_COMPLETION_SCHEMA description**

In `hermes_bridge/tools.py`, replace `REQUEST_COMPLETION_SCHEMA`:

```python
REQUEST_COMPLETION_SCHEMA = {
    "name": "belayer_request_completion",
    "description": (
        "Signal that the run is complete and ready to close. This is a terminal "
        "workflow action — it ends the active work phase and spawns the PM agent "
        "for adversarial spec-vs-reality verification. The PM will independently "
        "verify every spec item against the code and either approve (closing the "
        "session) or reject (returning gaps for remediation). "
        "Do NOT call this until: all specialist agents have reported done, all "
        "implementation branches have been merged or are ready, you have independently "
        "verified the work (tests pass, builds succeed), and review/QA feedback has "
        "been addressed. Once called, you must wait for the PM's verdict. You cannot "
        "approve or reject the run yourself."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "summary": {
                "type": "string",
                "description": "Summary of what was accomplished, which spec items were implemented, and any deviations from the spec",
            },
            "spec_artifact": {
                "type": "string",
                "description": "Path to the spec/design-doc artifact (optional, PM will search if not provided)",
            },
        },
        "required": ["summary"],
    },
}
```

- [ ] **Step 2: Commit**

```bash
git add hermes_bridge/tools.py
git commit -m "fix: improve belayer_request_completion description to convey finality

The old description was too casual about what this tool does. The new
description makes clear this is a terminal workflow action that ends
the active work phase and that the supervisor cannot self-approve."
```

---

### Task 7: Update AGENT_ARCHITECTURE.md and PM system prompt

**Files:**
- Modify: `docs/AGENT_ARCHITECTURE.md`
- Modify: `templates/pm/system-prompt.md`

- [ ] **Step 1: Update tool gating section in AGENT_ARCHITECTURE.md**

In `docs/AGENT_ARCHITECTURE.md`, replace line 70:

```
All seven Belayer tools are registered on every agent. Which tools an agent actually uses is governed by its soul, not tool gating. The planner's soul says to use `belayer_request_completion` instead of finishing directly. The PM's soul says to use `belayer_approve_completion` or `belayer_reject_completion` after verification.
```

With:

```
Each agent template declares which belayer tools it receives via the `belayer_tools` field in `agent.yaml`. Three baseline tools (send_message, report_status, create_artifact) are always registered. Role-specific tools are only available to agents whose templates declare them. The supervisor can spawn agents and request completion. Only the PM can approve or reject a run. This is enforced at registration time — agents never see tools they aren't authorized to use.
```

Also update all references to `planner` in the document to `supervisor`.

- [ ] **Step 2: Update PM system prompt to reference supervisor instead of planner**

In `templates/pm/system-prompt.md`, replace `planner` references:

```
The planner and specialists have already said "done."
```
→
```
The supervisor and specialists have already said "done."
```

```
The planner needs actionable information to fix it
```
→
```
The supervisor needs actionable information to fix it
```

- [ ] **Step 3: Update CLAUDE.md if it references planner**

Check `CLAUDE.md` for any `planner` references and update to `supervisor`.

- [ ] **Step 4: Commit**

```bash
git add docs/AGENT_ARCHITECTURE.md templates/pm/system-prompt.md CLAUDE.md
git commit -m "docs: update architecture docs and prompts for supervisor rename and tool gating

Replace soul-based tool gating description with the new explicit
template-based model. Update all planner→supervisor references in
docs and system prompts."
```

---

### Task 8: Run full test suite and verify

- [ ] **Step 1: Run Go tests**

```bash
go test ./... -v
```

Expected: All tests pass.

- [ ] **Step 2: Verify belayer --help shows updated text**

```bash
go run ./cmd/belayer --help
go run ./cmd/belayer run start --help
```

Expected: No references to `planner` in help text. `--supervisor-profile` flag visible.

- [ ] **Step 3: Verify template structure**

```bash
for d in templates/*/; do
  name=$(basename "$d")
  echo "=== $name ==="
  grep "belayer_tools" "$d/agent.yaml" -A 5
  echo
done
```

Expected: Each template shows its declared tools.

- [ ] **Step 4: Grep for any remaining planner references**

```bash
grep -r '"planner"' internal/ --include='*.go' | grep -v _test.go | grep -v '.claude/'
grep -r '"planner"' hermes_bridge/ --include='*.py'
grep -r 'planner' templates/ --include='*.yaml'
```

Expected: No matches (all renamed to supervisor).

- [ ] **Step 5: Commit if any fixups needed, otherwise done**

```bash
# Only if fixups were needed:
git add -A
git commit -m "fix: clean up remaining planner references"
```
