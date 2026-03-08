package anchor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAnchorPrompt(t *testing.T) {
	data := AnchorPromptData{
		Spec: "Add user roles to the API and display them in the app.",
		RepoDiffs: []RepoDiff{
			{
				RepoName: "api",
				DiffStat: " api/handlers/users.go | 25 +++++++++",
				Diff:     "+func GetUser(w http.ResponseWriter, r *http.Request) {",
			},
			{
				RepoName: "app",
				DiffStat: " src/UserProfile.tsx | 10 +++++",
				Diff:     "+<RoleBadge role={user.role} />",
			},
		},
		Summaries: []GoalSummary{
			{
				GoalID:      "api-1",
				RepoName:    "api",
				Description: "Add /users endpoint with role field",
				Status:      "complete",
				Summary:     "Added GET /users endpoint",
				Notes:       "Used existing auth middleware",
			},
			{
				GoalID:      "app-1",
				RepoName:    "app",
				Description: "Display user role in profile",
				Status:      "complete",
				Summary:     "Added RoleBadge component",
			},
		},
	}

	prompt, err := BuildAnchorPrompt(data)
	require.NoError(t, err)

	// Check spec included
	assert.Contains(t, prompt, "Add user roles to the API")

	// Check diffs included
	assert.Contains(t, prompt, "Repository: api")
	assert.Contains(t, prompt, "Repository: app")
	assert.Contains(t, prompt, "func GetUser")
	assert.Contains(t, prompt, "RoleBadge")

	// Check summaries included
	assert.Contains(t, prompt, "Goal: api-1")
	assert.Contains(t, prompt, "Goal: app-1")
	assert.Contains(t, prompt, "Added GET /users endpoint")
	assert.Contains(t, prompt, "Used existing auth middleware")

	// Check instructions included
	assert.Contains(t, prompt, "VERDICT.json")
	assert.Contains(t, prompt, "cross-repo alignment")
}
