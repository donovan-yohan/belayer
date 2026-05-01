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

// TalentCatalog is the local-first example catalog used by `belayer talent`
// commands. Development currently aliases the shipped default agents; story
// identities live under examples/talent-catalog/story/.
//
//go:embed all:examples/talent-catalog
var TalentCatalog embed.FS

// DefaultBridge is the hermes_bridge Python package copied into a project's
// .belayer/hermes_bridge/ directory by `belayer init`. Unlike DefaultAgents,
// this copy is machine-generated and gitignored — it is always overwritten
// on init so the extracted bridge matches the binary version. The extractor
// filters __pycache__/ and *.pyc entries at walk time to avoid leaking host
// bytecode into projects.
//
//go:embed all:hermes_bridge
var DefaultBridge embed.FS

// WebUI is the zero-build web dashboard shipped inside the belayer binary.
// Served at /ui/ when the daemon is running.
//
//go:embed all:web
var WebUI embed.FS

// DefaultPlugins is the Hermes 0.12+ plugin tree shipped inside the belayer
// binary. Extracted to $HERMES_HOME/plugins/ at daemon startup so the
// Hermes plugin loader (which scans that directory) can discover them.
// The extractor filters __pycache__/ and *.pyc to avoid leaking host
// bytecode into the user's plugin directory.
//
//go:embed plugins/belayer/*.py plugins/belayer/plugin.yaml
var DefaultPlugins embed.FS
