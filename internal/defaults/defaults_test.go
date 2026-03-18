package defaults

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedFilesExist(t *testing.T) {
	files := []string{
		"belayer.toml",
		"profiles/frontend.toml",
		"profiles/backend.toml",
		"profiles/cli.toml",
		"profiles/library.toml",
	}
	for _, f := range files {
		data, err := FS.ReadFile(f)
		require.NoError(t, err, "missing embedded file: %s", f)
		assert.NotEmpty(t, data, "empty embedded file: %s", f)
	}
}

func TestClaudeMDTemplatesExist(t *testing.T) {
	for _, name := range []string{"lead.md", "spotter.md", "anchor.md", "setter.md", "explorer.md"} {
		data, err := FS.ReadFile("claudemd/" + name)
		require.NoError(t, err, "claudemd/%s should be embedded", name)
		assert.NotEmpty(t, data)
	}
}

func TestCommandFilesExist(t *testing.T) {
	for _, name := range []string{
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
	} {
		data, err := FS.ReadFile("commands/" + name)
		require.NoError(t, err, "commands/%s should be embedded", name)
		assert.NotEmpty(t, data)
		assert.NotContains(t, string(data), "{{.CragName}}", "commands/%s should not contain unresolved template placeholders", name)
		assert.NotContains(t, string(data), "/belayer:", "commands/%s should not retain the old /belayer: namespace", name)
		assert.NotContains(t, string(data), "/problem-create", "commands/%s should not retain the old /problem-create command name", name)
		assert.NotContains(t, string(data), "/problem-list", "commands/%s should not retain the old /problem-list command name", name)
		assert.NotContains(t, string(data), "/problem-brainstorm", "commands/%s should not retain the old /problem-brainstorm command name", name)
		if strings.HasPrefix(name, "blr-research") {
			assert.Contains(t, string(data), "research-notes.md", "commands/%s should mention research-notes.md", name)
			assert.Contains(t, string(data), "research.md", "commands/%s should mention research.md", name)
			assert.Contains(t, string(data), "Next Steps", "commands/%s should include a Next Steps footer", name)
		}
		if name == "blr-research.md" {
			assert.Contains(t, string(data), "superpowers brainstorm skill", "commands/%s should describe the brainstorm backbone", name)
		}
		if name == "blr-research.md" || name == "blr-research-summarize.md" {
			assert.Contains(t, string(data), "/blr-phase-plan", "commands/%s should hand off into the shared phase-planning workflow", name)
		}
		switch name {
		case "blr-phase-plan.md":
			assert.Contains(t, string(data), "phases.md", "commands/%s should write phases.md", name)
			assert.Contains(t, string(data), "~/.belayer/drafts/", "commands/%s should describe the draft root", name)
			assert.Contains(t, string(data), "Next Steps", "commands/%s should include a Next Steps footer", name)
		case "blr-draft-create.md":
			assert.Contains(t, string(data), "spec.md", "commands/%s should mention spec.md", name)
			assert.Contains(t, string(data), "climbs.json", "commands/%s should mention climbs.json", name)
			assert.Contains(t, string(data), "draft_id", "commands/%s should define draft_id", name)
			assert.Contains(t, string(data), "depends_on", "commands/%s should define depends_on", name)
			assert.Contains(t, string(data), "~/.belayer/drafts/", "commands/%s should describe the draft root", name)
			assert.Contains(t, string(data), "Next Steps", "commands/%s should include a Next Steps footer", name)
		case "blr-draft-list.md":
			assert.Contains(t, string(data), "(published)", "commands/%s should describe published dependency rendering", name)
			assert.Contains(t, string(data), "depends_on", "commands/%s should mention depends_on", name)
			assert.Contains(t, string(data), "Next Steps", "commands/%s should include a Next Steps footer", name)
		case "blr-draft-review.md":
			assert.Contains(t, string(data), "belayer problem create", "commands/%s should publish through belayer problem create", name)
			assert.NotContains(t, string(data), "/blr-status <problem-id>", "commands/%s should not advertise unsupported status arguments", name)
			assert.Contains(t, string(data), "Delete the draft directory only after a successful publish", "commands/%s should preserve drafts on publish failure", name)
			assert.Contains(t, string(data), "Next Steps", "commands/%s should include a Next Steps footer", name)
		}
	}

	for _, name := range []string{
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
		_, err := FS.ReadFile("commands/" + name)
		require.Error(t, err, "commands/%s should not remain embedded after the blr rename", name)
	}
}
