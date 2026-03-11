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
