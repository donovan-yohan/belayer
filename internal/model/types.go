package model

// NodeOutcome is the result of a pipeline node execution.
type NodeOutcome string

const (
	OutcomePass  NodeOutcome = "PASS"
	OutcomeRetry NodeOutcome = "RETRY"
	OutcomeFail  NodeOutcome = "FAIL"
)

func (o NodeOutcome) IsValid() bool {
	switch o {
	case OutcomePass, OutcomeRetry, OutcomeFail:
		return true
	}
	return false
}

// CompletionResult is written by `belayer node-complete` and read by the activity.
type CompletionResult struct {
	Outcome    NodeOutcome `json:"outcome"`
	OutputPath string      `json:"output_path,omitempty"`
	TargetNode string      `json:"target_node,omitempty"`
	Feedback   string      `json:"feedback,omitempty"`
	Attempt    int         `json:"attempt"`
	CommitSHA  string      `json:"commit_sha,omitempty"`
	BaseRef    string      `json:"base_ref,omitempty"`
}

// ClimbStatus tracks the state of a pipeline run.
type ClimbStatus string

const (
	ClimbActive    ClimbStatus = "active"
	ClimbCompleted ClimbStatus = "completed"
	ClimbFailed    ClimbStatus = "failed"
)

func (s ClimbStatus) IsValid() bool {
	switch s {
	case ClimbActive, ClimbCompleted, ClimbFailed:
		return true
	}
	return false
}

// ClimbInput is the input to the Climb workflow.
type ClimbInput struct {
	Description      string              `json:"description"`
	DesignFile       string              `json:"design_file,omitempty"`
	PipelineFile     string              `json:"pipeline_file,omitempty"`
	PipelineYAML     []byte              `json:"pipeline_yaml,omitempty"`
	SubpipelineYAMLs map[string][]byte   `json:"subpipeline_yamls,omitempty"`
	FromNode         string              `json:"from_node,omitempty"`
	InputPath        string              `json:"input_path,omitempty"`
	WorkDir          string              `json:"work_dir"`
	Branch           string              `json:"branch"`
	Repos            []string            `json:"repos,omitempty"`
	BaseRef          string              `json:"base_ref,omitempty"`
}

// ClimbOutput is the output of a completed Climb workflow.
type ClimbOutput struct {
	Status      ClimbStatus       `json:"status"`
	NodeOutputs map[string]string `json:"node_outputs"`
	Message     string            `json:"message,omitempty"`
	Branch      string            `json:"branch"`
}
