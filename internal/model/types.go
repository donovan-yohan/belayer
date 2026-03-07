package model

import "time"

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusReviewing TaskStatus = "reviewing"
	TaskStatusComplete  TaskStatus = "complete"
	TaskStatusStuck     TaskStatus = "stuck"
)

// GoalStatus represents the lifecycle state of a goal.
type GoalStatus string

const (
	GoalStatusPending  GoalStatus = "pending"
	GoalStatusRunning  GoalStatus = "running"
	GoalStatusComplete GoalStatus = "complete"
	GoalStatusFailed   GoalStatus = "failed"
)

// EventType categorizes audit events.
type EventType string

const (
	EventTaskCreated    EventType = "task_created"
	EventGoalStarted   EventType = "goal_started"
	EventGoalCompleted EventType = "goal_completed"
	EventGoalFailed    EventType = "goal_failed"
	EventSpotterSpawned EventType = "spotter_spawned"
	EventReviewVerdict  EventType = "review_verdict"
	EventPRCreated     EventType = "pr_created"
)

// Task represents a work item submitted by the user.
type Task struct {
	ID         string     `json:"id"`
	InstanceID string     `json:"instance_id"`
	Spec       string     `json:"spec"`
	GoalsJSON  string     `json:"goals_json"`
	JiraRef    string     `json:"jira_ref"`
	Status     TaskStatus `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Goal represents a per-repo goal within a task.
type Goal struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	RepoName    string     `json:"repo_name"`
	Description string     `json:"description"`
	DependsOn   []string   `json:"depends_on"`
	Status      GoalStatus `json:"status"`
	Attempt     int        `json:"attempt"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Event represents an audit trail entry.
type Event struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	GoalID    string    `json:"goal_id"`
	Type      EventType `json:"type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// SpotterReview records a spotter review verdict.
type SpotterReview struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Attempt   int       `json:"attempt"`
	Verdict   string    `json:"verdict"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

// GoalsFile is the top-level structure of goals.json.
type GoalsFile struct {
	Repos map[string]RepoGoals `json:"repos"`
}

// RepoGoals contains the goals for a single repository.
type RepoGoals struct {
	Goals []GoalSpec `json:"goals"`
}

// GoalSpec defines a goal as specified in goals.json.
type GoalSpec struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"`
}
