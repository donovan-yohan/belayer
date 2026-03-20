package pipeline

import (
	"fmt"
	"strings"
)

// Validate checks the Route for structural correctness:
// - At least one phase with at least one role
// - All role names are unique
// - Loop targets reference existing roles within the same phase
// - No circular loops (A→B and B→A in same phase)
// - Safety config values are positive
func Validate(route *Route) []string {
	var errs []string

	if len(route.Phases) == 0 {
		errs = append(errs, "pipeline must have at least one phase")
		return errs
	}

	// Collect all role names for uniqueness check.
	roleNames := make(map[string]bool)
	for _, phase := range route.Phases {
		if len(phase.Roles) == 0 {
			errs = append(errs, fmt.Sprintf("phase %q has no roles", phase.Phase))
		}
		for _, r := range phase.Roles {
			if r.Name == "" {
				errs = append(errs, "role name cannot be empty")
				continue
			}
			if roleNames[r.Name] {
				errs = append(errs, fmt.Sprintf("duplicate role name: %q", r.Name))
			}
			roleNames[r.Name] = true
		}
	}

	// Validate loops.
	for _, phase := range route.Phases {
		phaseRoles := make(map[string]bool)
		for _, r := range phase.Roles {
			phaseRoles[r.Name] = true
		}

		loopEdges := make(map[string]string) // from → to
		for _, loop := range phase.Loops {
			if !phaseRoles[loop.From] {
				errs = append(errs, fmt.Sprintf("loop from %q: role not found in phase %q", loop.From, phase.Phase))
			}
			if !phaseRoles[loop.To] {
				errs = append(errs, fmt.Sprintf("loop to %q: role not found in phase %q", loop.To, phase.Phase))
			}
			if loop.MaxIterations < 0 {
				errs = append(errs, fmt.Sprintf("loop %s→%s: max_iterations must be non-negative", loop.From, loop.To))
			}

			// Circular loop detection: A→B and B→A.
			if prev, exists := loopEdges[loop.To]; exists && prev == loop.From {
				errs = append(errs, fmt.Sprintf("circular loop: %s→%s and %s→%s", loop.From, loop.To, loop.To, loop.From))
			}
			loopEdges[loop.From] = loop.To
		}
	}

	// Validate safety config.
	if route.Safety.MaxChildDepth < 0 {
		errs = append(errs, "safety.max_child_depth must be non-negative")
	}
	if route.Safety.GlobalChildBudget < 0 {
		errs = append(errs, "safety.global_child_budget must be non-negative")
	}

	return errs
}

// ValidateOrError returns an error if validation fails, nil if valid.
func ValidateOrError(route *Route) error {
	errs := Validate(route)
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("pipeline validation failed:\n  - %s", strings.Join(errs, "\n  - "))
}
