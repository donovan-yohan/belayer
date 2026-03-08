package defaults

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteToDir(t *testing.T) {
	dir := t.TempDir()
	err := WriteToDir(dir)
	require.NoError(t, err)

	// Verify belayer.toml written
	data, err := os.ReadFile(filepath.Join(dir, "belayer.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "[agents]")

	// Verify prompts written
	data, err = os.ReadFile(filepath.Join(dir, "prompts", "lead.md"))
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify profiles written
	data, err = os.ReadFile(filepath.Join(dir, "profiles", "frontend.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "build")
}

func TestWriteToDir_PreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write defaults first
	err := WriteToDir(dir)
	require.NoError(t, err)

	// Overwrite belayer.toml with custom content
	custom := []byte("# my custom config")
	os.WriteFile(filepath.Join(dir, "belayer.toml"), custom, 0644)

	// Write defaults again
	err = WriteToDir(dir)
	require.NoError(t, err)

	// Custom content should be preserved
	data, err := os.ReadFile(filepath.Join(dir, "belayer.toml"))
	require.NoError(t, err)
	assert.Equal(t, "# my custom config", string(data))
}
