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

// scanProblem scans a problem row using the provided scan function.
func scanProblem(scan func(...any) error) (*model.Problem, error) {
	p := &model.Problem{}
	var trackerIssueID sql.NullString
	err := scan(&p.ID, &p.CragID, &p.Spec, &p.ClimbsJSON, &p.JiraRef, &trackerIssueID, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.TrackerIssueID = trackerIssueID.String
	return p, nil
}

// GetProblem retrieves a problem by ID.
func (s *Store) GetProblem(id string) (*model.Problem, error) {
	row := s.db.QueryRow(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
		 FROM problems WHERE id = ?`, id,
	)
	p, err := scanProblem(row.Scan)
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
		`SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? ORDER BY created_at DESC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		p, err := scanProblem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, *p)
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
		`SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
		 FROM problems WHERE status = ? ORDER BY created_at DESC`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("querying problems by status: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		p, err := scanProblem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, *p)
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
		`SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? AND status = 'pending' ORDER BY created_at ASC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pending problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		p, err := scanProblem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, *p)
	}
	return problems, rows.Err()
}

// GetActiveProblems returns problems with in-flight statuses for the given crag, ordered by created_at ASC.
// In-flight statuses include: running, reviewing, imported, enriching, pr_creating, pr_monitoring, ci_fixing, review_reacting.
func (s *Store) GetActiveProblems(cragID string) ([]model.Problem, error) {
	rows, err := s.db.Query(
		`SELECT id, crag_id, spec, climbs_json, jira_ref, tracker_issue_id, status, created_at, updated_at
		 FROM problems WHERE crag_id = ? AND status IN ('running', 'reviewing', 'imported', 'enriching', 'pr_creating', 'pr_monitoring', 'ci_fixing', 'review_reacting') ORDER BY created_at ASC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying active problems: %w", err)
	}
	defer rows.Close()

	var problems []model.Problem
	for rows.Next() {
		p, err := scanProblem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning problem: %w", err)
		}
		problems = append(problems, *p)
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

// InsertTrackerIssue inserts or replaces a tracker issue.
// An empty ProblemID is stored as NULL to satisfy the foreign key constraint.
func (s *Store) InsertTrackerIssue(issue *model.TrackerIssue) error {
	problemID := sql.NullString{String: issue.ProblemID, Valid: issue.ProblemID != ""}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tracker_issues (id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issue.ID, issue.Provider, issue.Title, issue.Body, issue.CommentsJSON, issue.LabelsJSON,
		issue.Priority, issue.Assignee, issue.URL, issue.RawJSON, problemID, issue.SyncedAt,
	)
	return err
}

func scanTrackerIssue(scan func(...any) error) (*model.TrackerIssue, error) {
	ti := &model.TrackerIssue{}
	var problemID sql.NullString
	err := scan(&ti.ID, &ti.Provider, &ti.Title, &ti.Body, &ti.CommentsJSON, &ti.LabelsJSON,
		&ti.Priority, &ti.Assignee, &ti.URL, &ti.RawJSON, &problemID, &ti.SyncedAt)
	if err != nil {
		return nil, err
	}
	ti.ProblemID = problemID.String
	return ti, nil
}

// GetTrackerIssue retrieves a tracker issue by ID.
func (s *Store) GetTrackerIssue(id string) (*model.TrackerIssue, error) {
	row := s.db.QueryRow(
		`SELECT id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at
		 FROM tracker_issues WHERE id = ?`, id,
	)
	ti, err := scanTrackerIssue(row.Scan)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tracker issue %q not found", id)
	}
	return ti, err
}

// ListTrackerIssues returns all tracker issues, newest first.
// If unlinkedOnly is true, only issues with no problem_id are returned.
func (s *Store) ListTrackerIssues(unlinkedOnly bool) ([]model.TrackerIssue, error) {
	query := `SELECT id, provider, title, body, comments_json, labels_json, priority, assignee, url, raw_json, problem_id, synced_at FROM tracker_issues`
	if unlinkedOnly {
		query += ` WHERE problem_id IS NULL`
	}
	query += ` ORDER BY synced_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var issues []model.TrackerIssue
	for rows.Next() {
		ti, err := scanTrackerIssue(rows.Scan)
		if err != nil {
			return nil, err
		}
		issues = append(issues, *ti)
	}
	return issues, rows.Err()
}

// LinkTrackerIssueToProblem sets the problem_id for a tracker issue.
func (s *Store) LinkTrackerIssueToProblem(issueID, problemID string) error {
	_, err := s.db.Exec(`UPDATE tracker_issues SET problem_id = ? WHERE id = ?`, problemID, issueID)
	return err
}

// scanPullRequest scans a pull_request row using the provided scan function.
func scanPullRequest(scan func(...any) error) (*model.PullRequest, error) {
	pr := &model.PullRequest{}
	var lastPolledAt sql.NullTime
	err := scan(
		&pr.ID, &pr.ProblemID, &pr.RepoName, &pr.PRNumber, &pr.URL,
		&pr.StackPosition, &pr.StackSize, &pr.CIStatus, &pr.CIFixCount,
		&pr.ReviewStatus, &pr.State, &lastPolledAt, &pr.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lastPolledAt.Valid {
		pr.LastPolledAt = &lastPolledAt.Time
	}
	return pr, nil
}

// InsertPullRequest inserts a pull request and returns its auto-generated ID.
func (s *Store) InsertPullRequest(pr *model.PullRequest) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO pull_requests (problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, last_polled_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pr.ProblemID, pr.RepoName, pr.PRNumber, pr.URL, pr.StackPosition, pr.StackSize,
		pr.CIStatus, pr.CIFixCount, pr.ReviewStatus, pr.State, pr.LastPolledAt, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting pull request: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting last insert id: %w", err)
	}
	return id, nil
}

// GetPullRequest retrieves a pull request by ID.
func (s *Store) GetPullRequest(id int64) (*model.PullRequest, error) {
	row := s.db.QueryRow(
		`SELECT id, problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, last_polled_at, created_at
		 FROM pull_requests WHERE id = ?`, id,
	)
	pr, err := scanPullRequest(row.Scan)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pull request %d not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning pull request: %w", err)
	}
	return pr, nil
}

// ListPullRequestsForProblem returns all pull requests for the given problem.
func (s *Store) ListPullRequestsForProblem(problemID string) ([]model.PullRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, problem_id, repo_name, pr_number, url, stack_position, stack_size, ci_status, ci_fix_count, review_status, state, last_polled_at, created_at
		 FROM pull_requests WHERE problem_id = ? ORDER BY created_at ASC`, problemID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pull requests: %w", err)
	}
	defer rows.Close()

	var prs []model.PullRequest
	for rows.Next() {
		pr, err := scanPullRequest(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning pull request: %w", err)
		}
		prs = append(prs, *pr)
	}
	return prs, rows.Err()
}

// ListMonitoredPullRequests returns open pull requests for problems belonging to the given crag.
func (s *Store) ListMonitoredPullRequests(cragID string) ([]model.PullRequest, error) {
	rows, err := s.db.Query(
		`SELECT pr.id, pr.problem_id, pr.repo_name, pr.pr_number, pr.url, pr.stack_position, pr.stack_size, pr.ci_status, pr.ci_fix_count, pr.review_status, pr.state, pr.last_polled_at, pr.created_at
		 FROM pull_requests pr
		 JOIN problems p ON pr.problem_id = p.id
		 WHERE p.crag_id = ? AND pr.state = 'open'
		 ORDER BY pr.created_at ASC`, cragID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying monitored pull requests: %w", err)
	}
	defer rows.Close()

	var prs []model.PullRequest
	for rows.Next() {
		pr, err := scanPullRequest(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scanning pull request: %w", err)
		}
		prs = append(prs, *pr)
	}
	return prs, rows.Err()
}

// UpdatePullRequestCI updates ci_status and ci_fix_count for a pull request.
func (s *Store) UpdatePullRequestCI(id int64, ciStatus string, ciFixCount int) error {
	_, err := s.db.Exec(
		`UPDATE pull_requests SET ci_status = ?, ci_fix_count = ?, last_polled_at = ? WHERE id = ?`,
		ciStatus, ciFixCount, time.Now().UTC(), id,
	)
	return err
}

// UpdatePullRequestReview updates review_status for a pull request.
func (s *Store) UpdatePullRequestReview(id int64, reviewStatus string) error {
	_, err := s.db.Exec(
		`UPDATE pull_requests SET review_status = ?, last_polled_at = ? WHERE id = ?`,
		reviewStatus, time.Now().UTC(), id,
	)
	return err
}

// UpdatePullRequestState updates the state of a pull request.
func (s *Store) UpdatePullRequestState(id int64, state string) error {
	_, err := s.db.Exec(
		`UPDATE pull_requests SET state = ?, last_polled_at = ? WHERE id = ?`,
		state, time.Now().UTC(), id,
	)
	return err
}

// InsertPRReaction inserts a PR reaction record.
func (s *Store) InsertPRReaction(reaction *model.PRReaction) error {
	_, err := s.db.Exec(
		`INSERT INTO pr_reactions (pr_id, trigger_type, trigger_payload, action_taken, lead_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		reaction.PRID, reaction.TriggerType, reaction.TriggerPayload, reaction.ActionTaken, reaction.LeadID, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting pr reaction: %w", err)
	}
	return nil
}

// ListPRReactions returns all reactions for the given pull request, ordered by created_at ASC.
func (s *Store) ListPRReactions(prID int64) ([]model.PRReaction, error) {
	rows, err := s.db.Query(
		`SELECT id, pr_id, trigger_type, trigger_payload, action_taken, lead_id, created_at
		 FROM pr_reactions WHERE pr_id = ? ORDER BY created_at ASC`, prID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying pr reactions: %w", err)
	}
	defer rows.Close()

	var reactions []model.PRReaction
	for rows.Next() {
		var r model.PRReaction
		if err := rows.Scan(&r.ID, &r.PRID, &r.TriggerType, &r.TriggerPayload, &r.ActionTaken, &r.LeadID, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning pr reaction: %w", err)
		}
		reactions = append(reactions, r)
	}
	return reactions, rows.Err()
}

// InsertEnvironment inserts an environment record for a problem.
func (s *Store) InsertEnvironment(problemID, providerCommand, envName, envJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO environments (problem_id, provider_command, env_name, env_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		problemID, providerCommand, envName, envJSON, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting environment: %w", err)
	}
	return nil
}

// GetEnvironment retrieves the environment record for a problem.
func (s *Store) GetEnvironment(problemID string) (*model.Environment, error) {
	row := s.db.QueryRow(
		`SELECT problem_id, provider_command, env_name, env_json, created_at FROM environments WHERE problem_id = ?`,
		problemID,
	)
	e := &model.Environment{}
	err := row.Scan(&e.ProblemID, &e.ProviderCommand, &e.EnvName, &e.EnvJSON, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("environment for problem %q not found", problemID)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning environment: %w", err)
	}
	return e, nil
}

// DeleteEnvironment removes the environment record for a problem.
func (s *Store) DeleteEnvironment(problemID string) error {
	_, err := s.db.Exec(`DELETE FROM environments WHERE problem_id = ?`, problemID)
	return err
}

// ListEnvironments returns all environment records ordered by created_at ASC.
func (s *Store) ListEnvironments() ([]model.Environment, error) {
	rows, err := s.db.Query(
		`SELECT problem_id, provider_command, env_name, env_json, created_at FROM environments ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying environments: %w", err)
	}
	defer rows.Close()

	var envs []model.Environment
	for rows.Next() {
		var e model.Environment
		if err := rows.Scan(&e.ProblemID, &e.ProviderCommand, &e.EnvName, &e.EnvJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning environment: %w", err)
		}
		envs = append(envs, e)
	}
	return envs, rows.Err()
}

// UpdateClimbWorktreePath sets the worktree_path for a climb.
func (s *Store) UpdateClimbWorktreePath(climbID, worktreePath string) error {
	_, err := s.db.Exec(`UPDATE climbs SET worktree_path = ? WHERE id = ?`, worktreePath, climbID)
	return err
}

// GetClimbWorktreePath returns the worktree_path for a climb.
func (s *Store) GetClimbWorktreePath(climbID string) (string, error) {
	var path string
	err := s.db.QueryRow(`SELECT worktree_path FROM climbs WHERE id = ?`, climbID).Scan(&path)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("climb %q not found", climbID)
	}
	if err != nil {
		return "", fmt.Errorf("scanning worktree_path: %w", err)
	}
	return path, nil
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
