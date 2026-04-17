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

// DefaultBridge is the hermes_bridge Python package copied into a project's
// .belayer/hermes_bridge/ directory by `belayer init`. Unlike DefaultAgents,
// this copy is machine-generated and gitignored — it is always overwritten
// on init so the extracted bridge matches the binary version. The extractor
// filters __pycache__/ and *.pyc entries at walk time to avoid leaking host
// bytecode into projects.
//
//go:embed all:hermes_bridge
var DefaultBridge embed.FS
