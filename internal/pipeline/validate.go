package pipeline

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// routeNameRe validates route option names (alphanumeric with hyphens).
var routeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

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
		"file":         true,
		"gate_result":  true,
		"commit":       true,
		"pr":           true,
		"route_result": true,
	}
	validNodeTypes := map[NodeType]bool{
		"":            true,
		NodeTypeNode:  true,
		NodeTypeGate:  true,
		NodeTypeAgent: true,
	}
	for _, n := range cfg.Nodes {
		if !validNodeTypes[n.Type] {
			return fmt.Errorf("node %q: unknown type %q (must be \"node\", \"gate\", or \"agent\")", n.Name, n.Type)
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
		// Non-agent nodes with vendor: validate prompt and command exclusivity.
		if n.Vendor != "" && n.Type != NodeTypeAgent {
			if n.Prompt == "" {
				return fmt.Errorf("node %q has vendor %q but no prompt", n.Name, n.Vendor)
			}
			if n.Command != "" {
				return fmt.Errorf("node %q: command and vendor are mutually exclusive", n.Name)
			}
		}
		// Warn if a gate prompt does not include %{INPUT} (rubric injection point).
		// Skip warning for $name refs — the referenced file may contain %{INPUT}.
		if n.IsGate() && n.Prompt != "" && !strings.Contains(n.Prompt, "%{INPUT}") && !strings.HasPrefix(strings.TrimSpace(n.Prompt), "$") {
			fmt.Fprintf(os.Stderr, "warning: gate %q prompt does not contain %%{INPUT} — gate rubric may not be injected\n", n.Name)
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
		// Router-specific validation
		if n.IsRouter() {
			if n.Routes.Mode != "choose_one" {
				return fmt.Errorf("router %q: routes.mode must be \"choose_one\", got %q", n.Name, n.Routes.Mode)
			}
			if len(n.Routes.Options) < 2 {
				return fmt.Errorf("router %q: must have at least 2 route options, got %d", n.Name, len(n.Routes.Options))
			}
			for optName, opt := range n.Routes.Options {
				if !routeNameRe.MatchString(optName) {
					return fmt.Errorf("router %q: option name %q must match pattern ^[a-zA-Z0-9][a-zA-Z0-9-]*$", n.Name, optName)
				}
				if opt.Pipeline == "" {
					return fmt.Errorf("router %q: option %q must have a non-empty pipeline path", n.Name, optName)
				}
			}
			if n.Output.Type != "route_result" {
				return fmt.Errorf("router %q: output.type must be \"route_result\" when routes are present, got %q", n.Name, n.Output.Type)
			}
			if n.IsGate() {
				return fmt.Errorf("router %q: routes and dimensions are mutually exclusive — use one or the other", n.Name)
			}
			if n.Type != NodeTypeAgent {
				return fmt.Errorf("router %q: router nodes must have type \"agent\" (got %q)", n.Name, n.Type)
			}
			if n.OnRetry != "" && n.OnRetry != "self" {
				return fmt.Errorf("router %q: on_retry must be \"self\" or empty, got %q", n.Name, n.OnRetry)
			}
		} else if n.Output.Type == "route_result" {
			return fmt.Errorf("node %q: output.type \"route_result\" is only valid on router nodes (add routes: with options)", n.Name)
		}
	}
	// Validate intake configs (must be after node loop — uses `seen` for node name lookup).
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

// ValidateRoutes parses and validates every subpipeline referenced by router nodes.
// It must be called at all entrypoints (climb, worker) after Validate() succeeds.
func ValidateRoutes(cfg *PipelineConfig, workDir string) error {
	_, err := ResolveSubpipelineYAMLs(cfg, workDir)
	return err
}

// ResolveSubpipelineYAMLs validates and pre-reads all subpipeline YAML files for router nodes.
// Returns a map from route option name to raw YAML bytes. This snapshot is passed into
// ClimbInput.SubpipelineYAMLs so the Temporal workflow can resolve subpipelines
// deterministically without file I/O.
func ResolveSubpipelineYAMLs(cfg *PipelineConfig, workDir string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for _, n := range cfg.Nodes {
		if !n.IsRouter() {
			continue
		}
		for optName, opt := range n.Routes.Options {
			subPath := filepath.Join(workDir, opt.Pipeline)
			data, err := os.ReadFile(subPath)
			if err != nil {
				return nil, fmt.Errorf("router %q option %q: cannot read subpipeline %q: %w", n.Name, optName, opt.Pipeline, err)
			}
			subCfg, err := ParsePipeline(data)
			if err != nil {
				return nil, fmt.Errorf("router %q option %q: failed to parse subpipeline %q: %w", n.Name, optName, opt.Pipeline, err)
			}
			if err := Validate(subCfg); err != nil {
				return nil, fmt.Errorf("router %q option %q: subpipeline %q is invalid: %w", n.Name, optName, opt.Pipeline, err)
			}
			result[optName] = data
		}
	}
	return result, nil
}
