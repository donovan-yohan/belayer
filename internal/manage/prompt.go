package manage

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// PromptData holds the values injected into the manage CLAUDE.md template.
type PromptData struct {
	CragName  string
	RepoNames []string
}

// ExplorerPromptData holds the values injected into the explorer CLAUDE.md template.
type ExplorerPromptData struct {
	Name          string
	PRDPath       string
	WorkspaceRoot string
}

var nowFunc = time.Now

var explorerCommandFiles = []string{
	"blr-research.md",
	"blr-research-url.md",
	"blr-research-summarize.md",
	"blr-phase-plan.md",
	"blr-draft-create.md",
	"blr-draft-list.md",
	"blr-draft-review.md",
}

var legacyCommandFiles = []string{
	"config.md",
	"draft-create.md",
	"draft-list.md",
	"draft-review.md",
	"logs.md",
	"mail.md",
	"message.md",
	"phase-plan.md",
	"pr.md",
	"problem-brainstorm.md",
	"problem-create.md",
	"problem-list.md",
	"prs.md",
	"research.md",
	"research-summarize.md",
	"research-url.md",
	"status.md",
	"sync.md",
	"ticket-list.md",
	"ticket.md",
}

// PrepareManageDir writes .claude/CLAUDE.md (rendered) and .claude/commands/*.md (static)
// to the given directory, preparing it as a workspace for the manage Claude session.
func PrepareManageDir(dir string, data PromptData) error {
	return prepareClaudeWorkspace(dir, "claudemd/setter.md", "setter-claude-md", data, nil)
}

// PrepareExplorerDir writes explorer session context into a workspace under rootDir and returns
// the workspace path. Empty names use an _unnamed-<timestamp> directory while the template keeps
// the logical project name empty so the session can ask for it naturally.
func PrepareExplorerDir(rootDir string, data ExplorerPromptData) (string, error) {
	data.Name = sanitizeWorkspaceName(data.Name)
	data.PRDPath = strings.TrimSpace(data.PRDPath)

	workspaceDir := filepath.Join(rootDir, explorerWorkspaceName(data.Name, nowFunc().UTC()))
	data.WorkspaceRoot = workspaceDir
	if err := prepareClaudeWorkspace(workspaceDir, "claudemd/explorer.md", "explorer-claude-md", data, explorerCommandFiles); err != nil {
		return "", err
	}
	return workspaceDir, nil
}

func prepareClaudeWorkspace(dir, templatePath, templateName string, data any, commandFiles []string) error {
	claudeDir := filepath.Join(dir, ".claude")
	commandsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude/commands: %w", err)
	}

	if err := writeRenderedTemplate(filepath.Join(claudeDir, "CLAUDE.md"), templatePath, templateName, data); err != nil {
		return err
	}

	if err := copyCommandFiles(commandsDir, commandFiles); err != nil {
		return err
	}

	return nil
}

func writeRenderedTemplate(targetPath, templatePath, templateName string, data any) error {
	tmplBytes, err := defaults.FS.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading template %s: %w", templatePath, err)
	}

	tmpl, err := template.New(templateName).Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", templatePath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering template %s: %w", templatePath, err)
	}

	if err := os.WriteFile(targetPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", targetPath, err)
	}
	return nil
}

func copyCommandFiles(commandsDir string, commandFiles []string) error {
	entries, err := defaults.FS.ReadDir("commands")
	if err != nil {
		return fmt.Errorf("reading commands dir: %w", err)
	}

	allowed := make(map[string]struct{}, len(entries))
	if len(commandFiles) == 0 {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			allowed[entry.Name()] = struct{}{}
		}
	} else {
		for _, name := range commandFiles {
			allowed[name] = struct{}{}
		}
	}

	if err := pruneGeneratedCommandFiles(commandsDir, allowed); err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := allowed[entry.Name()]; !ok {
			continue
		}
		cmdData, err := fs.ReadFile(defaults.FS, "commands/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading command %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(commandsDir, entry.Name()), cmdData, 0o644); err != nil {
			return fmt.Errorf("writing command %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func pruneGeneratedCommandFiles(commandsDir string, allowed map[string]struct{}) error {
	if len(allowed) == 0 {
		return nil
	}

	generatedEntries, err := defaults.FS.ReadDir("commands")
	if err != nil {
		return fmt.Errorf("reading commands dir for pruning: %w", err)
	}

	managed := make(map[string]struct{}, len(generatedEntries)+len(legacyCommandFiles))
	for _, entry := range generatedEntries {
		if entry.IsDir() {
			continue
		}
		managed[entry.Name()] = struct{}{}
	}
	for _, name := range legacyCommandFiles {
		managed[name] = struct{}{}
	}

	existingEntries, err := os.ReadDir(commandsDir)
	if err != nil {
		return fmt.Errorf("reading existing commands dir: %w", err)
	}

	for _, entry := range existingEntries {
		if entry.IsDir() {
			continue
		}
		if _, ok := managed[entry.Name()]; !ok {
			continue
		}
		if _, ok := allowed[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(commandsDir, entry.Name())); err != nil {
			return fmt.Errorf("removing stale command %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// NamedExplorerWorkspaceDir returns the stable workspace path for a named explorer project.
// Empty or invalid names return the empty string because they map to timestamped unnamed workspaces.
func NamedExplorerWorkspaceDir(rootDir string, name string) string {
	name = sanitizeWorkspaceName(name)
	if name == "" {
		return ""
	}
	return filepath.Join(rootDir, name)
}

func explorerWorkspaceName(name string, timestamp time.Time) string {
	name = sanitizeWorkspaceName(name)
	if name != "" {
		return name
	}
	return "_unnamed-" + timestamp.UTC().Format("20060102-150405")
}

func sanitizeWorkspaceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.NewReplacer("/", "-", "\\", "-").Replace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}
