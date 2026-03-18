package defaults

import "embed"

//go:embed belayer.toml profiles/*.toml claudemd/*.md commands/*.md
var FS embed.FS

//go:embed personas/*.toml
var PersonaTemplates embed.FS
