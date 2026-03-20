package temporal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafetyTracker_MaxDepth(t *testing.T) {
	tracker := NewSafetyTracker(2, 50, false)

	assert.NoError(t, tracker.CanSpawnChild(0, nil))
	assert.NoError(t, tracker.CanSpawnChild(1, nil))
	err := tracker.CanSpawnChild(2, nil) // At max depth.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max child depth")
}

func TestSafetyTracker_GlobalBudget(t *testing.T) {
	tracker := NewSafetyTracker(10, 3, false)

	assert.NoError(t, tracker.CanSpawnChild(0, nil))
	assert.NoError(t, tracker.CanSpawnChild(0, nil))
	assert.NoError(t, tracker.CanSpawnChild(0, nil))
	err := tracker.CanSpawnChild(0, nil) // Budget exhausted.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "global child budget")
	assert.Equal(t, 3, tracker.ChildCount())
}

func TestSafetyTracker_Dedupe(t *testing.T) {
	tracker := NewSafetyTracker(10, 50, true)

	input := json.RawMessage(`{"spec":"build auth"}`)
	require.NoError(t, tracker.CanSpawnChild(0, input))

	// Same input again → duplicate.
	err := tracker.CanSpawnChild(0, input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestSafetyTracker_DedupeDisabled(t *testing.T) {
	tracker := NewSafetyTracker(10, 50, false)

	input := json.RawMessage(`{"spec":"build auth"}`)
	require.NoError(t, tracker.CanSpawnChild(0, input))
	assert.NoError(t, tracker.CanSpawnChild(0, input)) // No dedupe → allowed.
}

func TestSafetyTracker_DifferentInputsNotDuplicate(t *testing.T) {
	tracker := NewSafetyTracker(10, 50, true)

	require.NoError(t, tracker.CanSpawnChild(0, json.RawMessage(`{"a":1}`)))
	assert.NoError(t, tracker.CanSpawnChild(0, json.RawMessage(`{"a":2}`)))
}
