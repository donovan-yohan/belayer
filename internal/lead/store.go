package lead

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// Store handles SQLite operations for lead execution tracking.
type Store struct {
	db *sql.DB
}

// NewStore creates a new lead store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// UpdateLeadStatus updates a lead's status and output.
func (s *Store) UpdateLeadStatus(leadID string, status model.LeadStatus, output string) error {
	now := time.Now().UTC()
	query := `UPDATE leads SET status = ?, output = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(status), output, now, leadID)
	if err != nil {
		return fmt.Errorf("updating lead status: %w", err)
	}
	return nil
}

// SetLeadStarted marks a lead as running and records the start time.
func (s *Store) SetLeadStarted(leadID string) error {
	now := time.Now().UTC()
	query := `UPDATE leads SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(model.LeadStatusRunning), now, now, leadID)
	if err != nil {
		return fmt.Errorf("setting lead started: %w", err)
	}
	return nil
}

// SetLeadFinished marks a lead as finished with a final status.
func (s *Store) SetLeadFinished(leadID string, status model.LeadStatus, output string) error {
	now := time.Now().UTC()
	query := `UPDATE leads SET status = ?, output = ?, finished_at = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(status), output, now, now, leadID)
	if err != nil {
		return fmt.Errorf("setting lead finished: %w", err)
	}
	return nil
}

// UpdateLeadAttempt increments the lead's attempt counter.
func (s *Store) UpdateLeadAttempt(leadID string, attempt int) error {
	now := time.Now().UTC()
	query := `UPDATE leads SET attempt = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, attempt, now, leadID)
	if err != nil {
		return fmt.Errorf("updating lead attempt: %w", err)
	}
	return nil
}

// InsertLeadGoal creates a new lead goal record.
func (s *Store) InsertLeadGoal(goal *model.LeadGoal) error {
	query := `INSERT INTO lead_goals (id, lead_id, goal_index, description, status, attempt, output, verdict_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	now := time.Now().UTC()
	_, err := s.db.Exec(query,
		goal.ID, goal.LeadID, goal.GoalIndex, goal.Description,
		string(goal.Status), goal.Attempt, goal.Output, goal.VerdictJSON,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting lead goal: %w", err)
	}
	return nil
}

// UpdateLeadGoalStatus updates a goal's status, attempt, output, and verdict.
func (s *Store) UpdateLeadGoalStatus(goalID string, status model.LeadGoalStatus, attempt int, output, verdictJSON string) error {
	now := time.Now().UTC()
	query := `UPDATE lead_goals SET status = ?, attempt = ?, output = ?, verdict_json = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(status), attempt, output, verdictJSON, now, goalID)
	if err != nil {
		return fmt.Errorf("updating lead goal status: %w", err)
	}
	return nil
}

// SetLeadGoalStarted marks a goal as running and records the start time.
func (s *Store) SetLeadGoalStarted(goalID string) error {
	now := time.Now().UTC()
	query := `UPDATE lead_goals SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(model.LeadGoalRunning), now, now, goalID)
	if err != nil {
		return fmt.Errorf("setting lead goal started: %w", err)
	}
	return nil
}

// SetLeadGoalFinished marks a goal with a final status and records the finish time.
func (s *Store) SetLeadGoalFinished(goalID string, status model.LeadGoalStatus, verdictJSON string) error {
	now := time.Now().UTC()
	query := `UPDATE lead_goals SET status = ?, verdict_json = ?, finished_at = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(status), verdictJSON, now, now, goalID)
	if err != nil {
		return fmt.Errorf("setting lead goal finished: %w", err)
	}
	return nil
}

// InsertEvent records an event in the events table.
func (s *Store) InsertEvent(taskID, leadID string, eventType model.EventType, payload string) error {
	id := fmt.Sprintf("evt-%s-%d", leadID, time.Now().UnixNano())
	query := `INSERT INTO events (id, task_id, lead_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query, id, taskID, leadID, string(eventType), payload, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}
	return nil
}

// GetLead retrieves a lead by ID.
func (s *Store) GetLead(leadID string) (*model.Lead, error) {
	query := `SELECT id, task_repo_id, status, attempt, output, started_at, finished_at, created_at, updated_at FROM leads WHERE id = ?`
	row := s.db.QueryRow(query, leadID)

	var l model.Lead
	var startedAt, finishedAt sql.NullTime
	err := row.Scan(&l.ID, &l.TaskRepoID, &l.Status, &l.Attempt, &l.Output, &startedAt, &finishedAt, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting lead: %w", err)
	}
	if startedAt.Valid {
		l.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		l.FinishedAt = &finishedAt.Time
	}
	return &l, nil
}

// GetLeadGoals retrieves all goals for a lead, ordered by goal_index.
func (s *Store) GetLeadGoals(leadID string) ([]model.LeadGoal, error) {
	query := `SELECT id, lead_id, goal_index, description, status, attempt, output, verdict_json, started_at, finished_at, created_at, updated_at
		FROM lead_goals WHERE lead_id = ? ORDER BY goal_index`
	rows, err := s.db.Query(query, leadID)
	if err != nil {
		return nil, fmt.Errorf("querying lead goals: %w", err)
	}
	defer rows.Close()

	var goals []model.LeadGoal
	for rows.Next() {
		var g model.LeadGoal
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(&g.ID, &g.LeadID, &g.GoalIndex, &g.Description, &g.Status, &g.Attempt, &g.Output, &g.VerdictJSON,
			&startedAt, &finishedAt, &g.CreatedAt, &g.UpdatedAt)
		if err != nil {
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
