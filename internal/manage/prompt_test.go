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
		CragName:  "my-project",
		RepoNames: []string{"api", "frontend"},
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
	commands := []string{
		"blr-config.md",
		"blr-logs.md",
		"blr-mail.md",
		"blr-message.md",
		"blr-pr.md",
		"blr-problem-brainstorm.md",
		"blr-problem-create.md",
		"blr-problem-list.md",
		"blr-prs.md",
		"blr-status.md",
		"blr-sync.md",
		"blr-ticket-list.md",
		"blr-ticket.md",
	}
	for _, cmd := range commands {
		path := filepath.Join(dir, ".claude", "commands", cmd)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			t.Errorf("expected command file %s to exist", cmd)
			continue
		}
		if err != nil {
			t.Errorf("reading command file %s: %v", cmd, err)
			continue
		}
		if strings.Contains(string(data), "{{.CragName}}") {
			t.Errorf("command file %s should not contain unresolved template placeholders", cmd)
		}
	}

	for _, cmd := range []string{
		"config.md",
		"logs.md",
		"mail.md",
		"message.md",
		"pr.md",
		"problem-brainstorm.md",
		"problem-create.md",
		"problem-list.md",
		"prs.md",
		"status.md",
		"sync.md",
		"ticket-list.md",
		"ticket.md",
	} {
		path := filepath.Join(dir, ".claude", "commands", cmd)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("legacy command file %s should not exist", cmd)
		}
	}
}

func TestPrepareManageDir_TemplateRendering(t *testing.T) {
	dir := t.TempDir()

	err := PrepareManageDir(dir, PromptData{
		CragName:  "solo",
		RepoNames: []string{"monorepo"},
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
	if !strings.Contains(content, "/blr-problem-brainstorm") {
		t.Error("CLAUDE.md should contain the blr-problem-brainstorm command name")
	}
	if !strings.Contains(content, "/blr-problem-create") {
		t.Error("CLAUDE.md should contain the blr-problem-create command name")
	}
	if !strings.Contains(content, "/blr-ticket") || !strings.Contains(content, "/blr-prs") {
		t.Error("CLAUDE.md should contain blr-prefixed tracker and PR commands")
	}
	if strings.Contains(content, "/problem-brainstorm") || strings.Contains(content, "/problem-create") {
		t.Error("CLAUDE.md should not contain legacy unprefixed command names")
	}
	if strings.Contains(content, "/belayer:") {
		t.Error("CLAUDE.md should not contain legacy /belayer: command names")
	}
}
