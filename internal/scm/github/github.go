package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// Provider implements scm.SCMProvider via the gh CLI.
type Provider struct{}

func New() *Provider {
	return &Provider{}
}

// ghPRView is the JSON shape returned by gh pr view.
type ghPRView struct {
	Number           int            `json:"number"`
	State            string         `json:"state"`
	URL              string         `json:"url"`
	Mergeable        string         `json:"mergeable"`
	StatusCheckRollup []ghCheck     `json:"statusCheckRollup"`
	Reviews          []ghReview     `json:"reviews"`
}

type ghCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type ghReview struct {
	Author ghAuthor `json:"author"`
	State  string   `json:"state"`
	Body   string   `json:"body"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// ghComment is the JSON shape for PR review comments from gh api.
type ghComment struct {
	ID   int64    `json:"id"`
	User ghAuthor `json:"user"`
	Body string   `json:"body"`
	Path string   `json:"path"`
	Line int      `json:"line"`
}

// ghIssueComment is used for issue-level comments.
type ghIssueComment struct {
	ID        int64    `json:"id"`
	User      ghAuthor `json:"user"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"created_at"`
}

func parseGHPRStatusJSON(data []byte) (*model.PRStatus, error) {
	var gh ghPRView
	if err := json.Unmarshal(data, &gh); err != nil {
		return nil, fmt.Errorf("parse gh pr status: %w", err)
	}

	checks := make([]model.Check, len(gh.StatusCheckRollup))
	for i, c := range gh.StatusCheckRollup {
		checks[i] = model.Check{
			Name:   c.Name,
			Status: c.Conclusion,
		}
	}

	reviews := make([]model.Review, len(gh.Reviews))
	for i, r := range gh.Reviews {
		reviews[i] = model.Review{
			Author: r.Author.Login,
			State:  r.State,
			Body:   r.Body,
		}
	}

	ciStatus := determineCIStatus(gh.StatusCheckRollup)

	return &model.PRStatus{
		Number:    gh.Number,
		State:     strings.ToLower(gh.State),
		CIStatus:  ciStatus,
		CIDetails: checks,
		Reviews:   reviews,
		Mergeable: strings.ToUpper(gh.Mergeable) == "MERGEABLE",
		URL:       gh.URL,
	}, nil
}

func determineCIStatus(checks []ghCheck) string {
	if len(checks) == 0 {
		return "pending"
	}
	allPassed := true
	for _, c := range checks {
		conclusion := strings.ToUpper(c.Conclusion)
		if conclusion == "FAILURE" || conclusion == "FAILED" {
			return "failing"
		}
		status := strings.ToUpper(c.Status)
		if status != "COMPLETED" || (conclusion != "SUCCESS" && conclusion != "NEUTRAL" && conclusion != "SKIPPED") {
			allPassed = false
		}
	}
	if allPassed {
		return "passing"
	}
	return "pending"
}

type ghActivityComments struct {
	Comments []ghIssueComment `json:"comments"`
}

type ghActivityReviews struct {
	Reviews []ghReview `json:"reviews"`
}

func parseGHPRActivityJSON(commentsData, reviewsData []byte, since time.Time) (*model.PRActivity, error) {
	var comments []ghIssueComment
	if err := json.Unmarshal(commentsData, &comments); err != nil {
		return nil, fmt.Errorf("parse gh pr comments: %w", err)
	}

	var ghReviews []ghReview
	if err := json.Unmarshal(reviewsData, &ghReviews); err != nil {
		return nil, fmt.Errorf("parse gh pr reviews: %w", err)
	}

	var filteredComments []model.ReviewComment
	for _, c := range comments {
		t, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil || !t.After(since) {
			continue
		}
		filteredComments = append(filteredComments, model.ReviewComment{
			ID:     c.ID,
			Author: c.User.Login,
			Body:   c.Body,
		})
	}

	reviews := make([]model.Review, len(ghReviews))
	for i, r := range ghReviews {
		reviews[i] = model.Review{
			Author: r.Author.Login,
			State:  r.State,
			Body:   r.Body,
		}
	}

	return &model.PRActivity{
		Comments: filteredComments,
		Reviews:  reviews,
	}, nil
}

func runInDir(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.Output()
}

func (p *Provider) CreatePR(ctx context.Context, repoDir string, opts model.PROptions) (*model.PRStatus, error) {
	args := []string{"pr", "create", "--title", opts.Title, "--body", opts.Body}
	if opts.BaseBranch != "" {
		args = append(args, "--base", opts.BaseBranch)
	}
	if opts.Draft {
		args = append(args, "--draft")
	}

	out, err := runInDir(ctx, repoDir, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w", err)
	}

	// Output is the PR URL; extract the PR number from it.
	url := strings.TrimSpace(string(out))
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("unexpected gh pr create output: %q", url)
	}
	numStr := parts[len(parts)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse PR number from URL %q: %w", url, err)
	}

	return p.GetPRStatus(ctx, repoDir, num)
}

func (p *Provider) CreateStackedPRs(_ context.Context, _ string, _ []model.PRSplit) ([]*model.PRStatus, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func (p *Provider) GetPRStatus(ctx context.Context, repoDir string, prNumber int) (*model.PRStatus, error) {
	out, err := runInDir(ctx, repoDir, "gh", "pr", "view", strconv.Itoa(prNumber),
		"--json", "number,state,url,mergeable,statusCheckRollup,reviews",
	)
	if err != nil {
		return nil, fmt.Errorf("gh pr view %d: %w", prNumber, err)
	}
	return parseGHPRStatusJSON(out)
}

func (p *Provider) GetNewActivity(ctx context.Context, repoDir string, prNumber int, since time.Time) (*model.PRActivity, error) {
	num := strconv.Itoa(prNumber)

	// Fetch issue comments (general PR comments).
	commentsOut, err := runInDir(ctx, repoDir, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/issues/%s/comments", num),
		"--paginate",
	)
	if err != nil {
		return nil, fmt.Errorf("gh api pr comments %d: %w", prNumber, err)
	}

	// Fetch reviews.
	reviewsOut, err := runInDir(ctx, repoDir, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/reviews", num),
	)
	if err != nil {
		return nil, fmt.Errorf("gh api pr reviews %d: %w", prNumber, err)
	}

	return parseGHPRActivityJSON(commentsOut, reviewsOut, since)
}

func (p *Provider) ReplyToComment(ctx context.Context, repoDir string, prNumber int, commentID int64, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshal reply body: %w", err)
	}

	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/issues/comments/%d/replies", commentID),
		"--method", "POST",
		"--input", "-",
	)
	cmd.Dir = repoDir
	cmd.Stdin = bytes.NewReader(payload)

	if out, err := cmd.Output(); err != nil {
		return fmt.Errorf("gh api reply to comment %d: %w (output: %s)", commentID, err, string(out))
	}
	return nil
}

func (p *Provider) Merge(ctx context.Context, repoDir string, prNumber int) error {
	_, err := runInDir(ctx, repoDir, "gh", "pr", "merge", strconv.Itoa(prNumber), "--merge")
	if err != nil {
		return fmt.Errorf("gh pr merge %d: %w", prNumber, err)
	}
	return nil
}
