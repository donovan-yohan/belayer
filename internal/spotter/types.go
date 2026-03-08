package spotter

// SpotJSON is the structured output from a spotter validation run.
type SpotJSON struct {
	Pass        bool     `json:"pass"`
	ProjectType string   `json:"project_type"`
	Issues      []Issue  `json:"issues"`
	Screenshots []string `json:"screenshots,omitempty"`
}

// Issue describes a single validation failure.
type Issue struct {
	Check       string `json:"check"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "error" | "warning"
}
