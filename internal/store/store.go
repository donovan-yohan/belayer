package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Session represents a belayer session row.
type Session struct {
	ID           string
	Name         string
	Status       string
	Template     string
	Repos        string // JSON map: {"frontend": "/abs/path", "backend": "/abs/path"}
	WorkspaceDir string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SessionEvent represents a single event row associated with a session.
type SessionEvent struct {
	ID        int64
	SessionID string
	Timestamp time.Time
	Type      string
	Data      string
}

// AgentRun represents a launched agent/harness instance within a session.
type AgentRun struct {
	ID              string
	SessionID       string
	Name            string
	Role            string
	Profile         string
	RepoScope       string
	Workdir         string
	Branch          string // git branch the agent works on (empty = no worktree)
	WorktreePath    string // filesystem path of the git worktree (empty = shared workdir)
	Transport       string
	TmuxSession     string
	HermesSessionID string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Message represents a persistent message stored for pull-based delivery.
type Message struct {
	ID          string
	SessionID   string
	SenderID    string
	RecipientID string
	Type        string
	Content     string
	Urgent      bool
	Delivered   bool
	CreatedAt   time.Time
}

// Artifact represents a durable output produced by an agent during a run.
type Artifact struct {
	ID        string
	SessionID string
	Kind      string
	Path      string
	Producer  string
	Summary   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is a SQLite-backed session and event store.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dbPath, enables WAL mode, and
// runs Migrate. Use ":memory:" for ephemeral/test databases.
func Open(dbPath string) (*Store, error) {
	dsn := dbPath
	if dbPath != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)", dbPath)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateSession inserts a new session. If session.ID is empty a UUID is
// generated. Returns the ID of the created session.
func (s *Store) CreateSession(session Session) (string, error) {
	if session.ID == "" {
		session.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	if session.Status == "" {
		session.Status = "pending"
	}
	if session.Repos == "" {
		session.Repos = "{}"
	}

	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, status, template, repos, workspace_dir, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Name, session.Status, nullableString(session.Template),
		session.Repos, session.WorkspaceDir,
		session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("store: create session: %w", err)
	}
	return session.ID, nil
}

// GetSession retrieves a session by ID. Returns a wrapped sql.ErrNoRows if not found.
func (s *Store) GetSession(id string) (Session, error) {
	row := s.db.QueryRow(
		`SELECT id, name, status, COALESCE(template,''), COALESCE(repos,'{}'), COALESCE(workspace_dir,''), created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	var sess Session
	var createdAt, updatedAt string
	err := row.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &sess.Repos, &sess.WorkspaceDir, &createdAt, &updatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("store: get session: %w", err)
	}
	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return sess, nil
}

// ListSessions returns all sessions ordered by created_at DESC.
func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id, name, status, COALESCE(template,''), COALESCE(repos,'{}'), COALESCE(workspace_dir,''), created_at, updated_at
		 FROM sessions ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var createdAt, updatedAt string
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &sess.Repos, &sess.WorkspaceDir, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: list sessions scan: %w", err)
		}
		sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		sess.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// UpdateSessionStatus updates the status and updated_at of a session.
func (s *Store) UpdateSessionStatus(id, status string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update session status: %w", err)
	}
	return nil
}

// UpdateSessionWorkspaceDir updates the workspace_dir and updated_at of a session.
func (s *Store) UpdateSessionWorkspaceDir(id, workspaceDir string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET workspace_dir = ?, updated_at = ? WHERE id = ?`,
		workspaceDir, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update session workspace dir: %w", err)
	}
	return nil
}

// CreateAgentRun inserts a launched agent run. If run.ID is empty, a UUID is generated.
func (s *Store) CreateAgentRun(run AgentRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	if run.Status == "" {
		run.Status = "starting"
	}
	if run.Transport == "" {
		run.Transport = "bridge"
	}

	_, err := s.db.Exec(
		`INSERT INTO agent_runs (id, session_id, name, role, profile, repo_scope, workdir, branch, worktree_path, transport, tmux_session, hermes_session_id, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.SessionID, run.Name, run.Role, run.Profile, run.RepoScope, run.Workdir, run.Branch, run.WorktreePath, run.Transport, run.TmuxSession, run.HermesSessionID, run.Status, run.CreatedAt, run.UpdatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("store: create agent run: %w", err)
	}
	return run.ID, nil
}

// GetAgentRun retrieves a single agent run by session + name.
func (s *Store) GetAgentRun(sessionID, name string) (AgentRun, error) {
	row := s.db.QueryRow(
		`SELECT id, session_id, name, role, profile, repo_scope, workdir, branch, worktree_path, transport, tmux_session, hermes_session_id, status, created_at, updated_at
		 FROM agent_runs WHERE session_id = ? AND name = ?`, sessionID, name,
	)
	var run AgentRun
	var createdAt, updatedAt string
	err := row.Scan(&run.ID, &run.SessionID, &run.Name, &run.Role, &run.Profile, &run.RepoScope, &run.Workdir, &run.Branch, &run.WorktreePath, &run.Transport, &run.TmuxSession, &run.HermesSessionID, &run.Status, &createdAt, &updatedAt)
	if err != nil {
		return AgentRun{}, fmt.Errorf("store: get agent run: %w", err)
	}
	run.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return run, nil
}

// ListAgentRuns returns all agent runs for a session ordered by created_at.
func (s *Store) ListAgentRuns(sessionID string) ([]AgentRun, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, name, role, profile, repo_scope, workdir, branch, worktree_path, transport, tmux_session, hermes_session_id, status, created_at, updated_at
		 FROM agent_runs WHERE session_id = ? ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list agent runs: %w", err)
	}
	defer rows.Close()

	var runs []AgentRun
	for rows.Next() {
		var run AgentRun
		var createdAt, updatedAt string
		if err := rows.Scan(&run.ID, &run.SessionID, &run.Name, &run.Role, &run.Profile, &run.RepoScope, &run.Workdir, &run.Branch, &run.WorktreePath, &run.Transport, &run.TmuxSession, &run.HermesSessionID, &run.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: list agent runs scan: %w", err)
		}
		run.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// UpdateAgentRunStatus updates status/tmux session metadata.
func (s *Store) UpdateAgentRunStatus(sessionID, name, status string) error {
	_, err := s.db.Exec(
		`UPDATE agent_runs SET status = ?, updated_at = ? WHERE session_id = ? AND name = ?`,
		status, time.Now().UTC(), sessionID, name,
	)
	if err != nil {
		return fmt.Errorf("store: update agent run status: %w", err)
	}
	return nil
}

// UpdateAgentRunWorkdir updates the workdir for an agent run.
func (s *Store) UpdateAgentRunWorkdir(sessionID, name, workdir string) error {
	_, err := s.db.Exec(
		`UPDATE agent_runs SET workdir = ?, updated_at = ? WHERE session_id = ? AND name = ?`,
		workdir, time.Now().UTC(), sessionID, name,
	)
	if err != nil {
		return fmt.Errorf("store: update agent run workdir: %w", err)
	}
	return nil
}

// UpdateAgentRunTmuxSession updates the tmux session name for an agent run.
func (s *Store) UpdateAgentRunTmuxSession(sessionID, name, tmuxSession string) error {
	_, err := s.db.Exec(
		`UPDATE agent_runs SET tmux_session = ?, updated_at = ? WHERE session_id = ? AND name = ?`,
		tmuxSession, time.Now().UTC(), sessionID, name,
	)
	if err != nil {
		return fmt.Errorf("store: update agent run tmux session: %w", err)
	}
	return nil
}

// UpdateAgentRunWorktree updates the branch and worktree_path for an agent run.
func (s *Store) UpdateAgentRunWorktree(sessionID, name, branch, worktreePath string) error {
	_, err := s.db.Exec(
		`UPDATE agent_runs SET branch = ?, worktree_path = ?, updated_at = ? WHERE session_id = ? AND name = ?`,
		branch, worktreePath, time.Now().UTC(), sessionID, name,
	)
	if err != nil {
		return fmt.Errorf("store: update agent run worktree: %w", err)
	}
	return nil
}

// UpdateAgentRunHermesSessionID updates the hermes_session_id for an agent run.
func (s *Store) UpdateAgentRunHermesSessionID(sessionID, name, hermesSessionID string) error {
	_, err := s.db.Exec(
		`UPDATE agent_runs SET hermes_session_id = ?, updated_at = ? WHERE session_id = ? AND name = ?`,
		hermesSessionID, time.Now().UTC(), sessionID, name,
	)
	if err != nil {
		return fmt.Errorf("store: update agent run hermes session id: %w", err)
	}
	return nil
}

// CreateMessage inserts a new message. If msg.ID is empty a UUID is generated.
// Returns the ID of the created message.
func (s *Store) CreateMessage(msg Message) (string, error) {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Type == "" {
		msg.Type = "instruction"
	}
	urgent := 0
	if msg.Urgent {
		urgent = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO messages (id, session_id, sender_id, recipient_id, type, content, urgent, delivered, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		msg.ID, msg.SessionID, msg.SenderID, msg.RecipientID, msg.Type, msg.Content, urgent, msg.CreatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("store: create message: %w", err)
	}
	return msg.ID, nil
}

// PendingMessages returns undelivered messages for a specific agent in a session,
// ordered by created_at ASC. If afterID is non-empty, only messages created after
// the message with that ID are returned.
func (s *Store) PendingMessages(sessionID, recipientID string, afterID string) ([]Message, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if afterID == "" {
		rows, err = s.db.Query(
			`SELECT id, session_id, sender_id, recipient_id, type, content, urgent, delivered, created_at
			 FROM messages
			 WHERE session_id = ? AND recipient_id = ? AND delivered = 0
			 ORDER BY created_at ASC`,
			sessionID, recipientID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, session_id, sender_id, recipient_id, type, content, urgent, delivered, created_at
			 FROM messages
			 WHERE session_id = ? AND recipient_id = ? AND delivered = 0
			   AND created_at > (SELECT created_at FROM messages WHERE id = ?)
			 ORDER BY created_at ASC`,
			sessionID, recipientID, afterID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("store: pending messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var createdAt string
		var urgent, delivered int
		if err := rows.Scan(&m.ID, &m.SessionID, &m.SenderID, &m.RecipientID, &m.Type, &m.Content, &urgent, &delivered, &createdAt); err != nil {
			return nil, fmt.Errorf("store: pending messages scan: %w", err)
		}
		m.Urgent = urgent != 0
		m.Delivered = delivered != 0
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return msgs, nil
}

// MarkDelivered marks a message as delivered so it is excluded from future
// PendingMessages results.
func (s *Store) MarkDelivered(id string) error {
	_, err := s.db.Exec(
		`UPDATE messages SET delivered = 1 WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("store: mark delivered: %w", err)
	}
	return nil
}

// CreateArtifact inserts a new artifact record.
func (s *Store) CreateArtifact(a Artifact) (string, error) {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO artifacts (id, session_id, kind, path, producer, summary, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.Kind, a.Path, a.Producer, a.Summary, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("store: create artifact: %w", err)
	}
	return a.ID, nil
}

// GetArtifact retrieves a single artifact by ID.
func (s *Store) GetArtifact(id string) (Artifact, error) {
	row := s.db.QueryRow(
		`SELECT id, session_id, kind, path, producer, summary, created_at, updated_at
		 FROM artifacts WHERE id = ?`, id,
	)
	var a Artifact
	var createdAt, updatedAt string
	err := row.Scan(&a.ID, &a.SessionID, &a.Kind, &a.Path, &a.Producer, &a.Summary, &createdAt, &updatedAt)
	if err != nil {
		return Artifact{}, fmt.Errorf("store: get artifact: %w", err)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return a, nil
}

// ListArtifacts returns artifacts for a session ordered by created_at.
func (s *Store) ListArtifacts(sessionID string) ([]Artifact, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, kind, path, producer, summary, created_at, updated_at
		 FROM artifacts WHERE session_id = ? ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list artifacts: %w", err)
	}
	defer rows.Close()
	var artifacts []Artifact
	for rows.Next() {
		var a Artifact
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.SessionID, &a.Kind, &a.Path, &a.Producer, &a.Summary, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: list artifacts scan: %w", err)
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

// LogEvent inserts an event row for a session.
func (s *Store) LogEvent(event SessionEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Data == "" {
		event.Data = "{}"
	}
	_, err := s.db.Exec(
		`INSERT INTO events (session_id, timestamp, type, data) VALUES (?, ?, ?, ?)`,
		event.SessionID, event.Timestamp, event.Type, event.Data,
	)
	if err != nil {
		return fmt.Errorf("store: log event: %w", err)
	}
	return nil
}

// QueryEvents returns all events for a session ordered by timestamp ASC.
func (s *Store) QueryEvents(sessionID string) ([]SessionEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, type, data
		 FROM events WHERE session_id = ? ORDER BY timestamp ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// QueryEventsAfter returns all events for a session with IDs greater than afterID,
// ordered by ID ASC.
func (s *Store) QueryEventsAfter(sessionID string, afterID int64) ([]SessionEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, type, data
		 FROM events
		 WHERE session_id = ? AND id > ?
		 ORDER BY id ASC`,
		sessionID, afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query events after: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// QueryEventsForSessionsAfter returns all events for the provided session IDs
// with IDs greater than afterID, ordered by ID ASC.
func (s *Store) QueryEventsForSessionsAfter(sessionIDs []string, afterID int64) ([]SessionEvent, error) {
	if len(sessionIDs) == 0 {
		return []SessionEvent{}, nil
	}

	placeholders := make([]string, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs)+1)
	for i, sessionID := range sessionIDs {
		placeholders[i] = "?"
		args = append(args, sessionID)
	}
	args = append(args, afterID)

	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT id, session_id, timestamp, type, data
		 FROM events
		 WHERE session_id IN (%s) AND id > ?
		 ORDER BY id ASC`, strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query events for sessions after: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// SearchEvents performs a FTS5 MATCH query over type and data fields.
// Returns matching events ordered by rowid (insertion order).
func (s *Store) SearchEvents(query string) ([]SessionEvent, error) {
	rows, err := s.db.Query(
		`SELECT e.id, e.session_id, e.timestamp, e.type, e.data
		 FROM events e
		 JOIN events_fts f ON f.rowid = e.id
		 WHERE events_fts MATCH ?
		 ORDER BY e.id ASC`, query,
	)
	if err != nil {
		return nil, fmt.Errorf("store: search events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// scanEvents reads event rows into a slice.
func scanEvents(rows *sql.Rows) ([]SessionEvent, error) {
	var events []SessionEvent
	for rows.Next() {
		var evt SessionEvent
		var ts string
		if err := rows.Scan(&evt.ID, &evt.SessionID, &ts, &evt.Type, &evt.Data); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		evt.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if events == nil {
		events = []SessionEvent{}
	}
	return events, nil
}

// nullableString returns nil for empty strings (stores NULL in SQLite).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
