package spotter

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// SpotterPromptData holds the values injected into the spotter prompt template.
type SpotterPromptData struct {
	GoalID      string
	RepoName    string
	Description string
	WorkDir     string
	Profiles    map[string]string // profile name -> profile content
	DoneJSON    string            // contents of DONE.json from the lead
}

// BuildSpotterPrompt renders the spotter prompt template with the given data.
func BuildSpotterPrompt(templateStr string, data SpotterPromptData) (string, error) {
	tmpl, err := template.New("spotter-prompt").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("parsing spotter prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing spotter prompt template: %w", err)
	}

	return buf.String(), nil
}

// BuildSpotterPromptDefault renders the spotter prompt using the embedded default template.
func BuildSpotterPromptDefault(data SpotterPromptData) (string, error) {
	tmplBytes, err := defaults.FS.ReadFile("prompts/spotter.md")
	if err != nil {
		return "", fmt.Errorf("reading embedded spotter prompt: %w", err)
	}
	return BuildSpotterPrompt(string(tmplBytes), data)
}
