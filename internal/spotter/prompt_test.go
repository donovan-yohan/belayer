package spotter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSpotterPrompt(t *testing.T) {
	tmpl := `You are validating goal {{.GoalID}} in {{.RepoName}}.
Description: {{.Description}}
WorkDir: {{.WorkDir}}

{{range $name, $content := .Profiles}}
### Profile: {{$name}}
{{$content}}
{{end}}

Lead output: {{.DoneJSON}}

Write SPOT.json when done.`

	data := SpotterPromptData{
		GoalID:      "setup",
		RepoName:    "frontend",
		Description: "Initialize project scaffolding",
		WorkDir:     "/tmp/test",
		Profiles:    map[string]string{"frontend": "build = \"Run build command\""},
		DoneJSON:    `{"status": "complete", "summary": "scaffolded"}`,
	}

	prompt, err := BuildSpotterPrompt(tmpl, data)
	require.NoError(t, err)
	assert.Contains(t, prompt, "setup")
	assert.Contains(t, prompt, "frontend")
	assert.Contains(t, prompt, "SPOT.json")
	assert.Contains(t, prompt, "Run build command")
	assert.Contains(t, prompt, "scaffolded")
}

func TestBuildSpotterPromptDefault(t *testing.T) {
	data := SpotterPromptData{
		GoalID:      "api-setup",
		RepoName:    "backend",
		Description: "Set up API endpoints",
		WorkDir:     "/tmp/work",
		Profiles:    map[string]string{"backend": "test_suite = \"Run tests\""},
		DoneJSON:    `{"status": "complete"}`,
	}

	prompt, err := BuildSpotterPromptDefault(data)
	require.NoError(t, err)
	assert.Contains(t, prompt, "api-setup")
	assert.Contains(t, prompt, "SPOT.json")
}

func TestBuildSpotterPrompt_MultipleProfiles(t *testing.T) {
	tmpl := `{{range $name, $content := .Profiles}}Profile: {{$name}}: {{$content}}
{{end}}`

	data := SpotterPromptData{
		Profiles: map[string]string{
			"frontend": "build check",
			"backend":  "test check",
		},
	}

	prompt, err := BuildSpotterPrompt(tmpl, data)
	require.NoError(t, err)
	assert.Contains(t, prompt, "frontend")
	assert.Contains(t, prompt, "backend")
}
