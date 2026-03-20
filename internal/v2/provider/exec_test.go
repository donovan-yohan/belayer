package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v2/role"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMockScript creates a temporary executable script and returns its path.
func writeMockScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte("#!/bin/bash\n"+content), 0o755)
	require.NoError(t, err)
	return path
}

func TestExecProvider_ValidJSONOutput(t *testing.T) {
	// Script reads stdin and wraps it.
	script := writeMockScript(t, "echo-role", `cat`)
	provider := &ExecProvider{}

	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: script},
	}

	input := json.RawMessage(`{"spec":"build auth"}`)
	output, err := provider.Execute(context.Background(), roleDef, input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"spec":"build auth"}`, string(output))
}

func TestExecProvider_CommandNotFound(t *testing.T) {
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: "/nonexistent/command"},
	}

	_, err := provider.Execute(context.Background(), roleDef, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec provider")
}

func TestExecProvider_NoCommandConfigured(t *testing.T) {
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{},
	}

	_, err := provider.Execute(context.Background(), roleDef, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command configured")
}

func TestExecProvider_NonZeroExit(t *testing.T) {
	script := writeMockScript(t, "fail-role", `echo "something broke" >&2; exit 1`)
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: script},
	}

	_, err := provider.Execute(context.Background(), roleDef, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something broke")
}

func TestExecProvider_InvalidJSONOutput(t *testing.T) {
	script := writeMockScript(t, "bad-json-role", `echo "not json"`)
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: script},
	}

	_, err := provider.Execute(context.Background(), roleDef, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
}

func TestExecProvider_EmptyOutput(t *testing.T) {
	script := writeMockScript(t, "empty-role", `true`) // No output.
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: script},
	}

	_, err := provider.Execute(context.Background(), roleDef, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no output")
}

func TestExecProvider_WithArgs(t *testing.T) {
	// Script echoes its arguments as JSON.
	script := writeMockScript(t, "args-role", `echo "{\"args\":\"$@\"}"`)
	provider := &ExecProvider{}
	roleDef := role.RoleDef{
		Name:     "test-role",
		Provider: role.ProviderConfig{Command: script, Args: []string{"--strict", "--verbose"}},
	}

	output, err := provider.Execute(context.Background(), roleDef, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "--strict --verbose")
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 10))
	assert.Equal(t, "0123456789...", truncate("0123456789extra", 10))
}
