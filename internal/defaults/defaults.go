package defaults

import "embed"

//go:embed belayer.toml prompts/*.md profiles/*.toml
var FS embed.FS
