package session

import "fmt"

// Phase represents the three-phase model of a belayer session.
type Phase string

const (
	PhaseExplore Phase = "explore"
	PhaseClimb   Phase = "climb"
	PhaseSummit  Phase = "summit"
)

// AgentSpec describes a single agent within a session template.
// Role is kept as a plain string so this package remains self-contained
// and does not import internal/agent (built concurrently).
type AgentSpec struct {
	Name         string `json:"name"`
	Role         string `json:"role"`          // pilot, implementer, reviewer
	Vendor       string `json:"vendor"`        // claude, codex, opencode
	Model        string `json:"model"`         // opus, sonnet, etc.
	SystemPrompt string `json:"system_prompt"`
}

// SessionTemplate describes the agents and constraints for a session phase.
type SessionTemplate struct {
	Name        string      `json:"name"`
	Phase       Phase       `json:"phase"`
	Description string      `json:"description"`
	Agents      []AgentSpec `json:"agents"`
	MinAgents   int         `json:"min_agents"`
}

// built-in templates keyed by name.
var builtinTemplates = map[string]SessionTemplate{
	"explore": {
		Name:        "explore",
		Phase:       PhaseExplore,
		Description: "Intake — single agent generates spec from idea",
		Agents: []AgentSpec{
			{
				Name:         "explorer",
				Role:         "implementer",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are an exploration agent. Analyze the idea and produce a detailed spec.",
			},
		},
		MinAgents: 1,
	},
	"climb": {
		Name:        "climb",
		Phase:       PhaseClimb,
		Description: "Implementation — pilot + implementer + reviewer trio",
		Agents: []AgentSpec{
			{
				Name:         "pilot",
				Role:         "pilot",
				Vendor:       "claude",
				Model:        "opus",
				SystemPrompt: "You are the pilot agent. Coordinate the implementer and reviewer. You do NOT write code.",
			},
			{
				Name:         "implementer",
				Role:         "implementer",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are the implementer. Write code as directed by the pilot.",
			},
			{
				Name:         "reviewer",
				Role:         "reviewer",
				Vendor:       "codex",
				Model:        "",
				SystemPrompt: "You are the code reviewer. Review changes for correctness, style, and completeness.",
			},
		},
		MinAgents: 3,
	},
	"summit": {
		Name:        "summit",
		Phase:       PhaseSummit,
		Description: "Output — QA validation and merge",
		Agents: []AgentSpec{
			{
				Name:         "qa",
				Role:         "implementer",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are the QA agent. Run tests and validate the implementation.",
			},
			{
				Name:         "merger",
				Role:         "implementer",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are the merge agent. Create the PR and handle merge logistics.",
			},
		},
		MinAgents: 2,
	},
}

// LoadTemplate returns the built-in SessionTemplate with the given name.
// The returned template is a deep copy; callers may safely mutate its Agents slice.
// Returns an error if the name is not recognised.
func LoadTemplate(name string) (SessionTemplate, error) {
	src, ok := builtinTemplates[name]
	if !ok {
		return SessionTemplate{}, fmt.Errorf("session: unknown template %q", name)
	}
	// Return a copy with an independent agents slice so callers cannot
	// mutate the global backing array through slice aliasing.
	agents := make([]AgentSpec, len(src.Agents))
	copy(agents, src.Agents)
	tmpl := src
	tmpl.Agents = agents
	return tmpl, nil
}

// ListTemplates returns the names of all built-in templates in sorted order.
func ListTemplates() []string {
	return []string{"climb", "explore", "summit"}
}

// ValidateTemplate checks that a SessionTemplate satisfies its own constraints
// and, for the Climb phase, that the pilot-always-present invariant holds.
func ValidateTemplate(tmpl SessionTemplate) error {
	// The Climb phase enforces the trio invariant: pilot + implementer + reviewer
	// must all be present. Self-review is unreliable; different vendors for
	// implementation and review enforce real independence.
	// Role checks run before the MinAgents check so callers receive a precise
	// error ("missing pilot") rather than a generic count mismatch.
	if tmpl.Phase == PhaseClimb {
		var hasPilot, hasImplementer, hasReviewer bool
		for _, a := range tmpl.Agents {
			switch a.Role {
			case "pilot":
				hasPilot = true
			case "implementer":
				hasImplementer = true
			case "reviewer":
				hasReviewer = true
			}
		}
		if !hasPilot {
			return fmt.Errorf(
				"session: template %q (climb phase) must include at least one agent with role \"pilot\"",
				tmpl.Name,
			)
		}
		if !hasImplementer {
			return fmt.Errorf(
				"session: template %q (climb phase) must include at least one agent with role \"implementer\"",
				tmpl.Name,
			)
		}
		if !hasReviewer {
			return fmt.Errorf(
				"session: template %q (climb phase) must include at least one agent with role \"reviewer\"",
				tmpl.Name,
			)
		}
	}

	if len(tmpl.Agents) < tmpl.MinAgents {
		return fmt.Errorf(
			"session: template %q requires at least %d agent(s), got %d",
			tmpl.Name, tmpl.MinAgents, len(tmpl.Agents),
		)
	}

	return nil
}
