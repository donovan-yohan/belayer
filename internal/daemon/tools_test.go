package daemon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/store"
)

// createTestSession creates a session and returns its ID.
func createTestSession(t *testing.T, d *Daemon) string {
	t.Helper()
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "test-session",
		Template: "implement",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, body=%s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[store.Session](t, rr)
	return sess.ID
}

// --- Tool registration ---

func TestRegisterTool_Success(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	tool := agent.ToolSpec{
		Name:        "echo-tool",
		Description: "Echoes a message",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "echo {{.msg}}",
			Timeout: 10,
		},
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", tool)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register tool: got %d, body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[map[string]string](t, rr)
	if resp["status"] != "registered" {
		t.Errorf("status = %q, want registered", resp["status"])
	}
	if resp["tool"] != "echo-tool" {
		t.Errorf("tool = %q, want echo-tool", resp["tool"])
	}
}

func TestRegisterTool_SessionNotFound(t *testing.T) {
	d := testDaemon(t)
	tool := agent.ToolSpec{
		Name: "echo-tool",
		Exec: agent.ToolExec{Target: "host", Command: "echo hi"},
	}
	rr := doRequest(t, d, "POST", "/sessions/nonexistent/tools", tool)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestRegisterTool_MissingName(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)
	tool := agent.ToolSpec{
		Exec: agent.ToolExec{Target: "host", Command: "echo hi"},
	}
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", tool)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRegisterTool_InvalidTarget(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)
	tool := agent.ToolSpec{
		Name: "bad-tool",
		Exec: agent.ToolExec{Target: "nowhere", Command: "echo hi"},
	}
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", tool)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRegisterTool_Idempotent(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	tool := agent.ToolSpec{
		Name:        "echo-tool",
		Description: "First version",
		Exec:        agent.ToolExec{Target: "host", Command: "echo v1"},
	}
	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", tool)

	// Update with new description.
	tool.Description = "Second version"
	tool.Exec.Command = "echo v2"
	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", tool)

	// Should have exactly one tool.
	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tools", nil)
	tools := decodeJSON[[]agent.ToolSpec](t, rr)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Description != "Second version" {
		t.Errorf("description = %q, want 'Second version'", tools[0].Description)
	}
}

// --- Tool listing ---

func TestListTools_Empty(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tools", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	tools := decodeJSON[[]agent.ToolSpec](t, rr)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestListTools_WithTools(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	for _, name := range []string{"tool-a", "tool-b"} {
		doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
			Name: name,
			Exec: agent.ToolExec{Target: "host", Command: "echo " + name},
		})
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tools", nil)
	tools := decodeJSON[[]agent.ToolSpec](t, rr)
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestListTools_SessionNotFound(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "GET", "/sessions/nonexistent/tools", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Tool execution ---

func TestExecuteTool_HostEcho(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	// Register a host tool.
	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "echo-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "echo {{.msg}}",
			Timeout: 5,
		},
	})

	// Execute it.
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/echo-tool",
		map[string]string{"msg": "hello world"},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("execute tool: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var result struct {
		Output     string `json:"output"`
		ExitCode   int    `json:"exit_code"`
		DurationMS int64  `json:"duration_ms"`
		Target     string `json:"target"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", result.ExitCode)
	}
	if result.Target != "host" {
		t.Errorf("target = %q, want host", result.Target)
	}
	// echo 'hello world' outputs "hello world\n"
	if result.Output != "hello world\n" {
		t.Errorf("output = %q, want %q", result.Output, "hello world\n")
	}
}

func TestExecuteTool_InjectionPrevented(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "echo-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "echo {{.msg}}",
			Timeout: 5,
		},
	})

	// Attempt command injection via the input.
	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/echo-tool",
		map[string]string{"msg": "safe; echo INJECTED"},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("execute tool: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var result struct {
		Output string `json:"output"`
	}
	json.NewDecoder(rr.Body).Decode(&result)

	// The injection must NOT have run a second command.
	if result.Output == "safe\nINJECTED\n" {
		t.Errorf("INJECTION DETECTED: output = %q", result.Output)
	}
	// The output should be the literal string including the semicolon.
	want := "safe; echo INJECTED\n"
	if result.Output != want {
		t.Errorf("output = %q, want %q", result.Output, want)
	}
}

func TestExecuteTool_ToolNotFound(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/nonexistent",
		map[string]string{},
	)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestExecuteTool_SessionNotFound(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions/nonexistent/tools/foo",
		map[string]string{},
	)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestExecuteTool_LogsEvent(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "echo-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "echo {{.msg}}",
			Timeout: 5,
		},
	})

	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/echo-tool",
		map[string]string{"msg": "test"},
	)

	// Check that a tool_executed event was logged.
	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/events", nil)
	events := decodeJSON[[]store.SessionEvent](t, rr)

	found := false
	for _, e := range events {
		if e.Type == "tool_executed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool_executed event to be logged")
	}
}

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

func TestExecuteTool_ExitCode(t *testing.T) {
	d := testDaemon(t)
	sessID := createTestSession(t, d)

	doRequest(t, d, "POST", "/sessions/"+sessID+"/tools", agent.ToolSpec{
		Name: "fail-tool",
		Exec: agent.ToolExec{
			Target:  "host",
			Command: "exit 2",
			Timeout: 5,
		},
	})

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/tools/fail-tool",
		map[string]string{},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("execute tool: got %d", rr.Code)
	}
	var result struct {
		ExitCode int `json:"exit_code"`
	}
	json.NewDecoder(rr.Body).Decode(&result)
	if result.ExitCode != 2 {
		t.Errorf("exit_code = %d, want 2", result.ExitCode)
	}
}
