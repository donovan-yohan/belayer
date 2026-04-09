package vendor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Codex token pricing (approximate).
const (
	codexInputCostPerMTok  = 2.0 // $2 per million input tokens
	codexOutputCostPerMTok = 8.0 // $8 per million output tokens
)

// CodexAdapter implements Adapter for the Codex CLI.
type CodexAdapter struct{}

// Name returns "codex".
func (a CodexAdapter) Name() string { return "codex" }

// LaunchCmd returns the shell command to start Codex in full-auto mode.
// If systemPrompt is non-empty, it is passed as the -q flag argument.
func (a CodexAdapter) LaunchCmd(workDir string, systemPrompt string) string {
	base := "codex --approval-mode full-auto"
	if systemPrompt != "" {
		return fmt.Sprintf("%s -q %q", base, systemPrompt)
	}
	return base
}

// codexMessageLine represents a message output line from the Codex CLI.
type codexMessageLine struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// codexCompletedLine represents the final completion line from the Codex CLI.
type codexCompletedLine struct {
	Type  string `json:"type"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ParseOutput parses a single JSON line from the Codex CLI output.
// Unknown types are returned as raw events. Only malformed JSON returns an error.
func (a CodexAdapter) ParseOutput(line string) (OutputEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return OutputEvent{Type: "text", Raw: line}, nil
	}

	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return OutputEvent{}, fmt.Errorf("vendor/codex: parse output: %w", err)
	}

	switch probe.Type {
	case "message":
		var msg codexMessageLine
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return OutputEvent{}, fmt.Errorf("vendor/codex: parse message line: %w", err)
		}
		return OutputEvent{
			Type:    "text",
			Content: msg.Content,
			Raw:     line,
		}, nil

	case "completed":
		var msg codexCompletedLine
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return OutputEvent{}, fmt.Errorf("vendor/codex: parse completed line: %w", err)
		}
		costUSD := (float64(msg.Usage.InputTokens)*codexInputCostPerMTok +
			float64(msg.Usage.OutputTokens)*codexOutputCostPerMTok) / 1_000_000
		return OutputEvent{
			Type: "token_usage",
			TokenUsage: &TokenUsage{
				InputTokens:  msg.Usage.InputTokens,
				OutputTokens: msg.Usage.OutputTokens,
				CostUSD:      costUSD,
			},
			Raw: line,
		}, nil

	default:
		return OutputEvent{Type: "text", Raw: line}, nil
	}
}

// CompileRestartPrompt formats context for Codex continuation.
func (a CodexAdapter) CompileRestartPrompt(context string) string {
	return fmt.Sprintf("Continue from where you left off. Previous context:\n\n%s", context)
}
