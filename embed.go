// Package belayer holds embedded assets that ship inside the belayer binary.
// Code lives under cmd/ and internal/; this file exists so go:embed can reach
// the top-level agents/ directory without escaping a package boundary.
package belayer

import "embed"

// DefaultAgents is the set of identity templates copied into a project's
// .belayer/agents/ directory by `belayer init`. The "all:" prefix preserves
// dot-files in case an agent ever ships hidden config; today there are none.
//
//go:embed all:agents
var DefaultAgents embed.FS
