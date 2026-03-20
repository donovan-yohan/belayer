// Package role defines the contract types and registry for belayer pipeline roles.
// Each role has a typed contract (Type A or Type B) and belongs to a pipeline phase.
//
// Climbing metaphors:
//   - Type A = "Pitch" (short judgment call, JSON in/out)
//   - Type B = "Ascent" (interactive session, CLI-callback)
//   - Phase = "Section" (approach / ascent / send)
package role

import "time"

// ContractType distinguishes execution models for pipeline roles.
type ContractType string

const (
	// TypeA is a short-lived judgment call: JSON in → process → JSON out.
	// Examples: decomposer, spotter, anchor, pr-creator, pr-manager.
	TypeA ContractType = "pitch"

	// TypeB is a long-lived interactive session with CLI-callback completion.
	// The session calls `belayer <role> finish --task-id <id>` when done.
	// Examples: setter, explorer, lead, pr-reviewer.
	TypeB ContractType = "ascent"
)

// Phase classifies roles into pipeline stages.
type Phase string

const (
	PhaseApproach Phase = "approach" // Intake — "planning the route"
	PhaseAscent   Phase = "ascent"   // Execution — "climbing the wall"
	PhaseSend     Phase = "send"     // Output — "sending it"
)

// RoleDef defines a role in the pipeline.
type RoleDef struct {
	Name         string         `yaml:"name" json:"name"`
	Phase        Phase          `yaml:"phase" json:"phase"`
	ContractType ContractType   `yaml:"contract_type" json:"contract_type"`
	InputSchema  string         `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
	OutputSchema string         `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`
	Provider     ProviderConfig `yaml:"provider" json:"provider"`
	// Multi-repo annotations (fan-out / fan-in).
	FanOut string `yaml:"fan_out,omitempty" json:"fan_out,omitempty"` // e.g., "repos" — output creates per-repo tasks
	Per    string `yaml:"per,omitempty" json:"per,omitempty"`         // e.g., "repo" — runs once per repo
	FanIn  string `yaml:"fan_in,omitempty" json:"fan_in,omitempty"`   // e.g., "repos" — receives all per-repo results
}

// ProviderConfig specifies how a role is executed.
type ProviderConfig struct {
	// Type is "builtin" (Go-native default) or "exec" (external command).
	Type    string            `yaml:"type" json:"type"`
	// Command is the executable to run (for exec providers).
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	// Args are additional arguments passed to the command.
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	// Config holds provider-specific key-value settings.
	Config  map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
}

// LoopConfig defines a retry loop between two roles within a phase.
// Climbing metaphor: a "fall" triggers re-attempt from an earlier pitch.
type LoopConfig struct {
	From          string `yaml:"from" json:"from"`
	To            string `yaml:"to" json:"to"`
	MaxIterations int    `yaml:"max_iterations" json:"max_iterations"`
	Condition     string `yaml:"condition,omitempty" json:"condition,omitempty"`
}

// SafetyConfig holds pipeline-wide safety limits.
type SafetyConfig struct {
	MaxChildDepth     int           `yaml:"max_child_depth" json:"max_child_depth"`
	GlobalChildBudget int           `yaml:"global_child_budget" json:"global_child_budget"`
	ChildDedupe       bool          `yaml:"child_dedupe" json:"child_dedupe"`
	GateTimeout       time.Duration `yaml:"gate_timeout" json:"gate_timeout"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" json:"heartbeat_interval"`
	MaxLoopIterations int           `yaml:"max_loop_iterations" json:"max_loop_iterations"`
}

// DefaultSafetyConfig returns safety limits with sensible defaults.
func DefaultSafetyConfig() SafetyConfig {
	return SafetyConfig{
		MaxChildDepth:     2,
		GlobalChildBudget: 50,
		ChildDedupe:       true,
		GateTimeout:       24 * time.Hour,
		HeartbeatInterval: 60 * time.Second,
		MaxLoopIterations: 3,
	}
}
