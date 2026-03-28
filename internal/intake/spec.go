package intake

import "fmt"

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

// Validate checks that required fields are present.
func (s *SubmitSpec) Validate() error {
	if s.Spec == "" {
		return fmt.Errorf("spec is required")
	}
	if s.Source == "" {
		return fmt.Errorf("source is required")
	}
	return nil
}
