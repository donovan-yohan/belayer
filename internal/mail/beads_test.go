// internal/mail/beads_test.go
package mail

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not available")
	}
}

func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not available")
	}
}

func setupTestBeads(t *testing.T) *BeadsStore {
	t.Helper()
	skipIfNoBd(t)
	skipIfNoDolt(t)

	dir := t.TempDir()
	store, err := NewBeadsStore(dir, "test-mail")
	require.NoError(t, err)
	return store
}

func TestBeadsStore_Init(t *testing.T) {
	store := setupTestBeads(t)

	// .beads directory should exist
	_, err := os.Stat(filepath.Join(store.dir, ".beads"))
	assert.NoError(t, err)
}

func TestBeadsStore_CreateAndList(t *testing.T) {
	store := setupTestBeads(t)

	// Create a message
	err := store.Create("Test subject", "Test body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	// List messages for "setter"
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
	assert.Equal(t, "Test subject", issues[0].Title)
	assert.Contains(t, issues[0].Description, "Test body")
}

func TestBeadsStore_Close(t *testing.T) {
	store := setupTestBeads(t)

	// Create a message
	err := store.Create("Msg to close", "Body", map[string]string{
		"to":       "setter",
		"from":     "task/abc/lead/api/g1",
		"msg-type": "done",
	})
	require.NoError(t, err)

	// List to get the ID
	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Close it
	err = store.Close(issues[0].ID)
	require.NoError(t, err)

	// List again — should be empty (only open issues)
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}
