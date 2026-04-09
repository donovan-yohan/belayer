package vendor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Claude token pricing (approximate, claude-sonnet-4).
const (
	claudeInputCostPerMTok  = 3.0  // $3 per million input tokens
	claudeOutputCostPerMTok = 15.0 // $15 per million output tokens
)

// ClaudeAdapter implements Adapter for the Claude Code CLI.
type ClaudeAdapter struct{}

// Name returns "claude".
func (a ClaudeAdapter) Name() string { return "claude" }

// LaunchCmd returns the shell command to start Claude Code in stream-json mode.
// If systemPrompt is non-empty, it is passed via the -p flag.
func (a ClaudeAdapter) LaunchCmd(workDir string, systemPrompt string) string {
	base := "claude --dangerously-skip-permissions --output-format stream-json"
	if systemPrompt != "" {
		return fmt.Sprintf("%s -p %q", base, systemPrompt)
	}
	return base
}

// claudeAssistantMessage represents the stream-json assistant turn.
type claudeAssistantMessage struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		Usage struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			CacheReadTokens  int `json:"cache_read_tokens"`
			CacheWriteTokens int `json:"cache_write_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeResultMessage represents the stream-json result turn.
type claudeResultMessage struct {
	Type    string  `json:"type"`
	Result  string  `json:"result"`
	CostUSD float64 `json:"cost_usd"`
	Usage   struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		CacheReadTokens  int `json:"cache_read_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"usage"`
}

// ParseOutput parses a single stream-json line from Claude Code.
// Unknown types are returned as raw events. Only malformed JSON returns an error.
func (a ClaudeAdapter) ParseOutput(line string) (OutputEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return OutputEvent{Type: "text", Raw: line}, nil
	}

	// Detect JSON type field without full unmarshal first.
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return OutputEvent{}, fmt.Errorf("vendor/claude: parse output: %w", err)
	}

	switch probe.Type {
	case "assistant":
		var msg claudeAssistantMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return OutputEvent{}, fmt.Errorf("vendor/claude: parse assistant message: %w", err)
		}
		// Collect text content from the message content array.
		var sb strings.Builder
		for _, block := range msg.Message.Content {
			if block.Type == "text" {
				sb.WriteString(block.Text)
			}
		}
		usage := msg.Message.Usage
		costUSD := (float64(usage.InputTokens) * claudeInputCostPerMTok / 1_000_000) +
			(float64(usage.OutputTokens) * claudeOutputCostPerMTok / 1_000_000)
		return OutputEvent{
			Type:    "text",
			Content: sb.String(),
			TokenUsage: &TokenUsage{
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
				CostUSD:          costUSD,
			},
			Raw: line,
		}, nil

	case "result":
		var msg claudeResultMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return OutputEvent{}, fmt.Errorf("vendor/claude: parse result message: %w", err)
		}
		usage := msg.Usage
		costUSD := msg.CostUSD
		if costUSD == 0 {
			costUSD = (float64(usage.InputTokens)*claudeInputCostPerMTok +
				float64(usage.OutputTokens)*claudeOutputCostPerMTok) / 1_000_000
		}
		return OutputEvent{
			Type:    "token_usage",
			Content: msg.Result,
			TokenUsage: &TokenUsage{
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
				CostUSD:          costUSD,
			},
			Raw: line,
		}, nil

	default:
		return OutputEvent{Type: "text", Raw: line}, nil
	}
}

// CompileRestartPrompt formats context into Claude's expected continuation format.
func (a ClaudeAdapter) CompileRestartPrompt(context string) string {
	return fmt.Sprintf("Continue from where you left off. Previous context:\n\n%s", context)
}
