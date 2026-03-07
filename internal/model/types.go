package model

import "time"

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusPending      TaskStatus = "pending"
	TaskStatusDecomposing  TaskStatus = "decomposing"
	TaskStatusRunning      TaskStatus = "running"
	TaskStatusAligning     TaskStatus = "aligning"
	TaskStatusComplete     TaskStatus = "complete"
	TaskStatusFailed       TaskStatus = "failed"
)

// LeadStatus represents the lifecycle state of a lead execution loop.
type LeadStatus string

const (
	LeadStatusPending  LeadStatus = "pending"
	LeadStatusRunning  LeadStatus = "running"
	LeadStatusStuck    LeadStatus = "stuck"
	LeadStatusComplete LeadStatus = "complete"
	LeadStatusFailed   LeadStatus = "failed"
)

// EventType categorizes audit events.
type EventType string

const (
	EventTaskCreated       EventType = "task_created"
	EventTaskDecomposed    EventType = "task_decomposed"
	EventLeadStarted      EventType = "lead_started"
	EventLeadProgress     EventType = "lead_progress"
	EventLeadStuck        EventType = "lead_stuck"
	EventLeadComplete     EventType = "lead_complete"
	EventLeadFailed       EventType = "lead_failed"
	EventAlignmentStarted EventType = "alignment_started"
	EventAlignmentPassed  EventType = "alignment_passed"
	EventAlignmentFailed  EventType = "alignment_failed"
	EventPRsCreated       EventType = "prs_created"
)

// AgenticNodeType identifies which ephemeral Claude session produced a decision.
type AgenticNodeType string

const (
	AgenticSufficiency   AgenticNodeType = "sufficiency"
	AgenticDecomposition AgenticNodeType = "decomposition"
	AgenticAlignment     AgenticNodeType = "alignment"
	AgenticStuckAnalysis AgenticNodeType = "stuck_analysis"
)

// Instance represents a long-lived workspace containing repos and tasks.
type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Task represents a work item submitted by the user.
type Task struct {
	ID                 string     `json:"id"`
	InstanceID         string     `json:"instance_id"`
	Description        string     `json:"description"`
	Source             string     `json:"source"` // "text" or "jira"
	SourceRef          string     `json:"source_ref"`
	Status             TaskStatus `json:"status"`
	SufficiencyChecked bool       `json:"sufficiency_checked"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// TaskRepo represents a per-repo decomposition of a task.
type TaskRepo struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	RepoName    string `json:"repo_name"`
	Spec        string `json:"spec"` // Per-repo PRD/spec from decomposition
	WorktreePath string `json:"worktree_path"`
	CreatedAt   time.Time `json:"created_at"`
}

// Lead represents an execution loop for a specific repo within a task.
type Lead struct {
	ID         string     `json:"id"`
	TaskRepoID string     `json:"task_repo_id"`
	Status     LeadStatus `json:"status"`
	Attempt    int        `json:"attempt"`
	Output     string     `json:"output"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Event represents an audit trail entry.
type Event struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	LeadID    string    `json:"lead_id"`
	Type      EventType `json:"type"`
	Payload   string    `json:"payload"` // JSON blob
	CreatedAt time.Time `json:"created_at"`
}

// AgenticDecision records the output of an ephemeral Claude session.
type AgenticDecision struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"task_id"`
	NodeType  AgenticNodeType `json:"node_type"`
	Input     string          `json:"input"`  // Prompt sent
	Output    string          `json:"output"` // Response received
	Model     string          `json:"model"`
	DurationMs int64         `json:"duration_ms"`
	CreatedAt time.Time       `json:"created_at"`
}

// LeadGoalStatus represents the lifecycle state of a goal within a lead.
type LeadGoalStatus string

const (
	LeadGoalPending  LeadGoalStatus = "pending"
	LeadGoalRunning  LeadGoalStatus = "running"
	LeadGoalComplete LeadGoalStatus = "complete"
	LeadGoalStuck    LeadGoalStatus = "stuck"
	LeadGoalFailed   LeadGoalStatus = "failed"
)

// LeadGoal represents a sub-goal within a lead execution.
type LeadGoal struct {
	ID          string         `json:"id"`
	LeadID      string         `json:"lead_id"`
	GoalIndex   int            `json:"goal_index"`
	Description string         `json:"description"`
	Status      LeadGoalStatus `json:"status"`
	Attempt     int            `json:"attempt"`
	Output      string         `json:"output"`
	VerdictJSON string         `json:"verdict_json"`
	StartedAt   *time.Time     `json:"started_at"`
	FinishedAt  *time.Time     `json:"finished_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Verdict is the structured output from a review step.
type Verdict struct {
	Pass    bool     `json:"pass"`
	Summary string   `json:"summary"`
	Issues  []string `json:"issues,omitempty"`
}

// LeadEvent is a structured event emitted by the lead shell script via stdout.
type LeadEvent struct {
	Type        string `json:"type"`
	Goal        int    `json:"goal,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	Description string `json:"description,omitempty"`
	Pass        *bool  `json:"pass,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Error       string `json:"error,omitempty"`
}

// GoalSpec defines a goal for the lead to execute. Written to .lead/goals.json.
type GoalSpec struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
}
