package climbctx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLeadClimb(t *testing.T) {
	dir := t.TempDir()
	climb := LeadClimb{
		Role:            "lead",
		ProblemSpec:     "Build an API",
		ClimbID:         "api-1",
		RepoName:        "api",
		Description:     "Add /users endpoint",
		Attempt:         1,
		SpotterFeedback: "",
	}
	err := WriteClimbJSON(dir, "api-1", climb)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "api-1", "GOAL.json"))
	require.NoError(t, err)

	var parsed LeadClimb
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "lead", parsed.Role)
	assert.Equal(t, "api-1", parsed.ClimbID)
	assert.Equal(t, "Build an API", parsed.ProblemSpec)
}

func TestWriteSpotterClimb(t *testing.T) {
	dir := t.TempDir()
	climb := SpotterClimb{
		Role:        "spotter",
		ClimbID:     "fe-1",
		RepoName:    "app",
		Description: "Scaffold frontend",
		WorkDir:     "/tmp/worktree",
		Profiles:    map[string]string{"frontend": "[checks]\nbuild = \"npm run build\""},
		TopJSON:     `{"status":"complete","summary":"done"}`,
	}
	err := WriteClimbJSON(dir, "fe-1", climb)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "fe-1", "GOAL.json"))
	require.NoError(t, err)

	var parsed SpotterClimb
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "spotter", parsed.Role)
	assert.Contains(t, parsed.Profiles, "frontend")
}

func TestWriteAnchorClimb(t *testing.T) {
	dir := t.TempDir()
	climb := AnchorClimb{
		Role:        "anchor",
		ProblemSpec: "Build an app",
		RepoDiffs: []RepoDiff{
			{RepoName: "api", DiffStat: "handlers.go | 25 +++", Diff: "+func Get()"},
		},
		Summaries: []ClimbSummary{
			{ClimbID: "api-1", RepoName: "api", Summary: "Added endpoint"},
		},
	}
	err := WriteClimbJSON(dir, "anchor", climb)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".lead", "anchor", "GOAL.json"))
	require.NoError(t, err)

	var parsed AnchorClimb
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "anchor", parsed.Role)
	assert.Len(t, parsed.RepoDiffs, 1)
}
