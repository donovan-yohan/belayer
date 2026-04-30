package gates

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	SchemaVersion = "belayer-gate/v1"

	AcceptanceName        = "acceptance"
	CompletionRequested   = "completion_requested"
	DefaultAcceptanceRole = "pm"
)

type Gate struct {
	SchemaVersion  string   `json:"schema_version,omitempty" yaml:"schema_version,omitempty"`
	Name           string   `json:"name" yaml:"name"`
	Stage          string   `json:"stage" yaml:"stage"`
	Authority      string   `json:"authority" yaml:"authority"`
	Trigger        string   `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	AssignedTalent []string `json:"assigned_talent" yaml:"assigned_talent"`
	Requires       []string `json:"requires,omitempty" yaml:"requires,omitempty"`
	InputArtifacts []string `json:"input_artifacts,omitempty" yaml:"input_artifacts,omitempty"`
	Conditions     []string `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	OutputArtifact string   `json:"output_artifact,omitempty" yaml:"output_artifact,omitempty"`
	Verdicts       []string `json:"verdicts" yaml:"verdicts"`
}

type configFile struct {
	Gates []Gate `yaml:"gates"`
}

func BuiltInAcceptance() Gate {
	return Gate{
		SchemaVersion: SchemaVersion,
		Name:          AcceptanceName,
		Stage:         "session",
		Authority:     "blocking",
		Trigger:       CompletionRequested,
		AssignedTalent: []string{
			DefaultAcceptanceRole,
		},
		Requires: []string{
			"spec-or-task",
			"registered-artifacts",
		},
		Conditions: []string{
			"The delivered work satisfies the original task or spec",
			"All configured exit conditions have observable evidence",
		},
		OutputArtifact: "gate-result",
		Verdicts: []string{
			"pass",
			"fail",
			"blocked",
		},
	}
}

func AcceptanceFromConfig(raw []byte) (Gate, bool, error) {
	var cfg configFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Gate{}, false, fmt.Errorf("parse gates config: %w", err)
	}
	for _, gate := range cfg.Gates {
		if gate.isAcceptanceGate() {
			gate.applyDefaults()
			return gate, true, nil
		}
	}
	return Gate{}, false, nil
}

func (g Gate) PrimaryTalent() string {
	for _, talent := range g.AssignedTalent {
		if talent = strings.TrimSpace(talent); talent != "" {
			return talent
		}
	}
	return DefaultAcceptanceRole
}

func (g Gate) isAcceptanceGate() bool {
	name := strings.TrimSpace(strings.ToLower(g.Name))
	trigger := strings.TrimSpace(strings.ToLower(g.Trigger))
	return name == AcceptanceName && (trigger == "" || trigger == CompletionRequested)
}

func (g *Gate) applyDefaults() {
	if strings.TrimSpace(g.SchemaVersion) == "" {
		g.SchemaVersion = SchemaVersion
	}
	if strings.TrimSpace(g.Stage) == "" {
		g.Stage = "session"
	}
	if strings.TrimSpace(g.Authority) == "" {
		g.Authority = "blocking"
	}
	if strings.TrimSpace(g.Trigger) == "" {
		g.Trigger = CompletionRequested
	}
	if strings.TrimSpace(g.OutputArtifact) == "" {
		g.OutputArtifact = "gate-result"
	}
	if len(g.AssignedTalent) == 0 {
		g.AssignedTalent = []string{DefaultAcceptanceRole}
	}
	if len(g.Verdicts) == 0 {
		g.Verdicts = []string{"pass", "fail", "blocked"}
	}
}
