package defaults

import "embed"

//go:embed belayer.toml prompts/*.md profiles/*.toml claudemd/*.md
var FS embed.FS
