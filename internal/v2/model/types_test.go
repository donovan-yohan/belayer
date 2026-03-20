package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleSignal_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		signal RoleSignal
	}{
		{
			name: "finish with output",
			signal: RoleSignal{
				TaskID:  "run-123",
				Role:    "setter",
				Action:  SignalFinish,
				Output:  json.RawMessage(`{"spec":"build auth system"}`),
				Message: "",
			},
		},
		{
			name: "flare with message",
			signal: RoleSignal{
				TaskID:  "run-456",
				Role:    "lead",
				Action:  SignalFlare,
				Output:  nil,
				Message: "stuck on database migration",
			},
		},
		{
			name: "fail with message",
			signal: RoleSignal{
				TaskID:  "run-789",
				Role:    "lead",
				Action:  SignalFail,
				Output:  nil,
				Message: "cannot access repository",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.signal)
			require.NoError(t, err)

			var got RoleSignal
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)

			assert.Equal(t, tt.signal.TaskID, got.TaskID)
			assert.Equal(t, tt.signal.Role, got.Role)
			assert.Equal(t, tt.signal.Action, got.Action)
			assert.Equal(t, tt.signal.Message, got.Message)
			if tt.signal.Output != nil {
				assert.JSONEq(t, string(tt.signal.Output), string(got.Output))
			}
		})
	}
}

func TestSignalAction_Values(t *testing.T) {
	assert.Equal(t, SignalAction("finish"), SignalFinish)
	assert.Equal(t, SignalAction("flare"), SignalFlare)
	assert.Equal(t, SignalAction("fail"), SignalFail)
}

func TestRunStatus_Values(t *testing.T) {
	assert.Equal(t, RunStatus("active"), RunStatusActive)
	assert.Equal(t, RunStatus("completed"), RunStatusCompleted)
	assert.Equal(t, RunStatus("failed"), RunStatusFailed)
	assert.Equal(t, RunStatus("flared"), RunStatusFlared)
}
