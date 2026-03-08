package lead

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptData holds the values injected into the lead prompt template.
type PromptData struct {
	Spec            string
	GoalID          string
	RepoName        string
	Description     string
	SpotterFeedback string // empty on first attempt, populated on retry
}

const promptTemplate = `You are a lead agent working on a specific goal within a larger task.

## Task Specification

{{.Spec}}

## Your Goal

**Goal ID**: {{.GoalID}}
**Repository**: {{.RepoName}}
**Description**: {{.Description}}

{{if .SpotterFeedback}}
## Previous Attempt Feedback

A validator found issues with your previous attempt. You MUST address these:

{{.SpotterFeedback}}

Fix these issues before marking the goal complete.
{{end}}
## Instructions

1. Read the task specification above carefully
2. Focus ONLY on your specific goal
3. Plan your approach, then implement it
4. Run tests to verify your work
5. Commit all your changes with a descriptive message
6. Push to the remote
7. Write a DONE.json file in the current directory

## Committing and Pushing

After completing your work, you MUST commit and push before writing DONE.json:

git add -A
git commit -m "{{.GoalID}}: <brief summary of what you did>"
git push origin HEAD

If the push fails (e.g., no upstream set), try:
git push -u origin HEAD

## DONE.json Format

After committing and pushing, create a file called DONE.json in the current directory:

{
  "status": "complete",
  "summary": "Brief description of what you did",
  "files_changed": ["list", "of", "files", "you", "modified"],
  "notes": "Any additional context for reviewers"
}

If you cannot complete the goal, still commit any partial work, then write DONE.json with status "failed":

{
  "status": "failed",
  "summary": "Why you could not complete the goal",
  "files_changed": [],
  "notes": "What blocked you"
}

IMPORTANT: You MUST commit, push, and write DONE.json before exiting. This is how the system tracks your work.`

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
