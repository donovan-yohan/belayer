package tracker

import (
	"context"

	"github.com/donovan-yohan/belayer/internal/model"
)

type Tracker interface {
	ListIssues(ctx context.Context, filter model.IssueFilter) ([]model.Issue, error)
	GetIssue(ctx context.Context, id string) (*model.Issue, error)
}
