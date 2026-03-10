// internal/mail/read_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatMessages(t *testing.T) {
	msgs := []MailMessage{
		{ID: "mail-abc", Title: "Feedback", Description: "Fix the bug"},
		{ID: "mail-def", Title: "Goal Assignment", Description: "Add dark mode"},
	}

	output := FormatMessages(msgs)
	assert.Contains(t, output, "Fix the bug")
	assert.Contains(t, output, "Add dark mode")
	assert.Contains(t, output, "mail-abc")
	assert.Contains(t, output, "mail-def")
}

func TestReadAndClose(t *testing.T) {
	store := setupTestFileStore(t)

	// Create two messages
	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "feedback"}))

	// Read should return both
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	// Close them
	for _, issue := range issues {
		require.NoError(t, store.Close(issue.ID))
	}

	// List again — empty
	issues, err = store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}
