package model

import "time"

// ProblemStatus represents the lifecycle state of a problem.
type ProblemStatus string

const (
	ProblemStatusPending   ProblemStatus = "pending"
	ProblemStatusRunning   ProblemStatus = "running"
	ProblemStatusReviewing ProblemStatus = "reviewing"
	ProblemStatusComplete  ProblemStatus = "complete"
	ProblemStatusStuck     ProblemStatus = "stuck"
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
	EventProblemCreated  EventType = "task_created"
	EventClimbStarted   EventType = "goal_started"
	EventClimbCompleted EventType = "goal_completed"
	EventClimbFailed    EventType = "goal_failed"
	EventAnchorSpawned  EventType = "anchor_spawned"
	EventAnchorVerdict  EventType = "anchor_verdict"
	EventSpotterSpawned EventType = "spotter_spawned"
	EventSpotterVerdict EventType = "spotter_verdict"
	EventPRCreated      EventType = "pr_created"
)

// Problem represents a work item submitted by the user.
type Problem struct {
	ID         string        `json:"id"`
	InstanceID string        `json:"instance_id"`
	Spec       string        `json:"spec"`
	ClimbsJSON string        `json:"climbs_json"`
	JiraRef    string        `json:"jira_ref"`
	Status     ProblemStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
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
