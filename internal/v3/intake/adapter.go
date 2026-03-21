package intake

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// IntakeAdapter polls an external source and produces SubmitSpecs.
type IntakeAdapter interface {
	Poll(ctx context.Context) ([]SubmitSpec, error)
}

// NewAdapter creates an IntakeAdapter from pipeline intake config.
func NewAdapter(cfg pipeline.IntakeConfig, pipelineName string) (IntakeAdapter, error) {
	switch cfg.Type {
	case "jira":
		return NewJiraAdapter(cfg, pipelineName)
	default:
		return nil, fmt.Errorf("unsupported intake type: %q", cfg.Type)
	}
}
