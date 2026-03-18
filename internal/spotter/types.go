package spotter

// SpotJSON is the structured output from a spotter validation run.
type SpotJSON struct {
	Pass             bool             `json:"pass"`
	ProjectType      string           `json:"project_type"`
	Issues           []Issue          `json:"issues"`
	Screenshots      []string         `json:"screenshots,omitempty"`
	SpecCompliance   *SpecCompliance  `json:"spec_compliance,omitempty"`
	TestContract     *TestContractResult `json:"test_contract,omitempty"`
	Runtime          *RuntimeResult   `json:"runtime,omitempty"`
	CorrectionClimbs []CorrectionClimb `json:"correction_climbs,omitempty"`
}

// Issue describes a single validation failure.
type Issue struct {
	Check       string `json:"check"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "error" | "warning"
}

// SpecCompliance describes how well the implementation satisfies the spec requirements.
type SpecCompliance struct {
	Satisfied    []string `json:"satisfied"`
	Unsatisfied  []string `json:"unsatisfied"`
	Unverifiable []string `json:"unverifiable"`
}

// TestContractResult describes the results of test contract validation.
type TestContractResult struct {
	Satisfied   int      `json:"satisfied"`
	Unsatisfied int      `json:"unsatisfied"`
	Details     []string `json:"details"`
}

// RuntimeResult describes the runtime health of the implementation.
type RuntimeResult struct {
	Build     string `json:"build"`
	Tests     string `json:"tests"`
	DevServer string `json:"dev_server"`
}

// CorrectionClimb describes a follow-up climb needed to address issues.
type CorrectionClimb struct {
	Description     string   `json:"description"`
	IssuesAddressed []string `json:"issues_addressed"`
	Context         string   `json:"context"`
}
