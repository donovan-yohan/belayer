package pidfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, Write(path, 12345))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "12345\n", string(data))
}

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("42\n"), 0o644))

	pid, err := Read(path)
	require.NoError(t, err)
	assert.Equal(t, 42, pid)
}

func TestReadMissing(t *testing.T) {
	_, err := Read("/nonexistent/path.pid")
	assert.Error(t, err)
}

func TestRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("1\n"), 0o644))
	Remove(path)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestIsRunning_OwnProcess(t *testing.T) {
	assert.True(t, IsRunning(os.Getpid()))
}

func TestIsRunning_DeadProcess(t *testing.T) {
	assert.False(t, IsRunning(99999999))
}

func TestCheck_NoFile(t *testing.T) {
	pid, running := Check("/nonexistent.pid")
	assert.Equal(t, 0, pid)
	assert.False(t, running)
}

func TestCheck_StaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("99999999\n"), 0o644))

	pid, running := Check(path)
	assert.Equal(t, 99999999, pid)
	assert.False(t, running)
}
