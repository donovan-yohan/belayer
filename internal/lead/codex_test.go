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
