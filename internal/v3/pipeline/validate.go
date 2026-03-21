package pipeline

import (
	"errors"
	"fmt"
)

// Validate checks a PipelineConfig for structural correctness.
func Validate(cfg *PipelineConfig) error {
	if cfg.Name == "" {
		return errors.New("pipeline name is required")
	}
	if len(cfg.Nodes) == 0 {
		return errors.New("pipeline must have at least one node")
	}

	// Check for duplicate node names.
	seen := make(map[string]bool, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if seen[n.Name] {
			return fmt.Errorf("duplicate node name: %q", n.Name)
		}
		seen[n.Name] = true
	}

	// Validate each node.
	validTransitions := map[string]bool{
		"next": true,
		"stop": true,
		"self": true,
	}
	for _, n := range cfg.Nodes {
		if n.Output.Type == "" {
			return fmt.Errorf("node %q: output.type is required", n.Name)
		}
		if n.Output.Type != "file" && n.Output.Type != "code" {
			return fmt.Errorf("node %q: output.type must be \"file\" or \"code\", got %q", n.Name, n.Output.Type)
		}
		for _, ref := range []struct {
			field string
			value string
		}{
			{"on_pass", n.OnPass},
			{"on_retry", n.OnRetry},
			{"on_fail", n.OnFail},
		} {
			if ref.value == "" {
				continue
			}
			if !validTransitions[ref.value] && !seen[ref.value] {
				return fmt.Errorf("node %q: %s references unknown node or keyword %q", n.Name, ref.field, ref.value)
			}
		}
	}
	return nil
}
