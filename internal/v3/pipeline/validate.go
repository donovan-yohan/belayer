package pipeline

import (
	"errors"
	"fmt"
	"math"
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
		validOutputTypes := map[string]bool{"file": true, "code": true, "gate_result": true}
		if !validOutputTypes[n.Output.Type] {
			return fmt.Errorf("node %q: output.type must be \"file\", \"code\", or \"gate_result\", got %q", n.Name, n.Output.Type)
		}
		// Enforce consistency between node type and output type.
		if n.IsGate() && n.Output.Type != "gate_result" {
			return fmt.Errorf("gate %q: output.type must be \"gate_result\", got %q", n.Name, n.Output.Type)
		}
		if !n.IsGate() && n.Output.Type == "gate_result" {
			return fmt.Errorf("node %q: output.type \"gate_result\" is only valid on gate nodes", n.Name)
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
		// Gate-specific validation
		if n.IsGate() {
			if len(n.Dimensions) == 0 {
				return fmt.Errorf("gate %q: must have at least one dimension", n.Name)
			}
			var totalWeight float64
			dimNames := make(map[string]bool)
			for _, d := range n.Dimensions {
				if d.Name == "" {
					return fmt.Errorf("gate %q: dimension name is required", n.Name)
				}
				if dimNames[d.Name] {
					return fmt.Errorf("gate %q: duplicate dimension name %q", n.Name, d.Name)
				}
				dimNames[d.Name] = true
				if d.Weight <= 0 {
					return fmt.Errorf("gate %q: dimension %q weight must be positive", n.Name, d.Name)
				}
				totalWeight += d.Weight
			}
			if math.Abs(totalWeight-1.0) > 0.001 {
				return fmt.Errorf("gate %q: dimension weights sum to %.3f, must sum to 1.0", n.Name, totalWeight)
			}
			if n.Thresholds.Pass <= 0 || n.Thresholds.Pass > 10 {
				return fmt.Errorf("gate %q: thresholds.pass must be in (0, 10], got %.1f", n.Name, n.Thresholds.Pass)
			}
			if n.Thresholds.Retry < 0 || n.Thresholds.Retry > 10 {
				return fmt.Errorf("gate %q: thresholds.retry must be in [0, 10], got %.1f", n.Name, n.Thresholds.Retry)
			}
			if n.Thresholds.Retry >= n.Thresholds.Pass {
				return fmt.Errorf("gate %q: thresholds.retry (%.1f) must be less than thresholds.pass (%.1f)", n.Name, n.Thresholds.Retry, n.Thresholds.Pass)
			}
		} else if len(n.Dimensions) > 0 {
			return fmt.Errorf("node %q: dimensions are only valid on gate nodes", n.Name)
		}
	}
	return nil
}
