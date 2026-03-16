package model

import "time"

// ProblemStatus represents the lifecycle state of a problem.
type ProblemStatus string

const (
	ProblemStatusPending        ProblemStatus = "pending"
	ProblemStatusRunning        ProblemStatus = "running"
	ProblemStatusReviewing      ProblemStatus = "reviewing"
	ProblemStatusComplete       ProblemStatus = "complete"
	ProblemStatusStuck          ProblemStatus = "stuck"
	ProblemStatusImported       ProblemStatus = "imported"
	ProblemStatusEnriching      ProblemStatus = "enriching"
	ProblemStatusPRCreating     ProblemStatus = "pr_creating"
	ProblemStatusPRMonitoring   ProblemStatus = "pr_monitoring"
	ProblemStatusCIFixing       ProblemStatus = "ci_fixing"
	ProblemStatusReviewReacting ProblemStatus = "review_reacting"
	ProblemStatusMerged         ProblemStatus = "merged"
	ProblemStatusClosed         ProblemStatus = "closed"
	ProblemStatusSpotting       ProblemStatus = "spotting"
	ProblemStatusReflecting     ProblemStatus = "reflecting"
	ProblemStatusNeedsHuman     ProblemStatus = "needs_human"
)

// ClimbStatus represents the lifecycle state of a climb.
type ClimbStatus string

const (
	ClimbStatusPending  ClimbStatus = "pending"
	ClimbStatusRunning  ClimbStatus = "running"
	ClimbStatusSpotting ClimbStatus = "spotting"
	ClimbStatusComplete ClimbStatus = "complete"
	ClimbStatusFailed   ClimbStatus = "failed"
)

// EventType categorizes audit events.
type EventType string

const (
	EventProblemCreated           EventType = "problem_created"
	EventClimbStarted             EventType = "climb_started"
	EventClimbCompleted           EventType = "climb_completed"
	EventClimbFailed              EventType = "climb_failed"
	EventAnchorSpawned            EventType = "anchor_spawned"
	EventAnchorVerdict            EventType = "anchor_verdict"
	EventSpotterSpawned           EventType = "spotter_spawned"
	EventSpotterVerdict           EventType = "spotter_verdict"
	EventPRCreated                EventType = "pr_created"
	EventIssueImported            EventType = "issue_imported"
	EventIssueConverted           EventType = "issue_converted"
	EventPRStacked                EventType = "pr_stacked"
	EventCIFailed                 EventType = "ci_failed"
	EventCIFixDispatched          EventType = "ci_fix_dispatched"
	EventCIFixSucceeded           EventType = "ci_fix_succeeded"
	EventCIFixExhausted           EventType = "ci_fix_exhausted"
	EventReviewCommentReceived    EventType = "review_comment_received"
	EventReviewCommentReplied     EventType = "review_comment_replied"
	EventChangesRequested         EventType = "changes_requested"
	EventReviewReactionDispatched EventType = "review_reaction_dispatched"
	EventPRApproved               EventType = "pr_approved"
	EventPRMerged                 EventType = "pr_merged"
	EventPRClosed                 EventType = "pr_closed"
	EventCorrectionClimbCreated   EventType = "correction_climb_created"
	EventSpotterCorrectionLoop    EventType = "spotter_correction_loop"
	EventReflectStarted           EventType = "reflect_started"
	EventReflectCompleted         EventType = "reflect_completed"
	EventLearningCaptured         EventType = "learning_captured"
	EventNeedsHuman               EventType = "needs_human"
)

// TopStatusReviewIncomplete is used in TOP.json to indicate a lead completed
// but the multi-persona review loop did not fully pass within the allowed cycles.
const TopStatusReviewIncomplete = "review_incomplete"

// Problem represents a work item submitted by the user.
type Problem struct {
	ID             string        `json:"id"`
	CragID         string        `json:"crag_id"`
	Spec           string        `json:"spec"`
	ClimbsJSON     string        `json:"climbs_json"`
	JiraRef        string        `json:"jira_ref"`
	TrackerIssueID string        `json:"tracker_issue_id"`
	Status         ProblemStatus `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// Climb represents a per-repo climb within a problem.
type Climb struct {
	ID          string      `json:"id"`
	ProblemID   string      `json:"problem_id"`
	RepoName    string      `json:"repo_name"`
	Description string      `json:"description"`
	DependsOn   []string    `json:"depends_on"`
	Status      ClimbStatus `json:"status"`
	Attempt     int         `json:"attempt"`
	CreatedAt   time.Time   `json:"created_at"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
}

// Event represents an audit trail entry.
type Event struct {
	ID        int64     `json:"id"`
	ProblemID string    `json:"problem_id"`
	ClimbID   string    `json:"climb_id"`
	Type      EventType `json:"type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// SpotterReview records a spotter review verdict.
type SpotterReview struct {
	ID        int64     `json:"id"`
	ProblemID string    `json:"problem_id"`
	Attempt   int       `json:"attempt"`
	Verdict   string    `json:"verdict"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

// ClimbsFile is the top-level structure of climbs.json.
type ClimbsFile struct {
	Repos map[string]RepoClimbs `json:"repos"`
}

// RepoClimbs contains the climbs for a single repository.
type RepoClimbs struct {
	Climbs []ClimbSpec `json:"climbs"`
}

// ClimbSpec defines a climb as specified in climbs.json.
type ClimbSpec struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"`
}

// LearningCategory classifies the type of a learning.
type LearningCategory string

const (
	LearningCategoryTestGap       LearningCategory = "test_gap"
	LearningCategorySpecAmbiguity LearningCategory = "spec_ambiguity"
	LearningCategoryInfraIssue    LearningCategory = "infra_issue"
	LearningCategoryReviewMiss    LearningCategory = "review_miss"
	LearningCategoryPattern       LearningCategory = "pattern"
)

// LearningSeverity represents the impact level of a learning.
type LearningSeverity string

const (
	LearningSeverityHigh   LearningSeverity = "high"
	LearningSeverityMedium LearningSeverity = "medium"
	LearningSeverityLow    LearningSeverity = "low"
)

// Learning records a captured insight from a review loop for a crag.
type Learning struct {
	ID             string
	CragID         string
	ProblemID      string
	Category       LearningCategory
	Description    string
	Recommendation string
	Severity       LearningSeverity
	Resolved       bool
	AccessCount    int
	CreatedAt      time.Time
}

// Environment records the provider command and env vars for a problem.
type Environment struct {
	ProblemID       string    `json:"problem_id"`
	ProviderCommand string    `json:"provider_command"`
	EnvName         string    `json:"env_name"`
	EnvJSON         string    `json:"env_json"`
	CreatedAt       time.Time `json:"created_at"`
}
