package manage

import (
	"strings"
	"testing"
)

func TestBuildPrompt(t *testing.T) {
	data := PromptData{
		InstanceName: "my-project",
		RepoNames:    []string{"api", "frontend", "shared-lib"},
	}

	prompt, err := BuildPrompt(data)
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	// Verify instance name appears
	if !strings.Contains(prompt, `instance "my-project"`) {
		t.Error("prompt should contain instance name")
	}

	// Verify all repo names appear
	for _, repo := range data.RepoNames {
		if !strings.Contains(prompt, repo) {
			t.Errorf("prompt should contain repo name %q", repo)
		}
	}

	// Verify key sections are present
	sections := []string{
		"spec.md",
		"goals.json",
		"belayer task create",
		"depends_on",
		"Jira",
	}
	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("prompt should contain %q", section)
		}
	}

	// Verify the task create command uses the right instance
	if !strings.Contains(prompt, "--instance my-project") {
		t.Error("prompt task create command should reference the instance name")
	}
}

func TestBuildPromptSingleRepo(t *testing.T) {
	data := PromptData{
		InstanceName: "solo",
		RepoNames:    []string{"monorepo"},
	}

	prompt, err := BuildPrompt(data)
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "monorepo") {
		t.Error("prompt should contain single repo name")
	}

	if !strings.Contains(prompt, `instance "solo"`) {
		t.Error("prompt should contain instance name")
	}
}
