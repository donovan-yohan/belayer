package jira

import (
	"context"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/model"
)

type Tracker struct {
	BaseURL string
	Project string
	Token   string
}

func New(baseURL, project, token string) *Tracker {
	return &Tracker{BaseURL: baseURL, Project: project, Token: token}
}

func (t *Tracker) ListIssues(_ context.Context, _ model.IssueFilter) ([]model.Issue, error) {
	return nil, fmt.Errorf("jira tracker not yet implemented")
}

func (t *Tracker) GetIssue(_ context.Context, _ string) (*model.Issue, error) {
	return nil, fmt.Errorf("jira tracker not yet implemented")
}
