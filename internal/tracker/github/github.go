package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/donovan-yohan/belayer/internal/model"
)

type Tracker struct {
	Repo string // optional: "owner/repo", empty uses current repo
}

func New(repo string) *Tracker {
	return &Tracker{Repo: repo}
}

// ghIssue is the JSON shape returned by gh issue view/list.
type ghIssue struct {
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	Labels    []ghLabel   `json:"labels"`
	Assignees []ghUser    `json:"assignees"`
	Comments  []ghComment `json:"comments"`
	URL       string      `json:"url"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghComment struct {
	Author ghUser `json:"author"`
	Body   string `json:"body"`
	// gh returns createdAt as a string
	CreatedAt string `json:"createdAt"`
}

func parseGHIssueJSON(data []byte) (*model.Issue, error) {
	var gh ghIssue
	if err := json.Unmarshal(data, &gh); err != nil {
		return nil, fmt.Errorf("parse gh issue: %w", err)
	}
	return convertGHIssue(gh), nil
}

func parseGHIssueListJSON(data []byte) ([]model.Issue, error) {
	var ghs []ghIssue
	if err := json.Unmarshal(data, &ghs); err != nil {
		return nil, fmt.Errorf("parse gh issue list: %w", err)
	}
	issues := make([]model.Issue, len(ghs))
	for i, gh := range ghs {
		issues[i] = *convertGHIssue(gh)
	}
	return issues, nil
}

func convertGHIssue(gh ghIssue) *model.Issue {
	labels := make([]string, len(gh.Labels))
	for i, l := range gh.Labels {
		labels[i] = l.Name
	}

	comments := make([]model.Comment, len(gh.Comments))
	for i, c := range gh.Comments {
		comments[i] = model.Comment{
			Author: c.Author.Login,
			Body:   c.Body,
			Date:   c.CreatedAt,
		}
	}

	assignee := ""
	if len(gh.Assignees) > 0 {
		assignee = gh.Assignees[0].Login
	}

	return &model.Issue{
		ID:       fmt.Sprintf("#%d", gh.Number),
		Title:    gh.Title,
		Body:     gh.Body,
		Labels:   labels,
		Comments: comments,
		Assignee: assignee,
		URL:      gh.URL,
	}
}

func (t *Tracker) ghArgs(sub ...string) []string {
	args := sub
	if t.Repo != "" {
		args = append(args, "-R", t.Repo)
	}
	return args
}

func (t *Tracker) ListIssues(ctx context.Context, filter model.IssueFilter) ([]model.Issue, error) {
	args := t.ghArgs("issue", "list",
		"--json", "number,title,body,labels,assignees,comments,url",
		"--limit", "100",
	)
	for _, label := range filter.Labels {
		args = append(args, "--label", label)
	}
	if filter.Assignee != "" {
		args = append(args, "--assignee", filter.Assignee)
	}

	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	return parseGHIssueListJSON(out)
}

func (t *Tracker) GetIssue(ctx context.Context, id string) (*model.Issue, error) {
	num := strings.TrimPrefix(id, "#")
	args := t.ghArgs("issue", "view", num,
		"--json", "number,title,body,labels,assignees,comments,url",
	)

	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gh issue view %s: %w", id, err)
	}
	return parseGHIssueJSON(out)
}
