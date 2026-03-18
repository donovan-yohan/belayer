package defaults

import (
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
	for _, name := range []string{"lead.md", "spotter.md", "anchor.md", "setter.md"} {
		data, err := FS.ReadFile("claudemd/" + name)
		require.NoError(t, err, "claudemd/%s should be embedded", name)
		assert.NotEmpty(t, data)
	}
}

func TestCommandFilesExist(t *testing.T) {
	for _, name := range []string{
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
	} {
		data, err := FS.ReadFile("commands/" + name)
		require.NoError(t, err, "commands/%s should be embedded", name)
		assert.NotEmpty(t, data)
		assert.NotContains(t, string(data), "{{.CragName}}", "commands/%s should not contain unresolved template placeholders", name)
		assert.NotContains(t, string(data), "/belayer:", "commands/%s should not retain the old /belayer: namespace", name)
		assert.NotContains(t, string(data), "/problem-create", "commands/%s should not retain the old /problem-create command name", name)
		assert.NotContains(t, string(data), "/problem-list", "commands/%s should not retain the old /problem-list command name", name)
		assert.NotContains(t, string(data), "/problem-brainstorm", "commands/%s should not retain the old /problem-brainstorm command name", name)
	}

	for _, name := range []string{
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
		_, err := FS.ReadFile("commands/" + name)
		require.Error(t, err, "commands/%s should not remain embedded after the blr rename", name)
	}
}
