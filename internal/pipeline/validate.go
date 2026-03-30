package pipeline

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
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
	validOutputTypes := map[string]bool{
		"file":        true,
		"gate_result": true,
		"commit":      true,
		"pr":          true,
	}
	validNodeTypes := map[NodeType]bool{
		"":            true,
		NodeTypeNode:  true,
		NodeTypeGate:  true,
		NodeTypeAgent: true,
	}
	for _, n := range cfg.Nodes {
		if !validNodeTypes[n.Type] {
			return fmt.Errorf("node %q: unknown type %q (must be \"node\" or \"gate\")", n.Name, n.Type)
		}
		// Agent node validation: vendor and prompt required, command mutually exclusive.
		if n.Type == NodeTypeAgent {
			if n.Vendor == "" {
				return fmt.Errorf("agent %q: vendor is required (e.g. claude, codex)", n.Name)
			}
			if n.Prompt == "" {
				return fmt.Errorf("agent %q: prompt is required", n.Name)
			}
			if n.Command != "" {
				return fmt.Errorf("agent %q: command and vendor are mutually exclusive — use one or the other", n.Name)
			}
		}
		if n.Output.Type == "" {
			return fmt.Errorf("node %q: output.type is required", n.Name)
		}
		if !validOutputTypes[n.Output.Type] {
			valid := make([]string, 0, len(validOutputTypes))
			for k := range validOutputTypes {
				valid = append(valid, fmt.Sprintf("%q", k))
			}
			sort.Strings(valid)
			return fmt.Errorf("node %q: output.type must be one of [%s], got %q", n.Name, strings.Join(valid, ", "), n.Output.Type)
		}
		// Enforce consistency between node type and output type.
		// Gate nodes and agent nodes with dimensions both produce gate_result.
		if n.IsGate() && n.Output.Type != "gate_result" {
			return fmt.Errorf("gate %q: output.type must be \"gate_result\", got %q", n.Name, n.Output.Type)
		}
		if !n.IsGate() && n.Output.Type == "gate_result" {
			return fmt.Errorf("node %q: output.type \"gate_result\" is only valid on gate or agent nodes with dimensions", n.Name)
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
			if n.Thresholds.Retry < 0 {
				return fmt.Errorf("gate %q: thresholds.retry must be non-negative, got %.1f", n.Name, n.Thresholds.Retry)
			}
			if n.Thresholds.Retry >= n.Thresholds.Pass {
				return fmt.Errorf("gate %q: thresholds.retry (%.1f) must be less than thresholds.pass (%.1f)", n.Name, n.Thresholds.Retry, n.Thresholds.Pass)
			}
		} else if len(n.Dimensions) > 0 && n.Type != NodeTypeAgent {
			return fmt.Errorf("node %q: dimensions are only valid on gate or agent nodes", n.Name)
		}
	}
	// Validate intake configs.
	validIntakeTypes := map[string]bool{
		"jira":        true,
		"interactive": true,
		"trigger":     true,
	}
	intakeNames := make(map[string]bool)
	interactiveCount := 0
	for _, intake := range cfg.Intake {
		if intake.Name == "" {
			return fmt.Errorf("intake: name is required")
		}
		if intakeNames[intake.Name] {
			return fmt.Errorf("intake: duplicate name %q", intake.Name)
		}
		intakeNames[intake.Name] = true
		if !validIntakeTypes[intake.Type] {
			return fmt.Errorf("intake %q: unknown type %q", intake.Name, intake.Type)
		}
		if intake.Type == "interactive" {
			interactiveCount++
			if interactiveCount > 1 {
				return fmt.Errorf("intake: only one interactive intake allowed")
			}
		}
	}
	return nil
}
