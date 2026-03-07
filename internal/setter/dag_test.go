package setter

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDAG_Simple(t *testing.T) {
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)
	ready := dag.ReadyGoals()

	assert.Len(t, ready, 2)
	ids := readyIDs(ready)
	assert.Contains(t, ids, "a")
	assert.Contains(t, ids, "b")
}

func TestBuildDAG_Linear(t *testing.T) {
	// A -> B -> C (C depends on B, B depends on A)
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", DependsOn: []string{"a"}, Status: model.GoalStatusPending},
		{ID: "c", TaskID: "t1", DependsOn: []string{"b"}, Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)
	ready := dag.ReadyGoals()

	require.Len(t, ready, 1)
	assert.Equal(t, "a", ready[0].ID)
}

func TestBuildDAG_Diamond(t *testing.T) {
	// A -> B, A -> C, B+C -> D
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", DependsOn: []string{"a"}, Status: model.GoalStatusPending},
		{ID: "c", TaskID: "t1", DependsOn: []string{"a"}, Status: model.GoalStatusPending},
		{ID: "d", TaskID: "t1", DependsOn: []string{"b", "c"}, Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)

	// Only A should be ready initially.
	ready := dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "a", ready[0].ID)

	// After A completes, B and C should be ready.
	dag.MarkComplete("a")
	ready = dag.ReadyGoals()
	assert.Len(t, ready, 2)
	ids := readyIDs(ready)
	assert.Contains(t, ids, "b")
	assert.Contains(t, ids, "c")

	// After B and C complete, D should be ready.
	dag.MarkComplete("b")
	dag.MarkComplete("c")
	ready = dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "d", ready[0].ID)
}

func TestMarkComplete_UnblocksDependents(t *testing.T) {
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", DependsOn: []string{"a"}, Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)

	// B should not be ready yet.
	ready := dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "a", ready[0].ID)

	// Complete A; B should now be ready.
	dag.MarkComplete("a")
	ready = dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "b", ready[0].ID)
}

func TestAllComplete(t *testing.T) {
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)
	assert.False(t, dag.AllComplete())

	dag.MarkComplete("a")
	assert.False(t, dag.AllComplete())

	dag.MarkComplete("b")
	assert.True(t, dag.AllComplete())
}

func TestReadyGoals_SkipsRunning(t *testing.T) {
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)
	dag.MarkRunning("a")

	ready := dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "b", ready[0].ID)
}

func TestReadyGoals_SkipsFailed(t *testing.T) {
	goals := []model.Goal{
		{ID: "a", TaskID: "t1", Status: model.GoalStatusPending},
		{ID: "b", TaskID: "t1", Status: model.GoalStatusPending},
	}

	dag := BuildDAG(goals)
	dag.MarkFailed("a")

	ready := dag.ReadyGoals()
	require.Len(t, ready, 1)
	assert.Equal(t, "b", ready[0].ID)
}

func TestEmptyDAG(t *testing.T) {
	dag := BuildDAG(nil)

	ready := dag.ReadyGoals()
	assert.Empty(t, ready)
	assert.True(t, dag.AllComplete())
}

// readyIDs extracts goal IDs from a slice of goals for easier assertions.
func readyIDs(goals []model.Goal) []string {
	ids := make([]string, len(goals))
	for i, g := range goals {
		ids[i] = g.ID
	}
	return ids
}
