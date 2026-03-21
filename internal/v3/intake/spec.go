package intake

// SubmitSpec is the universal handoff format between intake adapters and the pipeline.
// Every intake (interactive, jira, linear, etc.) produces this struct.
type SubmitSpec struct {
	Spec         string            `json:"spec"`
	Repos        []string          `json:"repos,omitempty"`
	Source       string            `json:"source"`
	ExternalID   string            `json:"external_id"`
	PipelineName string            `json:"pipeline_name"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
