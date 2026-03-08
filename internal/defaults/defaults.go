package defaults

import "embed"

//go:embed belayer.toml profiles/*.toml claudemd/*.md
var FS embed.FS
