package scm

import (
	"strings"
	"testing"
)

func TestBuildPRBodyPrompt(t *testing.T) {
	spec := "Add user authentication via OAuth2"
	repo := "auth-service"
	files := []string{"internal/auth/oauth.go", "internal/auth/token.go", "cmd/server/main.go"}
	summaries := "Implemented OAuth2 flow; added token refresh logic"

	prompt := BuildPRBodyPrompt(spec, repo, files, summaries)

	if !strings.Contains(prompt, spec) {
		t.Errorf("prompt missing problem spec")
	}
	if !strings.Contains(prompt, repo) {
		t.Errorf("prompt missing repo name")
	}
	for _, f := range files {
		if !strings.Contains(prompt, f) {
			t.Errorf("prompt missing file %q", f)
		}
	}
	if !strings.Contains(prompt, summaries) {
		t.Errorf("prompt missing climb summaries")
	}
	if !strings.Contains(prompt, `"title"`) {
		t.Errorf("prompt missing output format hint for title")
	}
	if !strings.Contains(prompt, `"body"`) {
		t.Errorf("prompt missing output format hint for body")
	}
}

func TestParsePRBodyOutput(t *testing.T) {
	raw := `{"title": "feat: add OAuth2 authentication", "body": "## Summary\n\nAdds OAuth2 support."}`
	title, body, err := ParsePRBodyOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "feat: add OAuth2 authentication" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "## Summary\n\nAdds OAuth2 support." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestParsePRBodyOutput_WithFences(t *testing.T) {
	raw := "```json\n{\"title\": \"fix: resolve nil pointer\", \"body\": \"Fixes a crash.\"}\n```"
	title, body, err := ParsePRBodyOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "fix: resolve nil pointer" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "Fixes a crash." {
		t.Errorf("unexpected body: %q", body)
	}
}
