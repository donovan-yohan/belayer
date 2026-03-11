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

// InsertProblem inserts a problem and its parsed climbs in a single transaction.
func (s *Store) InsertProblem(problem *model.Problem, climbs []model.Climb) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	_, err = tx.Exec(
		`INSERT INTO problems (id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		problem.ID, problem.CragID, problem.Spec, problem.ClimbsJSON, problem.JiraRef, problem.Status, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting problem: %w", err)
	}

	for _, c := range climbs {
		depsJSON, err := json.Marshal(c.DependsOn)
		if err != nil {
			return fmt.Errorf("marshaling depends_on for climb %s: %w", c.ID, err)
		}
		_, err = tx.Exec(
			`INSERT INTO climbs (id, problem_id, repo_name, description, depends_on, status, attempt, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.ProblemID, c.RepoName, c.Description, string(depsJSON), c.Status, 0, now,
		)
		if err != nil {
			return fmt.Errorf("inserting climb %s: %w", c.ID, err)
		}
	}

	_, err = tx.Exec(
		`INSERT INTO events (problem_id, type, payload, created_at) VALUES (?, ?, ?, ?)`,
		problem.ID, model.EventProblemCreated, "{}", now,
	)
	if err != nil {
		return fmt.Errorf("inserting problem_created event: %w", err)
	}

	return tx.Commit()
}

// GetProblem retrieves a problem by ID.
func (s *Store) GetProblem(id string) (*model.Problem, error) {
	row := s.db.QueryRow(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at
		 FROM problems WHERE id = ?`, id,
	)
	p := &model.Problem{}
	err := row.Scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("problem %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning problem: %w", err)
	}
	return p, nil
}

// ListProblemsForCrag returns all problems for the given crag, newest first.
func (s *Store) ListProblemsForCrag(cragID string) ([]model.Problem, error) {
	rows, err := s.db.Query(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? ORDER BY created_at DESC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

// GetClimbsForProblem returns all climbs for a problem.
func (s *Store) GetClimbsForProblem(problemID string) ([]model.Climb, error) {
	rows, err := s.db.Query(
		`SELECT id, problem_id, repo_name, description, depends_on, status, attempt, created_at, completed_at
		 FROM climbs WHERE problem_id = ? ORDER BY id`, problemID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying climbs: %w", err)
	}
	defer rows.Close()

	var climbs []model.Climb
	for rows.Next() {
		var c model.Climb
		var depsJSON string
		if err := rows.Scan(&c.ID, &c.ProblemID, &c.RepoName, &c.Description, &depsJSON, &c.Status, &c.Attempt, &c.CreatedAt, &c.CompletedAt); err != nil {
			return nil, fmt.Errorf("scanning climb: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &c.DependsOn); err != nil {
			c.DependsOn = nil
		}
		climbs = append(climbs, c)
	}
	return climbs, rows.Err()
}

// UpdateProblemStatus updates a problem's status.
func (s *Store) UpdateProblemStatus(problemID string, status model.ProblemStatus) error {
	_, err := s.db.Exec(
		`UPDATE problems SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), problemID,
	)
	return err
}

// UpdateClimbStatus updates a climb's status and optionally sets completed_at.
func (s *Store) UpdateClimbStatus(climbID string, status model.ClimbStatus) error {
	var completedAt *time.Time
	if status == model.ClimbStatusComplete {
		now := time.Now().UTC()
		completedAt = &now
	}
	_, err := s.db.Exec(
		`UPDATE climbs SET status = ?, completed_at = ? WHERE id = ?`,
		status, completedAt, climbID,
	)
	return err
}

// InsertEvent adds an audit event.
func (s *Store) InsertEvent(problemID, climbID string, eventType model.EventType, payload string) error {
	_, err := s.db.Exec(
		`INSERT INTO events (problem_id, climb_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		problemID, climbID, eventType, payload, time.Now().UTC(),
	)
	return err
}

// GetEventsForProblem returns all events for a problem, oldest first.
func (s *Store) GetEventsForProblem(problemID string) ([]model.Event, error) {
	rows, err := s.db.Query(
		`SELECT id, problem_id, climb_id, type, payload, created_at
		 FROM events WHERE problem_id = ? ORDER BY created_at ASC`, problemID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.ProblemID, &e.ClimbID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetProblemsByStatus returns all problems with the given status.
func (s *Store) GetProblemsByStatus(status model.ProblemStatus) ([]model.Problem, error) {
	rows, err := s.db.Query(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at
		 FROM problems WHERE status = ? ORDER BY created_at DESC`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("querying problems by status: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

// ValidateClimbsFile validates a parsed ClimbsFile for consistency.
// It checks that all climb IDs are unique and all depends_on references exist
// within the same repo.
func ValidateClimbsFile(cf *model.ClimbsFile) error {
	allIDs := make(map[string]string) // climb ID -> repo name
	for repoName, repoClimbs := range cf.Repos {
		for _, climb := range repoClimbs.Climbs {
			if climb.ID == "" {
				return fmt.Errorf("climb in repo %q has empty ID", repoName)
			}
			if climb.Description == "" {
				return fmt.Errorf("climb %q in repo %q has empty description", climb.ID, repoName)
			}
			if existing, ok := allIDs[climb.ID]; ok {
				return fmt.Errorf("duplicate climb ID %q (in repos %q and %q)", climb.ID, existing, repoName)
			}
			allIDs[climb.ID] = repoName
		}
	}

	for repoName, repoClimbs := range cf.Repos {
		for _, climb := range repoClimbs.Climbs {
			for _, dep := range climb.DependsOn {
				depRepo, ok := allIDs[dep]
				if !ok {
					return fmt.Errorf("climb %q depends on %q which does not exist", climb.ID, dep)
				}
				if depRepo != repoName {
					return fmt.Errorf("climb %q depends on %q which is in a different repo (%q vs %q)", climb.ID, dep, repoName, depRepo)
				}
			}
		}
	}

	return nil
}

// ValidateClimbsRepos checks that all repos in the climbs file exist in the crag.
func ValidateClimbsRepos(cf *model.ClimbsFile, cragRepos []string) error {
	repoSet := make(map[string]bool, len(cragRepos))
	for _, r := range cragRepos {
		repoSet[r] = true
	}

	var missing []string
	for repoName := range cf.Repos {
		if !repoSet[repoName] {
			missing = append(missing, repoName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("climbs.json references repos not in crag: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ClimbsFromFile converts a parsed ClimbsFile into a slice of Climb models for a given problem.
func ClimbsFromFile(problemID string, cf *model.ClimbsFile) []model.Climb {
	var climbs []model.Climb
	for repoName, repoClimbs := range cf.Repos {
		for _, spec := range repoClimbs.Climbs {
			deps := spec.DependsOn
			if deps == nil {
				deps = []string{}
			}
			climbs = append(climbs, model.Climb{
				ID:          spec.ID,
				ProblemID:   problemID,
				RepoName:    repoName,
				Description: spec.Description,
				DependsOn:   deps,
				Status:      model.ClimbStatusPending,
			})
		}
	}
	return climbs
}

// GetPendingProblems returns problems with status='pending' for the given crag, ordered by created_at ASC.
func (s *Store) GetPendingProblems(cragID string) ([]model.Problem, error) {
	rows, err := s.db.Query(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? AND status = 'pending' ORDER BY created_at ASC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pending problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

// GetActiveProblems returns problems with status IN ('running', 'reviewing') for the given crag, ordered by created_at ASC.
func (s *Store) GetActiveProblems(cragID string) ([]model.Problem, error) {
	rows, err := s.db.Query(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? AND status IN ('running', 'reviewing') ORDER BY created_at ASC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying active problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		var p model.Problem
		if err := rows.Scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, p)
	}
	return problems, rows.Err()
}

// IncrementClimbAttempt increments the attempt column by 1 for the given climb.
func (s *Store) IncrementClimbAttempt(climbID string) error {
	_, err := s.db.Exec(
		`UPDATE climbs SET attempt = attempt + 1 WHERE id = ?`, climbID,
	)
	return err
}

// ResetClimbStatus sets the climb status back to pending and clears completed_at.
func (s *Store) ResetClimbStatus(climbID string) error {
	_, err := s.db.Exec(
		`UPDATE climbs SET status = 'pending', completed_at = NULL WHERE id = ?`, climbID,
	)
	return err
}

// InsertAnchorReview inserts an anchor review record.
func (s *Store) InsertAnchorReview(review *model.SpotterReview) error {
	_, err := s.db.Exec(
		`INSERT INTO spotter_reviews (problem_id, attempt, verdict, output, created_at) VALUES (?, ?, ?, ?, ?)`,
		review.ProblemID, review.Attempt, review.Verdict, review.Output, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting anchor review: %w", err)
	}
	return nil
}

// GetAnchorReviewsForProblem returns anchor reviews for a problem, ordered by attempt ASC.
func (s *Store) GetAnchorReviewsForProblem(problemID string) ([]model.SpotterReview, error) {
	rows, err := s.db.Query(
		`SELECT id, problem_id, attempt, verdict, output, created_at
		 FROM spotter_reviews WHERE problem_id = ? ORDER BY attempt ASC`, problemID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying anchor reviews: %w", err)
	}
	defer rows.Close()

	var reviews []model.SpotterReview
	for rows.Next() {
		var r model.SpotterReview
		if err := rows.Scan(&r.ID, &r.ProblemID, &r.Attempt, &r.Verdict, &r.Output, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning anchor review: %w", err)
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// InsertClimbs inserts multiple climbs in a single transaction.
// This is used for correction climbs from spotter redistribution.
func (s *Store) InsertClimbs(climbs []model.Climb) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	for _, c := range climbs {
		depsJSON, err := json.Marshal(c.DependsOn)
		if err != nil {
			return fmt.Errorf("marshaling depends_on for climb %s: %w", c.ID, err)
		}
		_, err = tx.Exec(
			`INSERT INTO climbs (id, problem_id, repo_name, description, depends_on, status, attempt, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.ProblemID, c.RepoName, c.Description, string(depsJSON), c.Status, 0, now,
		)
		if err != nil {
			return fmt.Errorf("inserting climb %s: %w", c.ID, err)
		}
	}

	return tx.Commit()
}
