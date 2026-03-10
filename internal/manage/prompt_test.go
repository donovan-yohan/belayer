package manage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareManageDir(t *testing.T) {
	dir := t.TempDir()

	err := PrepareManageDir(dir, PromptData{
		InstanceName: "my-project",
		RepoNames:    []string{"api", "frontend"},
	})
	if err != nil {
		t.Fatalf("PrepareManageDir() error: %v", err)
	}

	// Verify .claude/CLAUDE.md was written with rendered template
	claudeMD, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	content := string(claudeMD)
	if !strings.Contains(content, `crag "my-project"`) {
		t.Error("CLAUDE.md should contain crag name")
	}
	if !strings.Contains(content, "api") || !strings.Contains(content, "frontend") {
		t.Error("CLAUDE.md should contain repo names")
	}

	// Verify commands were copied
	commands := []string{"status.md", "problem-create.md", "problem-list.md", "logs.md", "message.md", "mail.md"}
	for _, cmd := range commands {
		path := filepath.Join(dir, ".claude", "commands", cmd)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected command file %s to exist", cmd)
		}
	}
}

func TestPrepareManageDir_TemplateRendering(t *testing.T) {
	dir := t.TempDir()

	err := PrepareManageDir(dir, PromptData{
		InstanceName: "solo",
		RepoNames:    []string{"monorepo"},
	})
	if err != nil {
		t.Fatalf("PrepareManageDir() error: %v", err)
	}

	claudeMD, _ := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	content := string(claudeMD)

	if !strings.Contains(content, `crag "solo"`) {
		t.Error("CLAUDE.md should contain crag name")
	}
	if !strings.Contains(content, "monorepo") {
		t.Error("CLAUDE.md should contain repo name")
	}
	if !strings.Contains(content, "belayer problem create") {
		t.Error("CLAUDE.md should contain CLI reference")
	}
}
