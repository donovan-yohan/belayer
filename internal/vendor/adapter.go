package vendor

// TokenUsage holds token consumption and cost data from a vendor agent run.
type TokenUsage struct {
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// AdapterStatus represents the current state of a vendor agent.
type AdapterStatus string

const (
	StatusIdle    AdapterStatus = "idle"
	StatusRunning AdapterStatus = "running"
	StatusDone    AdapterStatus = "done"
	StatusError   AdapterStatus = "error"
)

// Adapter abstracts over different AI coding agent vendors.
type Adapter interface {
	// Name returns the vendor name (e.g., "claude", "codex", "generic").
	Name() string
	// LaunchCmd returns the shell command to start this vendor's agent.
	LaunchCmd(workDir string, systemPrompt string) string
	// ParseOutput extracts structured data from a line of agent output.
	ParseOutput(line string) (OutputEvent, error)
	// CompileRestartPrompt formats a restart prompt appropriate for this vendor.
	CompileRestartPrompt(context string) string
}

// OutputEvent is a structured event extracted from a single line of agent output.
type OutputEvent struct {
	Type       string      `json:"type"` // "text", "tool_use", "token_usage", "error"
	Content    string      `json:"content,omitempty"`
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
	Raw        string      `json:"raw"`
}
