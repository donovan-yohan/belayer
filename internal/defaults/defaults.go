package defaults

import "embed"

//go:embed belayer.toml profiles/*.toml claudemd/*.md commands/*.md
var FS embed.FS
