// Package vendor resolves agent CLI commands from vendor names.
// It maps vendor identifiers (claude, codex, gemini, opencode) to the
// specific CLI invocations needed to run them headlessly with streaming output.
package vendor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

// SchemaConfig specifies structured output schema requirements for a vendor command.
type SchemaConfig struct {
	IsGate     bool
	Dimensions []pipeline.DimensionConfig
	IsRouter   bool
	RouteNames []string // enum values for route constraint
}

// Config holds the resolved CLI command and args for a vendor.
type Config struct {
	Command string
	Args    []string
	// SchemaMode indicates how the vendor accepts structured output schemas.
	// "inline" means the schema is passed as a CLI flag value (claude).
	// "file" means the schema must be written to a temp file (codex).
	SchemaMode string
	// SchemaFlag is the CLI flag name for structured output.
	SchemaFlag string
}

var registry = map[string]Config{
	"claude": {
		Command:    "claude",
		Args:       []string{"-p", "--dangerously-skip-permissions", "--output-format", "stream-json"},
		SchemaMode: "inline",
		SchemaFlag: "--json-schema",
	},
	"codex": {
		Command:    "codex",
		Args:       []string{"exec", "-s", "read-only", "--json"},
		SchemaMode: "file",
		SchemaFlag: "--output-schema",
	},
}

// Resolve returns the Config for a named vendor.
func Resolve(name string) (Config, error) {
	cfg, ok := registry[name]
	if !ok {
		known := make([]string, 0, len(registry))
		for k := range registry {
			known = append(known, k)
		}
		return Config{}, fmt.Errorf("unknown vendor %q (known: %s)", name, strings.Join(known, ", "))
	}
	return cfg, nil
}

// KnownVendors returns the list of registered vendor names.
func KnownVendors() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}

// GateResultSchema returns the JSON Schema for gate result output.
// Dimensions are embedded from the pipeline config so the schema
// constrains the model's output to exactly the expected dimension names.
func GateResultSchema(dimensions []pipeline.DimensionConfig) map[string]any {
	dimProps := make(map[string]any, len(dimensions))
	dimRequired := make([]string, 0, len(dimensions))
	for _, d := range dimensions {
		dimProps[d.Name] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"score": map[string]any{
					"type":        "number",
					"minimum":     0,
					"maximum":     10,
					"description": d.Description,
				},
				"issues": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
				"rationale": map[string]any{
					"type": "string",
				},
			},
			"required": []string{"score", "rationale"},
		}
		dimRequired = append(dimRequired, d.Name)
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dimensions": map[string]any{
				"type":       "object",
				"properties": dimProps,
				"required":   dimRequired,
			},
			"summary": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"dimensions", "summary"},
	}
}

// RouteResultSchema returns the JSON Schema for route result output.
// RouteNames are embedded as an enum constraint so the model can only
// choose from declared route options.
func RouteResultSchema(routeNames []string) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"route", "confidence", "reasoning", "rejected"},
		"properties": map[string]any{
			"route": map[string]any{
				"type": "string",
				"enum": routeNames,
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"reasoning": map[string]any{
				"type": "string",
			},
			"rejected": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"route", "reason"},
					"properties": map[string]any{
						"route":  map[string]any{"type": "string"},
						"reason": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// BuildCommand constructs the full shell command for running an agent.
// For gate nodes, it adds structured output schema handling based on the vendor's SchemaMode.
// The returned command is meant to be run via sh -c.
func BuildCommand(vendorName, prompt, workDir string, schema SchemaConfig) (string, func(), error) {
	cfg, err := Resolve(vendorName)
	if err != nil {
		return "", nil, err
	}

	args := make([]string, len(cfg.Args))
	copy(args, cfg.Args)

	var cleanup func()

	// Add structured output schema for gate or router nodes.
	var schemaJSON []byte
	if schema.IsRouter && len(schema.RouteNames) > 0 {
		routeSchema := RouteResultSchema(schema.RouteNames)
		var err error
		schemaJSON, err = json.Marshal(routeSchema)
		if err != nil {
			return "", nil, fmt.Errorf("marshal route schema: %w", err)
		}
	} else if schema.IsGate && len(schema.Dimensions) > 0 {
		gateSchema := GateResultSchema(schema.Dimensions)
		var err error
		schemaJSON, err = json.Marshal(gateSchema)
		if err != nil {
			return "", nil, fmt.Errorf("marshal gate schema: %w", err)
		}
	}

	if len(schemaJSON) > 0 {
		switch cfg.SchemaMode {
		case "inline":
			args = append(args, cfg.SchemaFlag, string(schemaJSON))
		case "file":
			tmpFile, err := os.CreateTemp("", "belayer-schema-*.json")
			if err != nil {
				return "", nil, fmt.Errorf("create schema temp file: %w", err)
			}
			if _, err := tmpFile.Write(schemaJSON); err != nil {
				tmpFile.Close()
				os.Remove(tmpFile.Name())
				return "", nil, fmt.Errorf("write schema temp file: %w", err)
			}
			tmpFile.Close()
			args = append(args, cfg.SchemaFlag, tmpFile.Name())
			cleanup = func() { os.Remove(tmpFile.Name()) }
		}
	}

	// Add working directory for codex.
	if vendorName == "codex" && workDir != "" {
		args = append(args, "-C", workDir)
	}

	// Build the shell command. The prompt is the final argument.
	parts := []string{cfg.Command}
	parts = append(parts, args...)
	parts = append(parts, shellQuote(prompt))

	return strings.Join(parts, " "), cleanup, nil
}

// shellQuote wraps a string in single quotes, escaping any internal single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// WriteGateResult parses JSON output from an agent and writes it as gate-result.json
// in the standard belayer output directory.
func WriteGateResult(outputDir string, agentOutput []byte, gateName string, attempt int) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Parse the agent's structured output.
	var parsed struct {
		Dimensions map[string]struct {
			Score     float64  `json:"score"`
			Issues   []string `json:"issues"`
			Rationale string  `json:"rationale"`
		} `json:"dimensions"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(agentOutput, &parsed); err != nil {
		return fmt.Errorf("parse agent gate output: %w", err)
	}

	// Build belayer gate result format.
	result := map[string]any{
		"gate":    gateName,
		"attempt": attempt,
		"dimensions": map[string]any{},
		"summary": parsed.Summary,
	}
	dims := result["dimensions"].(map[string]any)
	for name, d := range parsed.Dimensions {
		dims[name] = map[string]any{
			"score":     d.Score,
			"issues":    d.Issues,
			"rationale": d.Rationale,
		}
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gate result: %w", err)
	}

	// Write gate-result.json.
	resultPath := filepath.Join(outputDir, "gate-result.json")
	if err := os.WriteFile(resultPath, data, 0o644); err != nil {
		return fmt.Errorf("write gate result: %w", err)
	}

	// Write rationale.md from dimension rationales.
	var rationale strings.Builder
	rationale.WriteString("# Review Rationale\n\n")
	for name, d := range parsed.Dimensions {
		fmt.Fprintf(&rationale, "## %s: %.0f/10\n\n%s\n\n", name, d.Score, d.Rationale)
		if len(d.Issues) > 0 {
			rationale.WriteString("Issues:\n")
			for _, issue := range d.Issues {
				fmt.Fprintf(&rationale, "- %s\n", issue)
			}
			rationale.WriteString("\n")
		}
	}
	fmt.Fprintf(&rationale, "## Summary\n\n%s\n", parsed.Summary)

	rationalePath := filepath.Join(outputDir, "rationale.md")
	if err := os.WriteFile(rationalePath, []byte(rationale.String()), 0o644); err != nil {
		return fmt.Errorf("write rationale: %w", err)
	}

	return nil
}

// ExtractStreamResult parses Claude's stream-json output format and extracts
// the final result payload. Claude's --output-format stream-json emits
// newline-delimited JSON events; the result is the event with "type": "result".
func ExtractStreamResult(data []byte) ([]byte, error) {
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type   string          `json:"type"`
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(line, &event) == nil && event.Type == "result" && len(event.Output) > 0 {
			return event.Output, nil
		}
	}
	return nil, fmt.Errorf("no result event found in stream-json output (%d lines scanned)", len(lines))
}
