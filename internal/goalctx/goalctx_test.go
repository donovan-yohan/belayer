package goalctx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLeadGoal(t *testing.T) {
	dir := t.TempDir()
	goal := LeadGoal{
		Role:            "lead",
		TaskSpec:        "Build an API",
		GoalID:          "api-1",
		RepoName:        "api",
		Description:     "Add /users endpoint",
		Attempt:         1,
		SpotterFeedback: "",
	}
	err := WriteGoalJSON(dir, goal)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
	require.NoError(t, err)

	var parsed LeadGoal
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "lead", parsed.Role)
	assert.Equal(t, "api-1", parsed.GoalID)
	assert.Equal(t, "Build an API", parsed.TaskSpec)
}

func TestWriteSpotterGoal(t *testing.T) {
	dir := t.TempDir()
	goal := SpotterGoal{
		Role:        "spotter",
		GoalID:      "fe-1",
		RepoName:    "app",
		Description: "Scaffold frontend",
		WorkDir:     "/tmp/worktree",
		Profiles:    map[string]string{"frontend": "[checks]\nbuild = \"npm run build\""},
		DoneJSON:    `{"status":"complete","summary":"done"}`,
	}
	err := WriteGoalJSON(dir, goal)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
	require.NoError(t, err)

	var parsed SpotterGoal
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "spotter", parsed.Role)
	assert.Contains(t, parsed.Profiles, "frontend")
}

func TestWriteAnchorGoal(t *testing.T) {
	dir := t.TempDir()
	goal := AnchorGoal{
		Role:     "anchor",
		TaskSpec: "Build an app",
		RepoDiffs: []RepoDiff{
			{RepoName: "api", DiffStat: "handlers.go | 25 +++", Diff: "+func Get()"},
		},
		Summaries: []GoalSummary{
			{GoalID: "api-1", RepoName: "api", Summary: "Added endpoint"},
		},
	}
	err := WriteGoalJSON(dir, goal)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "GOAL.json"))
	require.NoError(t, err)

	var parsed AnchorGoal
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "anchor", parsed.Role)
	assert.Len(t, parsed.RepoDiffs, 1)
}
