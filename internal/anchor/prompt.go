package anchor

import (
	"bytes"
	"fmt"
	"text/template"
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

const anchorTemplate = `You are an anchor agent reviewing cross-repo changes for alignment and correctness.

## Task Specification

{{.Spec}}

## Goal Completion Summaries

{{range .Summaries}}### Goal: {{.GoalID}} ({{.RepoName}})
**Description**: {{.Description}}
**Status**: {{.Status}}
**Summary**: {{.Summary}}
{{if .Notes}}**Notes**: {{.Notes}}
{{end}}
{{end}}

## Repository Diffs

{{range .RepoDiffs}}### Repository: {{.RepoName}}

**Changed files:**
` + "```" + `
{{.DiffStat}}
` + "```" + `

**Diff:**
` + "```diff" + `
{{.Diff}}
` + "```" + `

{{end}}

## Instructions

1. Review ALL diffs against the original task specification
2. Check cross-repo alignment:
   - API contracts match between frontend and backend
   - Shared types, schemas, or interfaces are consistent
   - Integration points are compatible
3. Verify each repo's changes fulfill their assigned goals
4. Write a VERDICT.json file in the current directory

## VERDICT.json Format

If all repos pass review:

{
  "verdict": "approve",
  "repos": {
    "repo-name": {
      "status": "pass",
      "goals": []
    }
  }
}

If any repo fails review, specify correction goals:

{
  "verdict": "reject",
  "repos": {
    "passing-repo": {
      "status": "pass",
      "goals": []
    },
    "failing-repo": {
      "status": "fail",
      "goals": [
        "Fix the response schema to match frontend expectations",
        "Add missing error handling for edge case X"
      ]
    }
  }
}

IMPORTANT:
- You MUST write VERDICT.json before exiting
- Include ALL repos in the verdict, even passing ones
- For failing repos, provide specific, actionable correction goals
- Each correction goal should be self-contained and implementable by a single agent session`

// BuildAnchorPrompt renders the anchor prompt template with the given data.
func BuildAnchorPrompt(data AnchorPromptData) (string, error) {
	tmpl, err := template.New("anchor-prompt").Parse(anchorTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing anchor prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing anchor prompt template: %w", err)
	}

	return buf.String(), nil
}
