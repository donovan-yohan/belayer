package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTmux records tmux operations for testing.
type mockTmux struct {
	sessions map[string]bool
	windows  map[string][]string // session -> window names
	keys     map[string]string   // "session:window" -> sent keys
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions: make(map[string]bool),
		windows:  make(map[string][]string),
		keys:     make(map[string]string),
	}
}

func (m *mockTmux) HasSession(name string) bool         { return m.sessions[name] }
func (m *mockTmux) NewSession(name string) error         { m.sessions[name] = true; return nil }
func (m *mockTmux) KillSession(name string) error        { delete(m.sessions, name); return nil }
func (m *mockTmux) NewWindow(session, name string) error {
	m.windows[session] = append(m.windows[session], name)
	return nil
}
func (m *mockTmux) KillWindow(session, name string) error { return nil }
func (m *mockTmux) SendKeys(session, window, keys string) error {
	m.keys[session+":"+window] = keys
	return nil
}
func (m *mockTmux) ListWindows(session string) ([]string, error) { return m.windows[session], nil }
func (m *mockTmux) PipePane(session, window, path string) error  { return nil }
func (m *mockTmux) SetRemainOnExit(session, window string, enabled bool) error { return nil }
func (m *mockTmux) IsPaneDead(session, window string) (bool, error) { return false, nil }
func (m *mockTmux) CapturePaneContent(session, window string, lines int) (string, error) {
	return "", nil
}
func (m *mockTmux) SetEnvironment(session, key, value string) error { return nil }
func (m *mockTmux) SendKeysLiteral(target, text string) error      { return nil }
func (m *mockTmux) SendKeysRaw(target, key string) error           { return nil }
func (m *mockTmux) GetPanePID(session, window string) (int, error) { return 0, nil }

func TestClaudeSessionSpawner_IncludesFinishCommand(t *testing.T) {
	tm := newMockTmux()
	spawner := NewClaudeSessionSpawner(tm)

	info, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName: "setter",
		TaskID:   "abc12345-def6-7890",
		WorkDir:  "/tmp/test-workdir",
	})
	require.NoError(t, err)
	assert.Equal(t, "belayer", info.TmuxSession)
	assert.Contains(t, info.WindowName, "setter")

	sentKeys := tm.keys["belayer:"+info.WindowName]
	assert.Contains(t, sentKeys, "claude --dangerously-skip-permissions")
	assert.Contains(t, sentKeys, "--append-system-prompt")
	assert.Contains(t, sentKeys, "belayer setter finish --task-id abc12345-def6-7890")
	assert.Contains(t, sentKeys, "belayer setter flare --task-id abc12345-def6-7890")
	assert.Contains(t, sentKeys, "belayer setter fail --task-id abc12345-def6-7890")
}

func TestClaudeSessionSpawner_CreatesSession(t *testing.T) {
	tm := newMockTmux()
	spawner := NewClaudeSessionSpawner(tm)

	_, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName: "lead",
		TaskID:   "xyz98765-abc1-2345",
		WorkDir:  "/tmp/test-workdir",
	})
	require.NoError(t, err)
	assert.True(t, tm.sessions["belayer"])
	assert.Len(t, tm.windows["belayer"], 1)
}

func TestClaudeSessionSpawner_ExistingSession(t *testing.T) {
	tm := newMockTmux()
	tm.sessions["belayer"] = true // Pre-existing session
	spawner := NewClaudeSessionSpawner(tm)

	_, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName: "setter",
		TaskID:   "abc12345-def6-7890",
		WorkDir:  "/tmp/test-workdir",
	})
	require.NoError(t, err)
	// Should not try to create a second session.
	assert.True(t, tm.sessions["belayer"])
}

func TestCodexSessionSpawner_PrependsInstructions(t *testing.T) {
	tm := newMockTmux()
	spawner := NewCodexSessionSpawner(tm)

	info, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName:    "lead",
		TaskID:      "abc12345-def6-7890",
		WorkDir:     "/tmp/test-workdir",
		ExtraPrompt: "Build the auth system",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["belayer:"+info.WindowName]
	assert.Contains(t, sentKeys, "codex --dangerously-bypass-approvals-and-sandbox")
	// Callback instructions should be in the prompt (not --append-system-prompt).
	assert.NotContains(t, sentKeys, "--append-system-prompt")
	assert.Contains(t, sentKeys, "belayer lead finish --task-id abc12345-def6-7890")
}

func TestBuildSystemPrompt(t *testing.T) {
	prompt := buildSystemPrompt("setter", "run-123")
	assert.Contains(t, prompt, "You are the setter")
	assert.Contains(t, prompt, "belayer setter finish --task-id run-123")
	assert.Contains(t, prompt, "belayer setter flare --task-id run-123")
	assert.Contains(t, prompt, "belayer setter fail --task-id run-123")
}

func TestClaudeSessionSpawner_WritesInputJSON(t *testing.T) {
	tm := newMockTmux()
	spawner := NewClaudeSessionSpawner(tm)
	dir := t.TempDir()

	_, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName:  "setter",
		TaskID:    "abc12345-def6-7890",
		WorkDir:   dir,
		InputJSON: json.RawMessage(`{"spec":"build auth"}`),
	})
	require.NoError(t, err)

	// Input file should be written.
	data, err := os.ReadFile(dir + "/.belayer/input.json")
	require.NoError(t, err)
	assert.Contains(t, string(data), "build auth")
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"simple", "'simple'"},
		{"it's here", "'it'\"'\"'s here'"},
		{"", "''"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("shellQuote(%q) = %q, want to contain %q", tt.input, got, tt.contains)
		}
	}
}

func TestClaudeSessionSpawner_WorkDirInCommand(t *testing.T) {
	tm := newMockTmux()
	spawner := NewClaudeSessionSpawner(tm)

	info, err := spawner.Spawn(context.Background(), SessionOpts{
		RoleName: "setter",
		TaskID:   "abc12345-def6-7890",
		WorkDir:  "/my/project/dir",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["belayer:"+info.WindowName]
	assert.Contains(t, sentKeys, "cd '/my/project/dir'")
}
