package lead

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptData holds the values injected into the lead prompt template.
type PromptData struct {
	Spec        string
	GoalID      string
	RepoName    string
	Description string
}

const promptTemplate = `You are a lead agent working on a specific goal within a larger task.

## Task Specification

{{.Spec}}

## Your Goal

**Goal ID**: {{.GoalID}}
**Repository**: {{.RepoName}}
**Description**: {{.Description}}

## Instructions

1. Read the task specification above carefully
2. Focus ONLY on your specific goal
3. Plan your approach, then implement it
4. Run tests to verify your work
5. When complete, write a DONE.json file in the current directory

## DONE.json Format

When you have completed your goal, create a file called DONE.json in the current directory with this exact format:

{
  "status": "complete",
  "summary": "Brief description of what you did",
  "files_changed": ["list", "of", "files", "you", "modified"],
  "notes": "Any additional context for reviewers"
}

If you cannot complete the goal, write DONE.json with status "failed":

{
  "status": "failed",
  "summary": "Why you could not complete the goal",
  "files_changed": [],
  "notes": "What blocked you"
}

IMPORTANT: You MUST write DONE.json before exiting. This is how the system knows you are finished.`

// BuildPrompt renders the lead prompt template with the given data.
func BuildPrompt(data PromptData) (string, error) {
	tmpl, err := template.New("lead-prompt").Parse(promptTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing prompt template: %w", err)
	}

	return buf.String(), nil
}
