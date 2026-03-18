package manage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
		"blr-draft-create.md",
		"blr-draft-list.md",
		"blr-draft-review.md",
		"blr-logs.md",
		"blr-mail.md",
		"blr-message.md",
		"blr-phase-plan.md",
		"blr-pr.md",
		"blr-problem-brainstorm.md",
		"blr-problem-create.md",
		"blr-problem-list.md",
		"blr-prs.md",
		"blr-research.md",
		"blr-research-summarize.md",
		"blr-research-url.md",
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
		"phase-plan.md",
		"problem-brainstorm.md",
		"problem-create.md",
		"problem-list.md",
		"prs.md",
		"draft-create.md",
		"draft-list.md",
		"draft-review.md",
		"research.md",
		"research-summarize.md",
		"research-url.md",
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
	if !strings.Contains(content, "/blr-research") || !strings.Contains(content, "/blr-research-url") || !strings.Contains(content, "/blr-research-summarize") {
		t.Error("CLAUDE.md should contain the blr research command names")
	}
	if !strings.Contains(content, "/blr-phase-plan") || !strings.Contains(content, "/blr-draft-create") || !strings.Contains(content, "/blr-draft-list") || !strings.Contains(content, "/blr-draft-review") {
		t.Error("CLAUDE.md should contain the blr draft workflow command names")
	}
	if !strings.Contains(content, "/blr-ticket") || !strings.Contains(content, "/blr-prs") {
		t.Error("CLAUDE.md should contain blr-prefixed tracker and PR commands")
	}
	if !strings.Contains(content, "## Operating Principles") || !strings.Contains(content, "Treat explorer-produced drafts as the normal handoff into setter sessions") {
		t.Error("CLAUDE.md should contain the setter operating-principles guidance")
	}
	if !strings.Contains(content, "research-notes.md") || !strings.Contains(content, "research.md") {
		t.Error("CLAUDE.md should describe the research artifact files")
	}
	if !strings.Contains(content, "~/.belayer/crags/solo/docs") {
		t.Error("CLAUDE.md should render the research root for the current crag")
	}
	if !strings.Contains(content, "~/.belayer/drafts/solo/problems") || !strings.Contains(content, "depends_on") {
		t.Error("CLAUDE.md should render the draft root and queue metadata guidance for the current crag")
	}
	if strings.Contains(content, "/problem-brainstorm") || strings.Contains(content, "/problem-create") {
		t.Error("CLAUDE.md should not contain legacy unprefixed command names")
	}
	if strings.Contains(content, "/belayer:") {
		t.Error("CLAUDE.md should not contain legacy /belayer: command names")
	}
}

func TestPrepareManageDir_RemovesLegacyCommandsOnReuse(t *testing.T) {
	dir := t.TempDir()

	if err := PrepareManageDir(dir, PromptData{
		CragName:  "solo",
		RepoNames: []string{"monorepo"},
	}); err != nil {
		t.Fatalf("PrepareManageDir() error: %v", err)
	}

	stalePath := filepath.Join(dir, ".claude", "commands", "problem-create.md")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := PrepareManageDir(dir, PromptData{
		CragName:  "solo",
		RepoNames: []string{"monorepo"},
	}); err != nil {
		t.Fatalf("PrepareManageDir() on reused workspace error: %v", err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("reused setter workspace should remove stale legacy generated command files")
	}
}

func TestPrepareExplorerDir_NamedWorkspace(t *testing.T) {
	rootDir := t.TempDir()

	workspaceDir, err := PrepareExplorerDir(rootDir, ExplorerPromptData{
		Name:    "myproject",
		PRDPath: "/tmp/prd.md",
	})
	if err != nil {
		t.Fatalf("PrepareExplorerDir() error: %v", err)
	}

	expectedDir := filepath.Join(rootDir, "myproject")
	if workspaceDir != expectedDir {
		t.Fatalf("workspaceDir = %q, want %q", workspaceDir, expectedDir)
	}

	claudeMD, err := os.ReadFile(filepath.Join(workspaceDir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	content := string(claudeMD)
	if !strings.Contains(content, "**Project Name:** myproject") {
		t.Error("CLAUDE.md should contain the explorer project name")
	}
	if !strings.Contains(content, "**PRD Path:** /tmp/prd.md") {
		t.Error("CLAUDE.md should contain the explorer PRD path")
	}
	if !strings.Contains(content, expectedDir) {
		t.Error("CLAUDE.md should publish the explorer workspace root")
	}
	for _, phase := range []string{"### 1. Research", "### 2. Decomposition", "### 3. Scaffold", "### 4. Draft Problems", "### 5. Handoff"} {
		if !strings.Contains(content, phase) {
			t.Errorf("CLAUDE.md should contain the explorer phase %q", phase)
		}
	}
	if !strings.Contains(content, "## Operating Principles") {
		t.Error("CLAUDE.md should contain the explorer operating principles section")
	}
	if !strings.Contains(content, "/blr-research") || !strings.Contains(content, "belayer setter -c <crag-name>") {
		t.Error("CLAUDE.md should describe the explorer workflow and handoff")
	}
	if !strings.Contains(content, "Belayer Draft Quality Bar") {
		t.Error("CLAUDE.md should include the explorer draft quality section")
	}
	if !strings.Contains(content, "## Workflows") || !strings.Contains(content, "New project from PRD") || !strings.Contains(content, "Blank-slate project") {
		t.Error("CLAUDE.md should document the explorer workflows explicitly")
	}
	if !strings.Contains(content, "`spec.md`") || !strings.Contains(content, "`climbs.json`") {
		t.Error("CLAUDE.md should describe the explorer draft artifacts")
	}
	if !strings.Contains(content, "~/.belayer/drafts/<crag-name>/problems/") || !strings.Contains(content, "intended crag name") {
		t.Error("CLAUDE.md should use the intended crag name for explorer draft storage guidance")
	}
	if !strings.Contains(content, "`depends_on`") || !strings.Contains(content, "same repo only") {
		t.Error("CLAUDE.md should describe same-repo depends_on rules for climbs")
	}
	if !strings.Contains(content, "Stay in explorer mode") || !strings.Contains(content, "stop the explorer session cleanly") {
		t.Error("CLAUDE.md should keep explorer sessions in planning mode until handoff is ready")
	}

	entries, err := os.ReadDir(filepath.Join(workspaceDir, ".claude", "commands"))
	if err != nil {
		t.Fatalf("reading commands dir: %v", err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
		if !slices.Contains(explorerCommandFiles, entry.Name()) {
			t.Fatalf("unexpected explorer command copied: %s", entry.Name())
		}
	}
	if len(got) != len(explorerCommandFiles) {
		t.Fatalf("copied %d explorer commands, want %d", len(got), len(explorerCommandFiles))
	}
	for _, name := range explorerCommandFiles {
		if !slices.Contains(got, name) {
			t.Fatalf("missing explorer command %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, ".claude", "commands", "blr-problem-create.md")); !os.IsNotExist(err) {
		t.Error("explorer workspace should not copy setter-only command files")
	}
}

func TestPrepareExplorerDir_RemovesStaleSetterCommandsOnReuse(t *testing.T) {
	rootDir := t.TempDir()

	workspaceDir, err := PrepareExplorerDir(rootDir, ExplorerPromptData{Name: "myproject"})
	if err != nil {
		t.Fatalf("PrepareExplorerDir() error: %v", err)
	}

	stalePath := filepath.Join(workspaceDir, ".claude", "commands", "blr-problem-create.md")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reusedDir, err := PrepareExplorerDir(rootDir, ExplorerPromptData{Name: "myproject"})
	if err != nil {
		t.Fatalf("PrepareExplorerDir() on reused workspace error: %v", err)
	}
	if reusedDir != workspaceDir {
		t.Fatalf("reusedDir = %q, want %q", reusedDir, workspaceDir)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("reused explorer workspace should remove stale setter-only command files")
	}
}

func TestPrepareExplorerDir_UnnamedWorkspace(t *testing.T) {
	rootDir := t.TempDir()

	restore := stubNowFunc(t, time.Date(2026, time.March, 17, 12, 34, 56, 0, time.UTC))
	defer restore()

	workspaceDir, err := PrepareExplorerDir(rootDir, ExplorerPromptData{})
	if err != nil {
		t.Fatalf("PrepareExplorerDir() error: %v", err)
	}

	expectedDir := filepath.Join(rootDir, "_unnamed-20260317-123456")
	if workspaceDir != expectedDir {
		t.Fatalf("workspaceDir = %q, want %q", workspaceDir, expectedDir)
	}

	claudeMD, err := os.ReadFile(filepath.Join(workspaceDir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	content := string(claudeMD)
	if !strings.Contains(content, "Not chosen yet") {
		t.Error("CLAUDE.md should handle empty project names gracefully")
	}
	if !strings.Contains(content, "None supplied") {
		t.Error("CLAUDE.md should handle missing PRD paths gracefully")
	}
	if !strings.Contains(content, "Redirect implementation requests to belayer problem creation") {
		t.Error("CLAUDE.md should enforce the explorer problem-creation boundary")
	}
	if !strings.Contains(content, "Workspace Root") || !strings.Contains(content, "/blr-draft-review") || !strings.Contains(content, expectedDir) {
		t.Error("CLAUDE.md should describe the explorer workspace root and draft workflow")
	}
}

func TestPrepareExplorerDir_DotNameIsTreatedAsUnnamed(t *testing.T) {
	rootDir := t.TempDir()

	restore := stubNowFunc(t, time.Date(2026, time.March, 17, 13, 0, 0, 0, time.UTC))
	defer restore()

	workspaceDir, err := PrepareExplorerDir(rootDir, ExplorerPromptData{Name: "."})
	if err != nil {
		t.Fatalf("PrepareExplorerDir() error: %v", err)
	}

	expectedDir := filepath.Join(rootDir, "_unnamed-20260317-130000")
	if workspaceDir != expectedDir {
		t.Fatalf("workspaceDir = %q, want %q", workspaceDir, expectedDir)
	}

	claudeMD, err := os.ReadFile(filepath.Join(workspaceDir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(claudeMD), "Not chosen yet") {
		t.Error("CLAUDE.md should treat dot-path names as unnamed projects")
	}
}

func TestNamedExplorerWorkspaceDir(t *testing.T) {
	rootDir := t.TempDir()

	if got := NamedExplorerWorkspaceDir(rootDir, "my/project"); got != filepath.Join(rootDir, "my-project") {
		t.Fatalf("NamedExplorerWorkspaceDir() = %q, want sanitized named workspace path", got)
	}
	if got := NamedExplorerWorkspaceDir(rootDir, "."); got != "" {
		t.Fatalf("NamedExplorerWorkspaceDir() = %q, want empty path for unnamed workspace", got)
	}
}

func stubNowFunc(t *testing.T, timestamp time.Time) func() {
	t.Helper()
	original := nowFunc
	nowFunc = func() time.Time { return timestamp }
	return func() {
		nowFunc = original
	}
}
