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

func TestNewSpawnerSet_AllDefault(t *testing.T) {
	tm := newMockTmux()
	ss, err := NewSpawnerSet(SpawnerSetConfig{DefaultProvider: "claude"}, tm)
	require.NoError(t, err)
	assert.IsType(t, &ClaudeSpawner{}, ss.Lead)
	assert.IsType(t, &ClaudeSpawner{}, ss.Spotter)
	assert.IsType(t, &ClaudeSpawner{}, ss.Anchor)
}

func TestNewSpawnerSet_PerRoleOverrides(t *testing.T) {
	tm := newMockTmux()
	ss, err := NewSpawnerSet(SpawnerSetConfig{
		DefaultProvider: "claude",
		LeadProvider:    "codex",
	}, tm)
	require.NoError(t, err)
	assert.IsType(t, &CodexSpawner{}, ss.Lead)
	assert.IsType(t, &ClaudeSpawner{}, ss.Spotter)
	assert.IsType(t, &ClaudeSpawner{}, ss.Anchor)
}

func TestNewSpawnerSet_AllDifferent(t *testing.T) {
	tm := newMockTmux()
	ss, err := NewSpawnerSet(SpawnerSetConfig{
		DefaultProvider: "claude",
		LeadProvider:    "codex",
		SpotterProvider: "codex",
		AnchorProvider:  "claude",
	}, tm)
	require.NoError(t, err)
	assert.IsType(t, &CodexSpawner{}, ss.Lead)
	assert.IsType(t, &CodexSpawner{}, ss.Spotter)
	assert.IsType(t, &ClaudeSpawner{}, ss.Anchor)
}

func TestNewSpawnerSet_InvalidRoleProvider(t *testing.T) {
	tm := newMockTmux()
	ss, err := NewSpawnerSet(SpawnerSetConfig{
		DefaultProvider: "claude",
		SpotterProvider: "gemini",
	}, tm)
	assert.Nil(t, ss)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "spotter")
	assert.Contains(t, err.Error(), "gemini")
}

func TestNewSpawnerSet_InvalidDefault(t *testing.T) {
	tm := newMockTmux()
	ss, err := NewSpawnerSet(SpawnerSetConfig{DefaultProvider: "gemini"}, tm)
	assert.Nil(t, ss)
	assert.Error(t, err)
}
