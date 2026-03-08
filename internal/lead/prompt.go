package lead

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// PromptData holds the values injected into the lead prompt template.
type PromptData struct {
	Spec            string
	GoalID          string
	RepoName        string
	Description     string
	SpotterFeedback string // empty on first attempt, populated on retry
}

// BuildPrompt renders the lead prompt template with the given data.
func BuildPrompt(templateStr string, data PromptData) (string, error) {
	tmpl, err := template.New("lead-prompt").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing prompt template: %w", err)
	}

	return buf.String(), nil
}

// BuildPromptDefault renders the lead prompt using the embedded default template.
func BuildPromptDefault(data PromptData) (string, error) {
	tmplBytes, err := defaults.FS.ReadFile("prompts/lead.md")
	if err != nil {
		return "", fmt.Errorf("reading embedded lead prompt: %w", err)
	}
	return BuildPrompt(string(tmplBytes), data)
}
