// internal/mail/templates_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name     string
		msgType  MessageType
		body     string
		wantSub  string // substring that should appear in output
	}{
		{
			name:    "feedback",
			msgType: MessageTypeFeedback,
			body:    "Login form missing validation",
			wantSub: "Login form missing validation",
		},
		{
			name:    "goal_assignment",
			msgType: MessageTypeGoalAssignment,
			body:    "Implement dark mode",
			wantSub: "Implement dark mode",
		},
		{
			name:    "done",
			msgType: MessageTypeDone,
			body:    `{"status":"complete"}`,
			wantSub: `{"status":"complete"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := RenderTemplate(tt.msgType, tt.body)
			require.NoError(t, err)
			assert.Contains(t, rendered, tt.wantSub)
		})
	}
}

func TestRenderTemplate_InvalidType(t *testing.T) {
	_, err := RenderTemplate(MessageType("bogus"), "body")
	assert.Error(t, err)
}

func TestDefaultSubject(t *testing.T) {
	assert.Equal(t, "Goal Assignment", DefaultSubject(MessageTypeGoalAssignment))
	assert.Equal(t, "Goal Complete", DefaultSubject(MessageTypeDone))
	assert.Equal(t, "Feedback", DefaultSubject(MessageTypeFeedback))
}
