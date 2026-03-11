package scm

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PRBodyOutput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// BuildPRBodyPrompt constructs a prompt asking Claude to generate a PR title and body.
// Output format is JSON with "title" and "body" keys.
func BuildPRBodyPrompt(problemSpec, repoName string, filesChanged []string, climbSummaries string) string {
	fileList := strings.Join(filesChanged, "\n- ")
	if len(filesChanged) > 0 {
		fileList = "- " + fileList
	}
	return fmt.Sprintf(`You are a senior engineer writing a pull request for repository %q.

Problem specification:
%s

Files changed:
%s

Climb summaries:
%s

Generate a concise PR title and body that accurately describes the changes.
The body should include a summary of what changed and why.

Respond ONLY with valid JSON in this exact format:
{"title": "<PR title>", "body": "<PR body in markdown>"}`,
		repoName, problemSpec, fileList, climbSummaries)
}

// ParsePRBodyOutput parses the JSON output from Claude into a title and body.
// It strips markdown code fences if present.
func ParsePRBodyOutput(raw string) (title, body string, err error) {
	s := strings.TrimSpace(raw)
	// Strip ```json ... ``` or ``` ... ``` fences
	if strings.HasPrefix(s, "```") {
		first := strings.Index(s, "\n")
		last := strings.LastIndex(s, "```")
		if first != -1 && last > first {
			s = strings.TrimSpace(s[first+1 : last])
		}
	}
	var out PRBodyOutput
	if err = json.Unmarshal([]byte(s), &out); err != nil {
		return "", "", fmt.Errorf("failed to parse PR body output: %w", err)
	}
	return out.Title, out.Body, nil
}
