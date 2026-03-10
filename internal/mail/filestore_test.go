package mail

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestFileStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

func TestFileStore_CreateAndList(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Test subject", "Test body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "Test subject", issues[0].Title)
	assert.Contains(t, issues[0].Description, "Test body")
}

func TestFileStore_Close(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Msg to close", "Body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	err = store.Close("setter", issues[0].ID)
	require.NoError(t, err)

	// Unread should be empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)

	// File should exist in read/
	readDir := filepath.Join(store.dir, "setter", "read")
	entries, err := os.ReadDir(readDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestFileStore_ListEmpty(t *testing.T) {
	store := setupTestFileStore(t)

	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestFileStore_NestedAddress(t *testing.T) {
	store := setupTestFileStore(t)

	err := store.Create("Goal", "Do the thing", map[string]string{
		"to":       "task/t1/lead/api/g1",
		"msg-type": "goal_assignment",
	})
	require.NoError(t, err)

	issues, err := store.List("task/t1/lead/api/g1")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Different address should be empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestFileStore_CreateMissingTo(t *testing.T) {
	store := setupTestFileStore(t)
	err := store.Create("Subject", "Body", map[string]string{"msg-type": "done"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'to' label")
}

func TestFileStore_CloseNonexistent(t *testing.T) {
	store := setupTestFileStore(t)
	err := store.Close("setter", "does-not-exist.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFileStore_ListSkipsMalformed(t *testing.T) {
	store := setupTestFileStore(t)

	// Create a valid message
	require.NoError(t, store.Create("Valid", "Body", map[string]string{"to": "setter", "msg-type": "done"}))

	// Write a malformed JSON file directly
	unreadDir := filepath.Join(store.dir, "setter", "unread")
	require.NoError(t, os.WriteFile(filepath.Join(unreadDir, "bad.json"), []byte("not json"), 0o644))

	// List should return only the valid message
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	assert.Equal(t, "Valid", issues[0].Title)
}

func TestFileStore_MultipleMessages(t *testing.T) {
	store := setupTestFileStore(t)

	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 3", "Body 3", map[string]string{"to": "task/x/lead/api/g1", "msg-type": "feedback"}))

	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	issues, err = store.List("task/x/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
}
