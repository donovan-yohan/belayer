// Package pipeline defines the pipeline DSL model and parser for belayer v2.
// A pipeline is called a "Route" (climbing metaphor) and consists of phases
// (Approach, Ascent, Send) each containing roles with typed contracts.
package pipeline

import "github.com/donovan-yohan/belayer/internal/v2/role"

// Route is the top-level pipeline definition — a climbing route.
type Route struct {
	Name    string            `yaml:"name" json:"name"`
	Extends string            `yaml:"extends,omitempty" json:"extends,omitempty"` // Parent pipeline to inherit from
	Repos   []string          `yaml:"repos,omitempty" json:"repos,omitempty"`     // Registered repo names for multi-repo
	Phases  []PhaseConfig     `yaml:"phases" json:"phases"`
	Safety  role.SafetyConfig `yaml:"safety" json:"safety"`
}

// IsMultiRepo returns true if the pipeline targets multiple repos.
func (r *Route) IsMultiRepo() bool {
	return len(r.Repos) > 1
}

// PhaseConfig defines one pipeline phase (approach / ascent / send).
type PhaseConfig struct {
	Phase role.Phase     `yaml:"phase" json:"phase"`
	Roles []role.RoleDef `yaml:"roles" json:"roles"`
	Loops []role.LoopConfig `yaml:"loops,omitempty" json:"loops,omitempty"`
}

// AllRoles returns a flat list of all roles across all phases, in order.
func (r *Route) AllRoles() []role.RoleDef {
	var roles []role.RoleDef
	for _, p := range r.Phases {
		roles = append(roles, p.Roles...)
	}
	return roles
}

// FindRole returns the role definition with the given name, or nil.
func (r *Route) FindRole(name string) *role.RoleDef {
	for i := range r.Phases {
		for j := range r.Phases[i].Roles {
			if r.Phases[i].Roles[j].Name == name {
				return &r.Phases[i].Roles[j]
			}
		}
	}
	return nil
}

// AllLoops returns a flat list of all loop configs across all phases.
func (r *Route) AllLoops() []role.LoopConfig {
	var loops []role.LoopConfig
	for _, p := range r.Phases {
		loops = append(loops, p.Loops...)
	}
	return loops
}
