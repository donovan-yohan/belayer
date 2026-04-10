package agent

// ToolSpec defines a tool with execution routing for the daemon.
// It extends the basic Tool struct with target-aware execution configuration.
type ToolSpec struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	InputSchema map[string]string `yaml:"input" json:"input"`           // field name → type
	Exec        ToolExec          `yaml:"exec" json:"exec"`
	Constraints ToolConstraints   `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

// ToolExec holds execution routing configuration for a ToolSpec.
type ToolExec struct {
	// Target is the execution target. Valid values:
	//   "agent"     — run in the agent's own container (docker compose exec)
	//   "workbench" — run in the shared workbench container (docker compose exec)
	//   "infra"     — run in the infra container (docker compose exec)
	//   "host"      — run directly on the host (opt-in only, use with care)
	Target  string `yaml:"target" json:"target"`
	Command string `yaml:"command" json:"command"`           // Go template with {{.field}} placeholders
	Timeout int    `yaml:"timeout,omitempty" json:"timeout,omitempty"` // seconds, default 60
}

// ToolConstraints expresses optional safety constraints on a tool.
type ToolConstraints struct {
	ReadOnly bool `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	Audit    bool `yaml:"audit,omitempty" json:"audit,omitempty"`
}

// ValidTargets is the set of allowed execution targets.
var ValidTargets = map[string]bool{
	"agent":     true,
	"workbench": true,
	"infra":     true,
	"host":      true,
}

// EffectiveTimeout returns the configured timeout in seconds, defaulting to 60.
func (e ToolExec) EffectiveTimeout() int {
	if e.Timeout <= 0 {
		return 60
	}
	return e.Timeout
}
