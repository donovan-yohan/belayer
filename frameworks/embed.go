package frameworks

import "embed"

// BuiltinFS contains all built-in framework directories.
//
//go:embed all:gstack
var BuiltinFS embed.FS
