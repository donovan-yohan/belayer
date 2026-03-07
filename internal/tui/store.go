package tui

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// Store provides read-only queries optimized for the TUI dashboard.
type Store struct {
	db *sql.DB
}

// NewStore creates a TUI store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// TaskSummary is a denormalized view of a task for the task list.
type TaskSummary struct {
	Task        model.Task
	RepoCount   int
	LeadCount   int
	LeadsDone   int
	LeadsFailed int
	LeadsStuck  int
}

// LeadDetail includes repo name and goals alongside lead info.
type LeadDetail struct {
	Lead     model.Lead
	RepoName string
	Goals    []model.LeadGoal
}

// EventEntry is a recent event for display.
type EventEntry struct {
	Event    model.Event
	RepoName string
}

// ListInstances returns all instances from the database.
func (s *Store) ListInstances() ([]model.Instance, error) {
	query := `SELECT id, name, path, created_at, updated_at FROM instances ORDER BY name`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	defer rows.Close()

	var instances []model.Instance
	for rows.Next() {
		var inst model.Instance
		if err := rows.Scan(&inst.ID, &inst.Name, &inst.Path, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning instance: %w", err)
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// ListTaskSummaries returns all tasks for an instance with lead counts.
func (s *Store) ListTaskSummaries(instanceID string) ([]TaskSummary, error) {
	query := `SELECT id, instance_id, description, source, source_ref, status, sufficiency_checked, created_at, updated_at
		FROM tasks WHERE instance_id = ? ORDER BY created_at DESC`
	rows, err := s.db.Query(query, instanceID)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	var summaries []TaskSummary
	for rows.Next() {
		var t model.Task
		var suffChecked int
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.Description, &t.Source, &t.SourceRef,
			&t.Status, &suffChecked, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		t.SufficiencyChecked = suffChecked != 0

		summary := TaskSummary{Task: t}
		if err := s.fillTaskCounts(&summary); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

func (s *Store) fillTaskCounts(summary *TaskSummary) error {
	repoQuery := `SELECT COUNT(*) FROM task_repos WHERE task_id = ?`
	if err := s.db.QueryRow(repoQuery, summary.Task.ID).Scan(&summary.RepoCount); err != nil {
		return fmt.Errorf("counting repos: %w", err)
	}

	leadQuery := `SELECT COUNT(*) FROM leads l JOIN task_repos tr ON l.task_repo_id = tr.id WHERE tr.task_id = ?`
	if err := s.db.QueryRow(leadQuery, summary.Task.ID).Scan(&summary.LeadCount); err != nil {
		return fmt.Errorf("counting leads: %w", err)
	}

	doneQuery := `SELECT COUNT(*) FROM leads l JOIN task_repos tr ON l.task_repo_id = tr.id WHERE tr.task_id = ? AND l.status = 'complete'`
	if err := s.db.QueryRow(doneQuery, summary.Task.ID).Scan(&summary.LeadsDone); err != nil {
		return fmt.Errorf("counting done leads: %w", err)
	}

	failedQuery := `SELECT COUNT(*) FROM leads l JOIN task_repos tr ON l.task_repo_id = tr.id WHERE tr.task_id = ? AND l.status = 'failed'`
	if err := s.db.QueryRow(failedQuery, summary.Task.ID).Scan(&summary.LeadsFailed); err != nil {
		return fmt.Errorf("counting failed leads: %w", err)
	}

	stuckQuery := `SELECT COUNT(*) FROM leads l JOIN task_repos tr ON l.task_repo_id = tr.id WHERE tr.task_id = ? AND l.status = 'stuck'`
	if err := s.db.QueryRow(stuckQuery, summary.Task.ID).Scan(&summary.LeadsStuck); err != nil {
		return fmt.Errorf("counting stuck leads: %w", err)
	}

	return nil
}

// GetLeadDetails returns all leads for a task with repo names and goals.
func (s *Store) GetLeadDetails(taskID string) ([]LeadDetail, error) {
	query := `SELECT l.id, l.task_repo_id, l.status, l.attempt, l.output,
		l.started_at, l.finished_at, l.created_at, l.updated_at,
		tr.repo_name
		FROM leads l
		JOIN task_repos tr ON l.task_repo_id = tr.id
		WHERE tr.task_id = ?
		ORDER BY tr.repo_name, l.created_at DESC`
	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting lead details: %w", err)
	}
	defer rows.Close()

	var details []LeadDetail
	for rows.Next() {
		var d LeadDetail
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&d.Lead.ID, &d.Lead.TaskRepoID, &d.Lead.Status,
			&d.Lead.Attempt, &d.Lead.Output,
			&startedAt, &finishedAt, &d.Lead.CreatedAt, &d.Lead.UpdatedAt,
			&d.RepoName); err != nil {
			return nil, fmt.Errorf("scanning lead detail: %w", err)
		}
		if startedAt.Valid {
			d.Lead.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			d.Lead.FinishedAt = &finishedAt.Time
		}

		goals, err := s.getLeadGoals(d.Lead.ID)
		if err != nil {
			return nil, err
		}
		d.Goals = goals
		details = append(details, d)
	}
	return details, rows.Err()
}

func (s *Store) getLeadGoals(leadID string) ([]model.LeadGoal, error) {
	query := `SELECT id, lead_id, goal_index, description, status, attempt, output, verdict_json,
		started_at, finished_at, created_at, updated_at
		FROM lead_goals WHERE lead_id = ? ORDER BY goal_index`
	rows, err := s.db.Query(query, leadID)
	if err != nil {
		return nil, fmt.Errorf("getting lead goals: %w", err)
	}
	defer rows.Close()

	var goals []model.LeadGoal
	for rows.Next() {
		var g model.LeadGoal
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&g.ID, &g.LeadID, &g.GoalIndex, &g.Description, &g.Status,
			&g.Attempt, &g.Output, &g.VerdictJSON,
			&startedAt, &finishedAt, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning lead goal: %w", err)
		}
		if startedAt.Valid {
			g.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			g.FinishedAt = &finishedAt.Time
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// GetRecentEvents returns the most recent events for a task.
func (s *Store) GetRecentEvents(taskID string, limit int) ([]EventEntry, error) {
	query := `SELECT e.id, e.task_id, e.lead_id, e.type, e.payload, e.created_at,
		COALESCE(tr.repo_name, '') as repo_name
		FROM events e
		LEFT JOIN leads l ON e.lead_id = l.id
		LEFT JOIN task_repos tr ON l.task_repo_id = tr.id
		WHERE e.task_id = ?
		ORDER BY e.created_at DESC
		LIMIT ?`
	rows, err := s.db.Query(query, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("getting recent events: %w", err)
	}
	defer rows.Close()

	var entries []EventEntry
	for rows.Next() {
		var entry EventEntry
		if err := rows.Scan(&entry.Event.ID, &entry.Event.TaskID, &entry.Event.LeadID,
			&entry.Event.Type, &entry.Event.Payload, &entry.Event.CreatedAt,
			&entry.RepoName); err != nil {
			return nil, fmt.Errorf("scanning event entry: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// RelativeTime formats a time as a human-readable relative string.
func RelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
