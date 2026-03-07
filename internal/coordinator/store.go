package coordinator

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// Store handles SQLite operations for coordinator-level entities:
// tasks, task_repos, leads, and agentic decisions.
type Store struct {
	db *sql.DB
}

// NewStore creates a new coordinator store backed by the given database connection.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// InsertTask creates a new task record.
func (s *Store) InsertTask(task *model.Task) error {
	now := time.Now().UTC()
	suffChecked := 0
	if task.SufficiencyChecked {
		suffChecked = 1
	}
	query := `INSERT INTO tasks (id, instance_id, description, source, source_ref, status, sufficiency_checked, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query,
		task.ID, task.InstanceID, task.Description, task.Source, task.SourceRef,
		string(task.Status), suffChecked, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting task: %w", err)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(taskID string) (*model.Task, error) {
	query := `SELECT id, instance_id, description, source, source_ref, status, sufficiency_checked, created_at, updated_at
		FROM tasks WHERE id = ?`
	row := s.db.QueryRow(query, taskID)

	var t model.Task
	var suffChecked int
	err := row.Scan(&t.ID, &t.InstanceID, &t.Description, &t.Source, &t.SourceRef,
		&t.Status, &suffChecked, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting task: %w", err)
	}
	t.SufficiencyChecked = suffChecked != 0
	return &t, nil
}

// UpdateTaskStatus transitions a task to a new status.
func (s *Store) UpdateTaskStatus(taskID string, status model.TaskStatus) error {
	now := time.Now().UTC()
	query := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, string(status), now, taskID)
	if err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}
	return nil
}

// GetTasksByStatus returns all tasks matching the given status.
func (s *Store) GetTasksByStatus(status model.TaskStatus) ([]model.Task, error) {
	query := `SELECT id, instance_id, description, source, source_ref, status, sufficiency_checked, created_at, updated_at
		FROM tasks WHERE status = ?`
	rows, err := s.db.Query(query, string(status))
	if err != nil {
		return nil, fmt.Errorf("querying tasks by status: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		var suffChecked int
		err := rows.Scan(&t.ID, &t.InstanceID, &t.Description, &t.Source, &t.SourceRef,
			&t.Status, &suffChecked, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		t.SufficiencyChecked = suffChecked != 0
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// InsertTaskRepo creates a new task_repo record.
func (s *Store) InsertTaskRepo(tr *model.TaskRepo) error {
	now := time.Now().UTC()
	query := `INSERT INTO task_repos (id, task_id, repo_name, spec, worktree_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query,
		tr.ID, tr.TaskID, tr.RepoName, tr.Spec, tr.WorktreePath, now,
	)
	if err != nil {
		return fmt.Errorf("inserting task repo: %w", err)
	}
	return nil
}

// GetTaskReposForTask returns all task_repos belonging to a task.
func (s *Store) GetTaskReposForTask(taskID string) ([]model.TaskRepo, error) {
	query := `SELECT id, task_id, repo_name, spec, worktree_path, created_at
		FROM task_repos WHERE task_id = ?`
	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("querying task repos: %w", err)
	}
	defer rows.Close()

	var repos []model.TaskRepo
	for rows.Next() {
		var tr model.TaskRepo
		err := rows.Scan(&tr.ID, &tr.TaskID, &tr.RepoName, &tr.Spec, &tr.WorktreePath, &tr.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning task repo: %w", err)
		}
		repos = append(repos, tr)
	}
	return repos, rows.Err()
}

// InsertLead creates a new lead record.
func (s *Store) InsertLead(lead *model.Lead) error {
	now := time.Now().UTC()
	query := `INSERT INTO leads (id, task_repo_id, status, attempt, output, started_at, finished_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query,
		lead.ID, lead.TaskRepoID, string(lead.Status), lead.Attempt, lead.Output,
		lead.StartedAt, lead.FinishedAt, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting lead: %w", err)
	}
	return nil
}

// GetLeadsForTask returns all leads for a task by joining through task_repos.
func (s *Store) GetLeadsForTask(taskID string) ([]model.Lead, error) {
	query := `SELECT l.id, l.task_repo_id, l.status, l.attempt, l.output, l.started_at, l.finished_at, l.created_at, l.updated_at
		FROM leads l
		JOIN task_repos tr ON l.task_repo_id = tr.id
		WHERE tr.task_id = ?`
	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("querying leads for task: %w", err)
	}
	defer rows.Close()

	var leads []model.Lead
	for rows.Next() {
		var l model.Lead
		var startedAt, finishedAt sql.NullTime
		err := rows.Scan(&l.ID, &l.TaskRepoID, &l.Status, &l.Attempt, &l.Output,
			&startedAt, &finishedAt, &l.CreatedAt, &l.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning lead: %w", err)
		}
		if startedAt.Valid {
			l.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			l.FinishedAt = &finishedAt.Time
		}
		leads = append(leads, l)
	}
	return leads, rows.Err()
}

// InsertAgenticDecision records an agentic decision.
func (s *Store) InsertAgenticDecision(d *model.AgenticDecision) error {
	now := time.Now().UTC()
	query := `INSERT INTO agentic_decisions (id, task_id, node_type, input, output, model, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query,
		d.ID, d.TaskID, string(d.NodeType), d.Input, d.Output, d.Model, d.DurationMs, now,
	)
	if err != nil {
		return fmt.Errorf("inserting agentic decision: %w", err)
	}
	return nil
}

// GetAgenticDecisionsForTask returns all agentic decisions for a task.
func (s *Store) GetAgenticDecisionsForTask(taskID string) ([]model.AgenticDecision, error) {
	query := `SELECT id, task_id, node_type, input, output, model, duration_ms, created_at
		FROM agentic_decisions WHERE task_id = ?`
	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("querying agentic decisions: %w", err)
	}
	defer rows.Close()

	var decisions []model.AgenticDecision
	for rows.Next() {
		var d model.AgenticDecision
		err := rows.Scan(&d.ID, &d.TaskID, &d.NodeType, &d.Input, &d.Output, &d.Model, &d.DurationMs, &d.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning agentic decision: %w", err)
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

// GetLeadsForTaskByLeadID retrieves a single lead by its ID.
func (s *Store) GetLeadsForTaskByLeadID(leadID string) (*model.Lead, error) {
	query := `SELECT id, task_repo_id, status, attempt, output, started_at, finished_at, created_at, updated_at
		FROM leads WHERE id = ?`
	row := s.db.QueryRow(query, leadID)

	var l model.Lead
	var startedAt, finishedAt sql.NullTime
	err := row.Scan(&l.ID, &l.TaskRepoID, &l.Status, &l.Attempt, &l.Output,
		&startedAt, &finishedAt, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting lead by ID: %w", err)
	}
	if startedAt.Valid {
		l.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		l.FinishedAt = &finishedAt.Time
	}
	return &l, nil
}

// GetTaskRepoByID retrieves a single task repo by its ID.
func (s *Store) GetTaskRepoByID(taskRepoID string) (*model.TaskRepo, error) {
	query := `SELECT id, task_id, repo_name, spec, worktree_path, created_at
		FROM task_repos WHERE id = ?`
	row := s.db.QueryRow(query, taskRepoID)

	var tr model.TaskRepo
	err := row.Scan(&tr.ID, &tr.TaskID, &tr.RepoName, &tr.Spec, &tr.WorktreePath, &tr.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting task repo by ID: %w", err)
	}
	return &tr, nil
}
