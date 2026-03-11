package scm

import (
	"context"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

type SCMProvider interface {
	CreatePR(ctx context.Context, repoDir string, opts model.PROptions) (*model.PRStatus, error)
	CreateStackedPRs(ctx context.Context, repoDir string, splits []model.PRSplit) ([]*model.PRStatus, error)
	GetPRStatus(ctx context.Context, repoDir string, prNumber int) (*model.PRStatus, error)
	GetNewActivity(ctx context.Context, repoDir string, prNumber int, since time.Time) (*model.PRActivity, error)
	ReplyToComment(ctx context.Context, repoDir string, prNumber int, commentID int64, body string) error
	Merge(ctx context.Context, repoDir string, prNumber int) error
}
