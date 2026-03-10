// internal/mail/integration_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SendAndRead(t *testing.T) {
	store := setupTestBeads(t)

	// Send a feedback message (no tmux delivery in this test)
	err := store.Create(
		DefaultSubject(MessageTypeFeedback),
		mustRender(t, MessageTypeFeedback, "Login form missing validation"),
		map[string]string{
			"to":       "task/abc/lead/api/g1",
			"from":     "setter",
			"msg-type": string(MessageTypeFeedback),
		},
	)
	require.NoError(t, err)

	// Read inbox
	issues, err := store.List("task/abc/lead/api/g1")
	require.NoError(t, err)
	require.Len(t, issues, 1)

	// Verify rendered content
	assert.Contains(t, issues[0].Description, "FEEDBACK FROM SPOTTER")
	assert.Contains(t, issues[0].Description, "Login form missing validation")
	assert.Contains(t, issues[0].Description, "belayer message setter --type done")

	// Close (mark read)
	require.NoError(t, store.Close(issues[0].ID))

	// Inbox should be empty
	issues, err = store.List("task/abc/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 0)
}

func TestIntegration_MultipleMessages(t *testing.T) {
	store := setupTestBeads(t)

	// Send two messages to same recipient
	require.NoError(t, store.Create("Msg 1", "Body 1", map[string]string{"to": "setter", "msg-type": "done"}))
	require.NoError(t, store.Create("Msg 2", "Body 2", map[string]string{"to": "setter", "msg-type": "done"}))

	// Send one to different recipient
	require.NoError(t, store.Create("Msg 3", "Body 3", map[string]string{"to": "task/x/lead/api/g1", "msg-type": "feedback"}))

	// Setter should see 2
	issues, err := store.List("setter")
	require.NoError(t, err)
	assert.Len(t, issues, 2)

	// Lead should see 1
	issues, err = store.List("task/x/lead/api/g1")
	require.NoError(t, err)
	assert.Len(t, issues, 1)
}

func TestIntegration_DoneSignal(t *testing.T) {
	store := setupTestBeads(t)

	// Lead sends done signal to setter
	doneBody := `{"status":"complete","summary":"Added login validation"}`
	rendered := mustRender(t, MessageTypeDone, doneBody)
	require.NoError(t, store.Create(
		DefaultSubject(MessageTypeDone),
		rendered,
		map[string]string{
			"to":       "setter",
			"from":     "task/abc/lead/api/g1",
			"msg-type": string(MessageTypeDone),
		},
	))

	// Setter reads it
	issues, err := store.List("setter")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Description, "Added login validation")
}

func mustRender(t *testing.T, mt MessageType, body string) string {
	t.Helper()
	rendered, err := RenderTemplate(mt, body)
	require.NoError(t, err)
	return rendered
}
