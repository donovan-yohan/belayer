// internal/mail/message_test.go
package mail

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Address
		wantErr bool
	}{
		{
			name:  "setter",
			input: "setter",
			want:  Address{Role: "setter"},
		},
		{
			name:  "lead",
			input: "task/abc123/lead/frontend/goal-1",
			want:  Address{Role: "lead", TaskID: "abc123", Repo: "frontend", GoalID: "goal-1"},
		},
		{
			name:  "spotter",
			input: "task/abc123/spotter/frontend/goal-1",
			want:  Address{Role: "spotter", TaskID: "abc123", Repo: "frontend", GoalID: "goal-1"},
		},
		{
			name:  "anchor",
			input: "task/abc123/anchor",
			want:  Address{Role: "anchor", TaskID: "abc123"},
		},
		{
			name:    "invalid empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "task/abc123/unknown/x/y",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAddress_String(t *testing.T) {
	addr := Address{Role: "lead", TaskID: "abc", Repo: "frontend", GoalID: "g1"}
	assert.Equal(t, "task/abc/lead/frontend/g1", addr.String())

	addr2 := Address{Role: "setter"}
	assert.Equal(t, "setter", addr2.String())
}

func TestAddress_TmuxTarget(t *testing.T) {
	tests := []struct {
		name       string
		addr       Address
		wantSess   string
		wantWindow string
	}{
		{
			name:       "setter",
			addr:       Address{Role: "setter"},
			wantSess:   "belayer-setter",
			wantWindow: "0",
		},
		{
			name:       "lead",
			addr:       Address{Role: "lead", TaskID: "abc", Repo: "frontend", GoalID: "g1"},
			wantSess:   "belayer-task-abc",
			wantWindow: "frontend-g1",
		},
		{
			name:       "anchor",
			addr:       Address{Role: "anchor", TaskID: "abc"},
			wantSess:   "belayer-task-abc",
			wantWindow: "anchor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, win := tt.addr.TmuxTarget()
			assert.Equal(t, tt.wantSess, sess)
			assert.Equal(t, tt.wantWindow, win)
		})
	}
}

func TestMessageType_Valid(t *testing.T) {
	assert.True(t, MessageTypeDone.Valid())
	assert.True(t, MessageTypeFeedback.Valid())
	assert.False(t, MessageType("bogus").Valid())
}
