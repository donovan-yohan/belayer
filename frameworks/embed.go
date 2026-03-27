package frameworks

import "embed"

// BuiltinFS contains all built-in framework directories.
//
//go:embed all:claude-tmux
var BuiltinFS embed.FS
