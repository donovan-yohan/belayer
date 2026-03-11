package tracker

import (
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/model"
)

func TestBuildSpecAssemblyPrompt(t *testing.T) {
	issue := &model.Issue{
		ID:    "PROJ-42",
		Title: "Add login feature",
		Body:  "Users need to be able to log in with email and password.",
		Comments: []model.Comment{
			{Author: "alice", Body: "Should support SSO too."},
		},
	}
	repoNames := []string{"api-repo", "frontend-repo"}

	prompt := BuildSpecAssemblyPrompt(issue, repoNames)

	if !strings.Contains(prompt, issue.ID) {
		t.Errorf("prompt missing issue ID %q", issue.ID)
	}
	if !strings.Contains(prompt, issue.Title) {
		t.Errorf("prompt missing issue title %q", issue.Title)
	}
	for _, repo := range repoNames {
		if !strings.Contains(prompt, repo) {
			t.Errorf("prompt missing repo name %q", repo)
		}
	}
	if !strings.Contains(prompt, "spec") {
		t.Error("prompt missing 'spec' output format reference")
	}
	if !strings.Contains(prompt, "@alice") {
		t.Error("prompt missing comment author")
	}
}

func TestParseSpecAssemblyOutput(t *testing.T) {
	raw := `{"spec":"Do the thing","climbs":{"repos":{"api-repo":{"climbs":[{"id":"add-auth","description":"Add auth endpoint","depends_on":[]}]}}}}`

	out, err := ParseSpecAssemblyOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Spec != "Do the thing" {
		t.Errorf("expected spec %q, got %q", "Do the thing", out.Spec)
	}
	repoClimbs, ok := out.Climbs.Repos["api-repo"]
	if !ok {
		t.Fatal("expected 'api-repo' in climbs")
	}
	if len(repoClimbs.Climbs) != 1 {
		t.Fatalf("expected 1 climb, got %d", len(repoClimbs.Climbs))
	}
	if repoClimbs.Climbs[0].ID != "add-auth" {
		t.Errorf("expected climb ID %q, got %q", "add-auth", repoClimbs.Climbs[0].ID)
	}
}

func TestParseSpecAssemblyOutput_WithFences(t *testing.T) {
	raw := "```json\n{\"spec\":\"Fenced spec\",\"climbs\":{\"repos\":{}}}\n```"

	out, err := ParseSpecAssemblyOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Spec != "Fenced spec" {
		t.Errorf("expected spec %q, got %q", "Fenced spec", out.Spec)
	}
}
