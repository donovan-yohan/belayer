package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// Store provides CRUD operations against the belayer SQLite database.
type Store struct {
	db *sql.DB
}

// New creates a Store backed by the given database connection.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// InsertTask inserts a task and its parsed goals in a single transaction.
func (s *Store) InsertTask(task *model.Task, goals []model.Goal) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	_, err = tx.Exec(
		`INSERT INTO tasks (id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.InstanceID, task.Spec, task.GoalsJSON, task.JiraRef, task.Status, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting task: %w", err)
	}

	for _, g := range goals {
		depsJSON, err := json.Marshal(g.DependsOn)
		if err != nil {
			return fmt.Errorf("marshaling depends_on for goal %s: %w", g.ID, err)
		}
		_, err = tx.Exec(
			`INSERT INTO goals (id, task_id, repo_name, description, depends_on, status, attempt, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			g.ID, g.TaskID, g.RepoName, g.Description, string(depsJSON), g.Status, 0, now,
		)
		if err != nil {
			return fmt.Errorf("inserting goal %s: %w", g.ID, err)
		}
	}

	_, err = tx.Exec(
		`INSERT INTO events (task_id, type, payload, created_at) VALUES (?, ?, ?, ?)`,
		task.ID, model.EventTaskCreated, "{}", now,
	)
	if err != nil {
		return fmt.Errorf("inserting task_created event: %w", err)
	}

	return tx.Commit()
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(id string) (*model.Task, error) {
	row := s.db.QueryRow(
		`SELECT id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	)
	t := &model.Task{}
	err := row.Scan(&t.ID, &t.InstanceID, &t.Spec, &t.GoalsJSON, &t.JiraRef, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning task: %w", err)
	}
	return t, nil
}

// ListTasksForInstance returns all tasks for the given instance, newest first.
func (s *Store) ListTasksForInstance(instanceID string) ([]model.Task, error) {
	rows, err := s.db.Query(
		`SELECT id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at
		 FROM tasks WHERE instance_id = ? ORDER BY created_at DESC`, instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.Spec, &t.GoalsJSON, &t.JiraRef, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetGoalsForTask returns all goals for a task.
func (s *Store) GetGoalsForTask(taskID string) ([]model.Goal, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, repo_name, description, depends_on, status, attempt, created_at, completed_at
		 FROM goals WHERE task_id = ? ORDER BY id`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying goals: %w", err)
	}
	defer rows.Close()

	var goals []model.Goal
	for rows.Next() {
		var g model.Goal
		var depsJSON string
		if err := rows.Scan(&g.ID, &g.TaskID, &g.RepoName, &g.Description, &depsJSON, &g.Status, &g.Attempt, &g.CreatedAt, &g.CompletedAt); err != nil {
			return nil, fmt.Errorf("scanning goal: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &g.DependsOn); err != nil {
			g.DependsOn = nil
		}
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

// UpdateTaskStatus updates a task's status.
func (s *Store) UpdateTaskStatus(taskID string, status model.TaskStatus) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), taskID,
	)
	return err
}

// UpdateGoalStatus updates a goal's status and optionally sets completed_at.
func (s *Store) UpdateGoalStatus(goalID string, status model.GoalStatus) error {
	var completedAt *time.Time
	if status == model.GoalStatusComplete {
		now := time.Now().UTC()
		completedAt = &now
	}
	_, err := s.db.Exec(
		`UPDATE goals SET status = ?, completed_at = ? WHERE id = ?`,
		status, completedAt, goalID,
	)
	return err
}

// InsertEvent adds an audit event.
func (s *Store) InsertEvent(taskID, goalID string, eventType model.EventType, payload string) error {
	_, err := s.db.Exec(
		`INSERT INTO events (task_id, goal_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		taskID, goalID, eventType, payload, time.Now().UTC(),
	)
	return err
}

// GetEventsForTask returns all events for a task, oldest first.
func (s *Store) GetEventsForTask(taskID string) ([]model.Event, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, goal_id, type, payload, created_at
		 FROM events WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.GoalID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetTasksByStatus returns all tasks with the given status.
func (s *Store) GetTasksByStatus(status model.TaskStatus) ([]model.Task, error) {
	rows, err := s.db.Query(
		`SELECT id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at
		 FROM tasks WHERE status = ? ORDER BY created_at DESC`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("querying tasks by status: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.Spec, &t.GoalsJSON, &t.JiraRef, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ValidateGoalsFile validates a parsed GoalsFile for consistency.
// It checks that all goal IDs are unique and all depends_on references exist
// within the same repo.
func ValidateGoalsFile(gf *model.GoalsFile) error {
	allIDs := make(map[string]string) // goal ID -> repo name
	for repoName, repoGoals := range gf.Repos {
		for _, goal := range repoGoals.Goals {
			if goal.ID == "" {
				return fmt.Errorf("goal in repo %q has empty ID", repoName)
			}
			if goal.Description == "" {
				return fmt.Errorf("goal %q in repo %q has empty description", goal.ID, repoName)
			}
			if existing, ok := allIDs[goal.ID]; ok {
				return fmt.Errorf("duplicate goal ID %q (in repos %q and %q)", goal.ID, existing, repoName)
			}
			allIDs[goal.ID] = repoName
		}
	}

	for repoName, repoGoals := range gf.Repos {
		for _, goal := range repoGoals.Goals {
			for _, dep := range goal.DependsOn {
				depRepo, ok := allIDs[dep]
				if !ok {
					return fmt.Errorf("goal %q depends on %q which does not exist", goal.ID, dep)
				}
				if depRepo != repoName {
					return fmt.Errorf("goal %q depends on %q which is in a different repo (%q vs %q)", goal.ID, dep, repoName, depRepo)
				}
			}
		}
	}

	return nil
}

// ValidateGoalsRepos checks that all repos in the goals file exist in the instance.
func ValidateGoalsRepos(gf *model.GoalsFile, instanceRepos []string) error {
	repoSet := make(map[string]bool, len(instanceRepos))
	for _, r := range instanceRepos {
		repoSet[r] = true
	}

	var missing []string
	for repoName := range gf.Repos {
		if !repoSet[repoName] {
			missing = append(missing, repoName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("goals.json references repos not in instance: %s", strings.Join(missing, ", "))
	}
	return nil
}

// GoalsFromFile converts a parsed GoalsFile into a slice of Goal models for a given task.
func GoalsFromFile(taskID string, gf *model.GoalsFile) []model.Goal {
	var goals []model.Goal
	for repoName, repoGoals := range gf.Repos {
		for _, spec := range repoGoals.Goals {
			deps := spec.DependsOn
			if deps == nil {
				deps = []string{}
			}
			goals = append(goals, model.Goal{
				ID:          spec.ID,
				TaskID:      taskID,
				RepoName:    repoName,
				Description: spec.Description,
				DependsOn:   deps,
				Status:      model.GoalStatusPending,
			})
		}
	}
	return goals
}

// GetPendingTasks returns tasks with status='pending' for the given instance, ordered by created_at ASC.
func (s *Store) GetPendingTasks(instanceID string) ([]model.Task, error) {
	rows, err := s.db.Query(
		`SELECT id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at
		 FROM tasks WHERE instance_id = ? AND status = 'pending' ORDER BY created_at ASC`, instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pending tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.Spec, &t.GoalsJSON, &t.JiraRef, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetActiveTasks returns tasks with status IN ('running', 'reviewing') for the given instance, ordered by created_at ASC.
func (s *Store) GetActiveTasks(instanceID string) ([]model.Task, error) {
	rows, err := s.db.Query(
		`SELECT id, instance_id, spec, goals_json, jira_ref, status, created_at, updated_at
		 FROM tasks WHERE instance_id = ? AND status IN ('running', 'reviewing') ORDER BY created_at ASC`, instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.Spec, &t.GoalsJSON, &t.JiraRef, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// IncrementGoalAttempt increments the attempt column by 1 for the given goal.
func (s *Store) IncrementGoalAttempt(goalID string) error {
	_, err := s.db.Exec(
		`UPDATE goals SET attempt = attempt + 1 WHERE id = ?`, goalID,
	)
	return err
}

// ResetGoalStatus sets the goal status back to pending and clears completed_at.
func (s *Store) ResetGoalStatus(goalID string) error {
	_, err := s.db.Exec(
		`UPDATE goals SET status = 'pending', completed_at = NULL WHERE id = ?`, goalID,
	)
	return err
}

// InsertAnchorReview inserts an anchor review record.
func (s *Store) InsertAnchorReview(review *model.SpotterReview) error {
	_, err := s.db.Exec(
		`INSERT INTO spotter_reviews (task_id, attempt, verdict, output, created_at) VALUES (?, ?, ?, ?, ?)`,
		review.TaskID, review.Attempt, review.Verdict, review.Output, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting anchor review: %w", err)
	}
	return nil
}

// GetAnchorReviewsForTask returns anchor reviews for a task, ordered by attempt ASC.
func (s *Store) GetAnchorReviewsForTask(taskID string) ([]model.SpotterReview, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, attempt, verdict, output, created_at
		 FROM spotter_reviews WHERE task_id = ? ORDER BY attempt ASC`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying anchor reviews: %w", err)
	}
	defer rows.Close()

	var reviews []model.SpotterReview
	for rows.Next() {
		var r model.SpotterReview
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Attempt, &r.Verdict, &r.Output, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning anchor review: %w", err)
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// InsertGoals inserts multiple goals in a single transaction.
// This is used for correction goals from spotter redistribution.
func (s *Store) InsertGoals(goals []model.Goal) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	for _, g := range goals {
		depsJSON, err := json.Marshal(g.DependsOn)
		if err != nil {
			return fmt.Errorf("marshaling depends_on for goal %s: %w", g.ID, err)
		}
		_, err = tx.Exec(
			`INSERT INTO goals (id, task_id, repo_name, description, depends_on, status, attempt, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			g.ID, g.TaskID, g.RepoName, g.Description, string(depsJSON), g.Status, 0, now,
		)
		if err != nil {
			return fmt.Errorf("inserting goal %s: %w", g.ID, err)
		}
	}

	return tx.Commit()
}
