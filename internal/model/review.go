package model

import "time"

type PullRequest struct {
	ID            int64      `json:"id"`
	ProblemID     string     `json:"problem_id"`
	RepoName      string     `json:"repo_name"`
	PRNumber      int        `json:"pr_number"`
	URL           string     `json:"url"`
	StackPosition int        `json:"stack_position"`
	StackSize     int        `json:"stack_size"`
	CIStatus      string     `json:"ci_status"`
	CIFixCount    int        `json:"ci_fix_count"`
	ReviewStatus  string     `json:"review_status"`
	State         string     `json:"state"`
	LastPolledAt  *time.Time `json:"last_polled_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type PRReaction struct {
	ID             int64     `json:"id"`
	PRID           int64     `json:"pr_id"`
	TriggerType    string    `json:"trigger_type"`
	TriggerPayload string    `json:"trigger_payload"`
	ActionTaken    string    `json:"action_taken"`
	LeadID         string    `json:"lead_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type TrackerIssue struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	CommentsJSON string    `json:"comments_json"`
	LabelsJSON   string    `json:"labels_json"`
	Priority     string    `json:"priority"`
	Assignee     string    `json:"assignee"`
	URL          string    `json:"url"`
	RawJSON      string    `json:"raw_json"`
	ProblemID    string    `json:"problem_id"`
	SyncedAt     time.Time `json:"synced_at"`
}
