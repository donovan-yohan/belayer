package model

import "time"

// PRState represents the state of a pull request.
type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

// CIStatus represents the CI check rollup status.
type CIStatus string

const (
	CIStatusPassing CIStatus = "passing"
	CIStatusFailing CIStatus = "failing"
	CIStatusPending CIStatus = "pending"
	CIStatusUnknown CIStatus = "unknown"
)

// ReviewState represents the state of a code review.
type ReviewState string

const (
	ReviewStateApproved         ReviewState = "approved"
	ReviewStateChangesRequested ReviewState = "changes_requested"
	ReviewStateCommented        ReviewState = "commented"
	ReviewStateDismissed        ReviewState = "dismissed"
)

type PROptions struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`
	Draft      bool   `json:"draft"`
}

type PRSplit struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Commits       []string `json:"commits"`
	StackPosition int      `json:"stack_position"`
}

type PRStatus struct {
	Number    int        `json:"number"`
	State     PRState    `json:"state"`
	CIStatus  CIStatus   `json:"ci_status"`
	CIDetails []Check    `json:"ci_details"`
	Reviews   []Review   `json:"reviews"`
	Mergeable bool       `json:"mergeable"`
	URL       string     `json:"url"`
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Review struct {
	Author string      `json:"author"`
	State  ReviewState `json:"state"`
	Body   string      `json:"body"`
}

type PRActivity struct {
	Comments      []ReviewComment `json:"comments"`
	Reviews       []Review        `json:"reviews"`
	CITransitions []CITransition  `json:"ci_transitions"`
}

type ReviewComment struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

type CITransition struct {
	CheckName string    `json:"check_name"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Time      time.Time `json:"time"`
}
