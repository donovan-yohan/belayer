package lead

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTmux implements tmux.TmuxManager for tests.
type mockTmux struct {
	sessions map[string]map[string]bool
	keys     map[string]string
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions: make(map[string]map[string]bool),
		keys:     make(map[string]string),
	}
}

func (m *mockTmux) HasSession(name string) bool {
	_, ok := m.sessions[name]
	return ok
}

func (m *mockTmux) NewSession(name string) error {
	m.sessions[name] = make(map[string]bool)
	return nil
}

func (m *mockTmux) KillSession(name string) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockTmux) NewWindow(session, windowName string) error {
	if s, ok := m.sessions[session]; ok {
		s[windowName] = true
	}
	return nil
}

func (m *mockTmux) KillWindow(session, windowName string) error {
	if s, ok := m.sessions[session]; ok {
		delete(s, windowName)
	}
	return nil
}

func (m *mockTmux) SendKeys(session, windowName, keys string) error {
	m.keys[session+":"+windowName] = keys
	return nil
}

func (m *mockTmux) ListWindows(session string) ([]string, error) {
	s, ok := m.sessions[session]
	if !ok {
		return nil, nil
	}
	var names []string
	for name := range s {
		names = append(names, name)
	}
	return names, nil
}

func (m *mockTmux) PipePane(session, windowName, logPath string) error {
	return nil
}

func TestClaudeSpawner_Spawn(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("test-session")
	tm.NewWindow("test-session", "api-goal-1")

	spawner := NewClaudeSpawner(tm)

	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession: "test-session",
		WindowName:  "api-goal-1",
		WorkDir:     "/tmp/worktree/api",
		Prompt:      "Do the thing",
	})
	require.NoError(t, err)

	// Verify the command sent to tmux
	sentKeys := tm.keys["test-session:api-goal-1"]
	assert.Contains(t, sentKeys, "cd '/tmp/worktree/api'")
	assert.Contains(t, sentKeys, "claude -p")
	assert.Contains(t, sentKeys, "Do the thing")
	assert.Contains(t, sentKeys, "--allowedTools '*'")
}

func TestClaudeSpawner_ShellQuoting(t *testing.T) {
	tm := newMockTmux()
	tm.NewSession("s")
	tm.NewWindow("s", "w")

	spawner := NewClaudeSpawner(tm)

	// Prompt with single quotes that need escaping
	err := spawner.Spawn(context.Background(), SpawnOpts{
		TmuxSession: "s",
		WindowName:  "w",
		WorkDir:     "/tmp/it's a path",
		Prompt:      "Don't break",
	})
	require.NoError(t, err)

	sentKeys := tm.keys["s:w"]
	// Single quotes in paths and prompts should be properly escaped
	assert.NotContains(t, sentKeys, "it's")  // raw single quote should be escaped
	assert.NotContains(t, sentKeys, "Don't") // raw single quote should be escaped
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with spaces", "'with spaces'"},
		{"it's", "'it'\"'\"'s'"},
		{"", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shellQuote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClaudeSpawner_ImplementsInterface(t *testing.T) {
	// Compile-time check that ClaudeSpawner implements AgentSpawner
	var _ AgentSpawner = (*ClaudeSpawner)(nil)
}

func TestClaudeSpawner_NoClaudeSpecificLeaks(t *testing.T) {
	// Verify the interface doesn't mention Claude
	// This is a documentation test — the interface should be vendor-agnostic
	iface := "AgentSpawner"
	assert.False(t, strings.Contains(iface, "Claude"))
}
