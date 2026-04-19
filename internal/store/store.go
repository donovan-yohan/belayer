package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/trace"
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
	LogLevel     string // "standard" or "verbose"; frozen at creation time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SessionEvent represents a single event row associated with a session.
type SessionEvent struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Data      string    `json:"data"`

	// TraceFile is the absolute path to the fragment file for spilled events (omitted when NULL).
	TraceFile string `json:"trace_file,omitempty"`
	// TraceOffset is the byte offset within TraceFile where this event's payload begins.
	TraceOffset int64 `json:"trace_offset,omitempty"`
	// TraceLength is the byte length of this event's payload in TraceFile.
	TraceLength int64 `json:"trace_length,omitempty"`
	// TraceFragment is the fragment basename (e.g. "0001") derived from TraceFile,
	// stripped of .jsonl and .jsonl.zst suffixes. Absent when TraceFile is empty.
	TraceFragment string `json:"trace_fragment,omitempty"`
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

// DB exposes the underlying *sql.DB for tests. Do not use in production code paths — go through Store methods.
func (s *Store) DB() *sql.DB { return s.db }

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
	if session.LogLevel == "" {
		session.LogLevel = "standard"
	}

	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, status, template, repos, workspace_dir, log_level, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Name, session.Status, nullableString(session.Template),
		session.Repos, session.WorkspaceDir, session.LogLevel,
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
		`SELECT id, name, status, COALESCE(template,''), COALESCE(repos,'{}'), COALESCE(workspace_dir,''), COALESCE(log_level,'standard'), created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	var sess Session
	var createdAt, updatedAt string
	err := row.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &sess.Repos, &sess.WorkspaceDir, &sess.LogLevel, &createdAt, &updatedAt)
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
		`SELECT id, name, status, COALESCE(template,''), COALESCE(repos,'{}'), COALESCE(workspace_dir,''), COALESCE(log_level,'standard'), created_at, updated_at
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
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &sess.Repos, &sess.WorkspaceDir, &sess.LogLevel, &createdAt, &updatedAt); err != nil {
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

// UpdateSessionStatusIf conditionally updates the session status only if
// the current status matches expectedStatus. Returns true if the row was
// updated (i.e., the expected status matched). This prevents race conditions
// where concurrent callers overwrite each other's transitions.
func (s *Store) UpdateSessionStatusIf(id, expectedStatus, newStatus string) (bool, error) {
	result, err := s.db.Exec(
		`UPDATE sessions SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		newStatus, time.Now().UTC(), id, expectedStatus,
	)
	if err != nil {
		return false, fmt.Errorf("store: conditional update session status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: rows affected: %w", err)
	}
	return rows > 0, nil
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

// InsertEventWithSpill inserts an event row with optional trace fragment refs.
// If frag.Path is non-empty, trace_file/trace_offset/trace_length columns are
// populated. Otherwise equivalent to LogEvent (trace_* columns remain NULL).
func (s *Store) InsertEventWithSpill(event SessionEvent, frag trace.Fragment) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Data == "" {
		event.Data = "{}"
	}

	var traceFile sql.NullString
	var traceOffset, traceLength sql.NullInt64
	if frag.Path != "" {
		traceFile = sql.NullString{String: frag.Path, Valid: true}
		traceOffset = sql.NullInt64{Int64: frag.Offset, Valid: true}
		traceLength = sql.NullInt64{Int64: frag.Length, Valid: true}
	}

	_, err := s.db.Exec(
		`INSERT INTO events (session_id, timestamp, type, data, trace_file, trace_offset, trace_length)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.SessionID, event.Timestamp, event.Type, event.Data,
		traceFile, traceOffset, traceLength,
	)
	if err != nil {
		return fmt.Errorf("store: insert event with spill: %w", err)
	}
	return nil
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
		`SELECT id, session_id, timestamp, type, data,
		        COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0),
		        trace_file IS NOT NULL
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
		`SELECT id, session_id, timestamp, type, data,
		        COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0),
		        trace_file IS NOT NULL
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

// QueryEventsWindow returns events for a session with optional lower bound
// (afterID), optional upper bound (beforeID), and a limit (capped at 1000).
//
//   - afterID=0  → no lower bound (start from the beginning).
//   - beforeID=0 → no upper bound (return up to the end).
//   - limit<=0   → default 1000; limit>1000 is capped to 1000.
//
// Events are returned ordered by id ASC.
func (s *Store) QueryEventsWindow(sessionID string, afterID, beforeID int64, limit int) ([]SessionEvent, error) {
	const maxLimit = 1000
	effectiveLimit := limit
	if effectiveLimit <= 0 || effectiveLimit > maxLimit {
		effectiveLimit = maxLimit
	}

	var sb strings.Builder
	sb.WriteString(`SELECT id, session_id, timestamp, type, data,
	        COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0),
	        trace_file IS NOT NULL
	 FROM events WHERE session_id = ?`)
	args := []any{sessionID}

	if afterID > 0 {
		sb.WriteString(` AND id > ?`)
		args = append(args, afterID)
	}
	if beforeID > 0 {
		sb.WriteString(` AND id < ?`)
		args = append(args, beforeID)
	}
	sb.WriteString(` ORDER BY id ASC LIMIT ?`)
	args = append(args, effectiveLimit)

	rows, err := s.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: query events window: %w", err)
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
		fmt.Sprintf(`SELECT id, session_id, timestamp, type, data,
		        COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0),
		        trace_file IS NOT NULL
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
		`SELECT e.id, e.session_id, e.timestamp, e.type, e.data,
		        COALESCE(e.trace_file,''), COALESCE(e.trace_offset,0), COALESCE(e.trace_length,0),
		        e.trace_file IS NOT NULL
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

// MaxEventID returns the maximum event ID in the store, or 0 if the table is
// empty. Used by the SSE handler to populate daemon_hello.last_id.
func (s *Store) MaxEventID() (int64, error) {
	var maxID int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&maxID)
	if err != nil {
		return 0, fmt.Errorf("store: max event id: %w", err)
	}
	return maxID, nil
}

// SearchPredicates is the query shape for SearchEventsV1.
// Zero-valued predicates are treated as "no filter".
type SearchPredicates struct {
	Q          string // FTS5 MATCH query (may be empty)
	SessionID  string // exact session_id filter
	TypePrefix string // events where type LIKE prefix||'%'
	Agent      string // events where json_extract(data,'$.agent') == agent
	AfterID    int64  // id > after (0 = no filter)
	BeforeID   int64  // id < before (0 = no filter)
	Limit      int    // cap 1000; if 0 use 1000
	DescOrder  bool   // true when caller wants id DESC (no-params default)
}

const searchMaxLimit = 1000

// SearchEventsV1 executes a bounded, predicated search. Accepts ctx so handlers
// can enforce a timeout. Returns events sorted by id (ASC by default, DESC
// when p.DescOrder is true). LIMIT is always applied (cap 1000).
func (s *Store) SearchEventsV1(ctx context.Context, p SearchPredicates) ([]SessionEvent, error) {
	limit := p.Limit
	if limit <= 0 || limit > searchMaxLimit {
		limit = searchMaxLimit
	}

	var sb strings.Builder
	var args []any

	if p.Q != "" {
		// FTS5 INNER JOIN path.
		sb.WriteString(`SELECT e.id, e.session_id, e.timestamp, e.type, e.data,
        COALESCE(e.trace_file,''), COALESCE(e.trace_offset,0), COALESCE(e.trace_length,0),
        e.trace_file IS NOT NULL
FROM events e
JOIN events_fts f ON f.rowid = e.id
WHERE events_fts MATCH ?`)
		args = append(args, p.Q)
	} else {
		sb.WriteString(`SELECT e.id, e.session_id, e.timestamp, e.type, e.data,
        COALESCE(e.trace_file,''), COALESCE(e.trace_offset,0), COALESCE(e.trace_length,0),
        e.trace_file IS NOT NULL
FROM events e
WHERE 1=1`)
	}

	if p.SessionID != "" {
		sb.WriteString(` AND e.session_id = ?`)
		args = append(args, p.SessionID)
	}

	if p.TypePrefix != "" {
		// Escape LIKE special chars in the prefix so they match literally.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p.TypePrefix)
		sb.WriteString(` AND e.type LIKE ? || '%' ESCAPE '\'`)
		args = append(args, escaped)
	}

	if p.Agent != "" {
		sb.WriteString(` AND json_extract(e.data, '$.agent') = ?`)
		args = append(args, p.Agent)
	}

	if p.AfterID > 0 {
		sb.WriteString(` AND e.id > ?`)
		args = append(args, p.AfterID)
	}

	if p.BeforeID > 0 {
		sb.WriteString(` AND e.id < ?`)
		args = append(args, p.BeforeID)
	}

	order := "ASC"
	if p.DescOrder {
		order = "DESC"
	}
	sb.WriteString(` ORDER BY e.id ` + order + ` LIMIT ?`)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// scanEvents reads event rows into a slice. Each row must provide 9 columns:
// id, session_id, timestamp, type, data,
// COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0),
// trace_file IS NOT NULL (boolean indicating whether trace columns are set).
func scanEvents(rows *sql.Rows) ([]SessionEvent, error) {
	var events []SessionEvent
	for rows.Next() {
		var evt SessionEvent
		var ts string
		var hasTrace bool
		if err := rows.Scan(
			&evt.ID, &evt.SessionID, &ts, &evt.Type, &evt.Data,
			&evt.TraceFile, &evt.TraceOffset, &evt.TraceLength,
			&hasTrace,
		); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		evt.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		// If trace columns are not set, zero out the COALESCE'd defaults so
		// omitempty JSON tags suppress them correctly.
		if !hasTrace {
			evt.TraceFile = ""
			evt.TraceOffset = 0
			evt.TraceLength = 0
		} else {
			// Derive TraceFragment from the basename of TraceFile, stripping
			// .jsonl.zst and .jsonl suffixes so consumers get just the index
			// (e.g. "0001") for use in trace-slice requests.
			base := filepath.Base(evt.TraceFile)
			base = strings.TrimSuffix(base, ".jsonl.zst")
			base = strings.TrimSuffix(base, ".jsonl")
			evt.TraceFragment = base
		}
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

// ListMessagesInSession returns all messages for a session ordered by created_at ASC.
func (s *Store) ListMessagesInSession(sessionID string) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, sender_id, recipient_id, type, content, urgent, delivered, created_at
		 FROM messages
		 WHERE session_id = ?
		 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list messages in session: %w", err)
	}
	defer rows.Close()

	msgs := []Message{}
	for rows.Next() {
		var m Message
		var createdAt string
		var urgent, delivered int
		if err := rows.Scan(&m.ID, &m.SessionID, &m.SenderID, &m.RecipientID, &m.Type, &m.Content, &urgent, &delivered, &createdAt); err != nil {
			return nil, fmt.Errorf("store: list messages in session scan: %w", err)
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

// ListMessagesBetween returns messages where (sender_id, recipient_id) is either
// (agentA, agentB) or (agentB, agentA), ordered by created_at ASC.
func (s *Store) ListMessagesBetween(sessionID, agentA, agentB string) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, sender_id, recipient_id, type, content, urgent, delivered, created_at
		 FROM messages
		 WHERE session_id = ?
		   AND (
		     (sender_id = ? AND recipient_id = ?)
		     OR (sender_id = ? AND recipient_id = ?)
		   )
		 ORDER BY created_at ASC`,
		sessionID, agentA, agentB, agentB, agentA,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list messages between: %w", err)
	}
	defer rows.Close()

	msgs := []Message{}
	for rows.Next() {
		var m Message
		var createdAt string
		var urgent, delivered int
		if err := rows.Scan(&m.ID, &m.SessionID, &m.SenderID, &m.RecipientID, &m.Type, &m.Content, &urgent, &delivered, &createdAt); err != nil {
			return nil, fmt.Errorf("store: list messages between scan: %w", err)
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

// nullableString returns nil for empty strings (stores NULL in SQLite).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LookupCursor returns the last_id for a reader+session cursor pair. Returns 0
// if no row exists or if the row is expired (updated_at < now - ttl). A ttl of
// 0 disables the expiry check.
func (s *Store) LookupCursor(readerID, sessionID string) (int64, error) {
	var lastID int64
	var updatedAt string
	err := s.db.QueryRow(
		`SELECT last_id, updated_at FROM reader_cursors WHERE reader_id = ? AND session_id = ?`,
		readerID, sessionID,
	).Scan(&lastID, &updatedAt)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: lookup cursor: %w", err)
	}
	// Check expiry: row is expired if updated_at < now - 24h.
	t, _ := time.Parse(time.RFC3339Nano, updatedAt)
	if t.IsZero() {
		// Try SQLite default CURRENT_TIMESTAMP format.
		t, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	}
	if !t.IsZero() && time.Since(t) > 24*time.Hour {
		return 0, nil
	}
	return lastID, nil
}

// UpdateCursor upserts the last_id for a reader+session cursor pair.
func (s *Store) UpdateCursor(readerID, sessionID string, lastID int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO reader_cursors (reader_id, session_id, last_id, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(reader_id, session_id) DO UPDATE SET last_id=excluded.last_id, updated_at=excluded.updated_at`,
		readerID, sessionID, lastID, now,
	)
	if err != nil {
		return fmt.Errorf("store: update cursor: %w", err)
	}
	return nil
}

// SweepExpiredCursors deletes reader_cursors rows where updated_at is older
// than now-ttl. Returns the number of rows deleted.
func (s *Store) SweepExpiredCursors(ttl time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-ttl).Format(time.RFC3339Nano)
	result, err := s.db.Exec(
		`DELETE FROM reader_cursors WHERE updated_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store: sweep expired cursors: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: sweep expired cursors rows affected: %w", err)
	}
	return n, nil
}
