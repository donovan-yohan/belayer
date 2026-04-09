package session

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

var safeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Phase represents the three-phase model of a belayer session.
type Phase string

const (
	PhaseIntake    Phase = "intake"
	PhaseImplement Phase = "implement"
	PhaseDeliver   Phase = "deliver"
)

// AgentSpec describes a single agent within a session template.
type AgentSpec struct {
	Name         string            `yaml:"name" json:"name"`
	Vendor       string            `yaml:"vendor" json:"vendor"`
	Model        string            `yaml:"model" json:"model"`
	Repo         string            `yaml:"repo,omitempty" json:"repo,omitempty"` // optional repo name from environment config
	Role         string            `yaml:"role,omitempty" json:"role,omitempty"` // human-readable description of what this agent does
	SystemPrompt string            `yaml:"system_prompt" json:"system_prompt"`
	MCPConfig    string            `yaml:"mcp_config,omitempty" json:"mcp_config,omitempty"`
	Settings     string            `yaml:"settings,omitempty" json:"settings,omitempty"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// SessionTemplate describes the agents for a session phase.
type SessionTemplate struct {
	Name        string      `yaml:"name" json:"name"`
	Phase       Phase       `yaml:"phase" json:"phase"`
	Description string      `yaml:"description" json:"description"`
	Agents      []AgentSpec `yaml:"agents" json:"agents"`
}

// LoadTemplate loads a session template by name. It checks the workspace
// templates directory first (.belayer/templates/<name>.yaml), then falls
// back to built-in defaults. This lets users customize templates by editing
// the YAML files that belayer setup writes.
func LoadTemplate(name string) (SessionTemplate, error) {
	return LoadTemplateFromDir("", name)
}

// LoadTemplateFromDir loads a template, checking templatesDir first.
// If templatesDir is empty, only built-in defaults are used.
func LoadTemplateFromDir(templatesDir, name string) (SessionTemplate, error) {
	// Try workspace YAML first.
	if templatesDir != "" {
		path := filepath.Join(templatesDir, name+".yaml")
		if tmpl, err := LoadTemplateFile(path); err == nil {
			return tmpl, nil
		}
	}

	// Fall back to built-in defaults.
	src, ok := builtinTemplates[name]
	if !ok {
		return SessionTemplate{}, fmt.Errorf("session: unknown template %q", name)
	}
	agents := make([]AgentSpec, len(src.Agents))
	copy(agents, src.Agents)
	tmpl := src
	tmpl.Agents = agents
	return tmpl, nil
}

// LoadTemplateFile reads and parses a single YAML template file.
func LoadTemplateFile(path string) (SessionTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionTemplate{}, fmt.Errorf("session: read template %q: %w", path, err)
	}
	var tmpl SessionTemplate
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return SessionTemplate{}, fmt.Errorf("session: parse template %q: %w", path, err)
	}
	return tmpl, nil
}

// ListTemplates returns the names of all built-in templates in sorted order.
func ListTemplates() []string {
	names := make([]string, 0, len(builtinTemplates))
	for name := range builtinTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// WriteDefaultTemplates writes all built-in templates as YAML files to dir.
// Existing files are not overwritten.
func WriteDefaultTemplates(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("session: create templates dir: %w", err)
	}
	for name, tmpl := range builtinTemplates {
		path := filepath.Join(dir, name+".yaml")
		if _, err := os.Stat(path); err == nil {
			continue // don't overwrite existing
		}
		data, err := yaml.Marshal(tmpl)
		if err != nil {
			return fmt.Errorf("session: marshal template %q: %w", name, err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return fmt.Errorf("session: write template %q: %w", name, err)
		}
	}
	return nil
}

// ValidateTemplate checks basic sanity of a SessionTemplate.
func ValidateTemplate(tmpl SessionTemplate) error {
	if tmpl.Name == "" {
		return fmt.Errorf("session: template name is required")
	}
	if len(tmpl.Agents) == 0 {
		return fmt.Errorf("session: template %q must have at least one agent", tmpl.Name)
	}
	for _, a := range tmpl.Agents {
		if a.Name == "" {
			return fmt.Errorf("session: template %q has an agent with no name", tmpl.Name)
		}
		if a.Vendor == "" {
			return fmt.Errorf("session: template %q agent %q has no vendor", tmpl.Name, a.Name)
		}
		if !safeNamePattern.MatchString(a.Name) {
			return fmt.Errorf("session: template %q agent %q has invalid name (must be alphanumeric with ._- allowed)", tmpl.Name, a.Name)
		}
		for k := range a.Env {
			for _, c := range k {
				if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
					return fmt.Errorf("session: template %q agent %q env key %q contains invalid characters", tmpl.Name, a.Name, k)
				}
			}
		}
	}
	return nil
}

// built-in templates keyed by name — the defaults written by belayer setup.
var builtinTemplates = map[string]SessionTemplate{
	"intake": {
		Name:        "intake",
		Phase:       PhaseIntake,
		Description: "Intake — single agent generates spec from idea",
		Agents: []AgentSpec{
			{
				Name:         "explorer",
				Vendor:       "claude",
				Model:        "sonnet",
				Role:         "Exploration and spec generation",
				SystemPrompt: "You are an exploration agent. Analyze the idea, research the codebase, and produce a detailed implementation spec. The spec should include scope, approach, key files to modify, edge cases, and a testing strategy.",
			},
		},
	},
	"implement": {
		Name:        "implement",
		Phase:       PhaseImplement,
		Description: "Implementation — pilot orchestrates team to complete work",
		Agents: []AgentSpec{
			{
				Name:   "pilot",
				Vendor: "claude",
				Model:  "opus",
				Role:   "Orchestrator — decomposes work, delegates to team, interprets results",
				SystemPrompt: `You are the pilot agent for this belayer session. You orchestrate — you do NOT write code.
You decompose work, delegate to your team, interpret results, and decide what happens next.

How you coordinate your team is your judgment. You may discover effective patterns over time — write observations to your memory so future sessions benefit.

When delegating, provide enough context that your agents can succeed without asking clarifying questions: relevant file paths, architectural constraints, what has already been tried, and what success looks like.`,
			},
			{
				Name:   "implementer",
				Vendor: "opencode",
				Model:  "opencode/claude-sonnet-4-6",
				Role:   "Code implementation — writes and modifies code as directed",
				SystemPrompt: `You are an implementer. Write code as directed. Focus on correctness, completeness, and clean implementation.

When you finish a task, summarize what you changed and any decisions you made so the pilot can coordinate next steps.`,
			},
			{
				Name:   "reviewer",
				Vendor: "opencode",
				Model:  "opencode/gpt-5.1-codex",
				Role:   "Code review — evaluates changes for correctness, style, and completeness",
				SystemPrompt: `You are the code reviewer. Review changes for correctness, style, and completeness. Provide structured, actionable feedback.

Be specific about what needs to change and why. Distinguish between blocking issues and suggestions.`,
			},
		},
	},
	"deliver": {
		Name:        "deliver",
		Phase:       PhaseDeliver,
		Description: "Deliver — QA validation, merge, and monitoring",
		Agents: []AgentSpec{
			{
				Name:   "qa",
				Vendor: "claude",
				Model:  "sonnet",
				Role:   "QA validation — runs tests and validates the implementation",
				SystemPrompt: `You are the QA agent. Run the test suite, validate the implementation against the spec, and report any failures or gaps.

Be thorough but practical — focus on correctness and regressions, not style.`,
			},
			{
				Name:   "merger",
				Vendor: "claude",
				Model:  "sonnet",
				Role:   "Merge logistics — creates PR and handles merge",
				SystemPrompt: `You are the merge agent. Create the pull request with a clear description, ensure CI passes, and handle merge logistics.`,
			},
		},
	},
}
