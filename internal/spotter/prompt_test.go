package spotter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSpotterPrompt(t *testing.T) {
	prompt, err := BuildSpotterPrompt(SpotterPromptData{
		GoalID:      "setup",
		RepoName:    "frontend",
		Description: "Initialize project scaffolding",
		WorkDir:     "/tmp/test",
		Profiles:    map[string]string{"frontend": "build = \"Run build\""},
		DoneJSON:    `{"status": "complete", "summary": "scaffolded"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, prompt, "setup")
	assert.Contains(t, prompt, "SPOT.json")
	assert.Contains(t, prompt, "frontend")
	assert.Contains(t, prompt, "Initialize project scaffolding")
	assert.Contains(t, prompt, "/tmp/test")
	assert.Contains(t, prompt, "Run build")
	assert.Contains(t, prompt, "scaffolded")
}

func TestBuildSpotterPrompt_MultipleProfiles(t *testing.T) {
	prompt, err := BuildSpotterPrompt(SpotterPromptData{
		GoalID:      "api-1",
		RepoName:    "api",
		Description: "Add endpoint",
		WorkDir:     "/tmp/api",
		Profiles: map[string]string{
			"backend":  "tests = \"Run go test\"",
			"frontend": "build = \"Run npm build\"",
		},
		DoneJSON: `{"status": "complete"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, prompt, "Profile: backend")
	assert.Contains(t, prompt, "Profile: frontend")
	assert.Contains(t, prompt, "Run go test")
	assert.Contains(t, prompt, "Run npm build")
}
