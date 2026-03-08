package defaults

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedFilesExist(t *testing.T) {
	files := []string{
		"belayer.toml",
		"prompts/lead.md",
		"prompts/spotter.md",
		"prompts/anchor.md",
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
