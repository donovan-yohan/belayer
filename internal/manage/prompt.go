package manage

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"github.com/donovan-yohan/belayer/internal/defaults"
)

// PromptData holds the values injected into the manage CLAUDE.md template.
type PromptData struct {
	InstanceName string
	RepoNames    []string
}

// PrepareManageDir writes .claude/CLAUDE.md (rendered) and .claude/commands/*.md (static)
// to the given directory, preparing it as a workspace for the manage Claude session.
func PrepareManageDir(dir string, data PromptData) error {
	claudeDir := filepath.Join(dir, ".claude")
	commandsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude/commands: %w", err)
	}

	// Render and write CLAUDE.md
	tmplBytes, err := defaults.FS.ReadFile("claudemd/setter.md")
	if err != nil {
		return fmt.Errorf("reading setter template: %w", err)
	}

	tmpl, err := template.New("setter-claude-md").Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parsing manage template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering setter template: %w", err)
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing CLAUDE.md: %w", err)
	}

	// Copy static command files
	entries, err := defaults.FS.ReadDir("commands")
	if err != nil {
		return fmt.Errorf("reading commands dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
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
