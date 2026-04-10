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
