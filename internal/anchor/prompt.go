package anchor

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// AnchorPromptData holds the values injected into the anchor prompt template.
type AnchorPromptData struct {
	Spec      string
	RepoDiffs []RepoDiff
	Summaries []GoalSummary
}

// RepoDiff contains git diff output for a single repo worktree.
type RepoDiff struct {
	RepoName string
	DiffStat string
	Diff     string
}

// GoalSummary contains the completion summary for a single goal.
type GoalSummary struct {
	GoalID      string
	RepoName    string
	Description string
	Status      string
	Summary     string
	Notes       string
}

// BuildAnchorPrompt renders the anchor prompt template with the given data.
func BuildAnchorPrompt(templateStr string, data AnchorPromptData) (string, error) {
	tmpl, err := template.New("anchor-prompt").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("parsing anchor prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing anchor prompt template: %w", err)
	}

	return buf.String(), nil
}

// BuildAnchorPromptDefault renders the anchor prompt using the embedded default template.
func BuildAnchorPromptDefault(data AnchorPromptData) (string, error) {
	tmplBytes, err := defaults.FS.ReadFile("prompts/anchor.md")
	if err != nil {
		return "", fmt.Errorf("reading embedded anchor prompt: %w", err)
	}
	return BuildAnchorPrompt(string(tmplBytes), data)
}
