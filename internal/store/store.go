package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Session represents a belayer session row.
type Session struct {
	ID        string
	Name      string
	Status    string
	Template  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionEvent represents a single event row associated with a session.
type SessionEvent struct {
	ID        int64
	SessionID string
	Timestamp time.Time
	Type      string
	Data      string
}

// WorkbenchState represents a workbench instance associated with a session.
type WorkbenchState struct {
	ID        string
	SessionID string
	Status    string
	Endpoints string
	Spec      string
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

	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, status, template, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.Name, session.Status, nullableString(session.Template),
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
		`SELECT id, name, status, COALESCE(template,''), created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	var sess Session
	var createdAt, updatedAt string
	err := row.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &createdAt, &updatedAt)
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
		`SELECT id, name, status, COALESCE(template,''), created_at, updated_at
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
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Status, &sess.Template, &createdAt, &updatedAt); err != nil {
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

// CreateWorkbench inserts a new workbench. If wb.ID is empty a UUID is
// generated. Returns the ID of the created workbench.
func (s *Store) CreateWorkbench(wb WorkbenchState) (string, error) {
	if wb.ID == "" {
		wb.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if wb.CreatedAt.IsZero() {
		wb.CreatedAt = now
	}
	if wb.UpdatedAt.IsZero() {
		wb.UpdatedAt = now
	}
	if wb.Status == "" {
		wb.Status = "pending"
	}
	if wb.Endpoints == "" {
		wb.Endpoints = "{}"
	}
	if wb.Spec == "" {
		wb.Spec = "{}"
	}

	_, err := s.db.Exec(
		`INSERT INTO workbenches (id, session_id, status, endpoints, spec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		wb.ID, wb.SessionID, wb.Status, wb.Endpoints, wb.Spec,
		wb.CreatedAt, wb.UpdatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("store: create workbench: %w", err)
	}
	return wb.ID, nil
}

// GetWorkbenchBySession retrieves a workbench by session ID. Returns a wrapped sql.ErrNoRows if not found.
func (s *Store) GetWorkbenchBySession(sessionID string) (WorkbenchState, error) {
	row := s.db.QueryRow(
		`SELECT id, session_id, status, COALESCE(endpoints,'{}'), COALESCE(spec,'{}'), created_at, updated_at
		 FROM workbenches WHERE session_id = ?`, sessionID,
	)
	var wb WorkbenchState
	var createdAt, updatedAt string
	err := row.Scan(&wb.ID, &wb.SessionID, &wb.Status, &wb.Endpoints, &wb.Spec, &createdAt, &updatedAt)
	if err != nil {
		return WorkbenchState{}, fmt.Errorf("store: get workbench: %w", err)
	}
	wb.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	wb.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return wb, nil
}

// UpdateWorkbenchStatus updates the status and updated_at of a workbench.
func (s *Store) UpdateWorkbenchStatus(id, status string) error {
	_, err := s.db.Exec(
		`UPDATE workbenches SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update workbench status: %w", err)
	}
	return nil
}

// UpdateWorkbenchEndpoints updates the endpoints JSON and updated_at of a workbench.
func (s *Store) UpdateWorkbenchEndpoints(id, endpoints string) error {
	_, err := s.db.Exec(
		`UPDATE workbenches SET endpoints = ?, updated_at = ? WHERE id = ?`,
		endpoints, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("store: update workbench endpoints: %w", err)
	}
	return nil
}

// DeleteWorkbench deletes a workbench by ID.
func (s *Store) DeleteWorkbench(id string) error {
	_, err := s.db.Exec(`DELETE FROM workbenches WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete workbench: %w", err)
	}
	return nil
}

// DeleteWorkbenchBySession deletes all workbenches for a session.
func (s *Store) DeleteWorkbenchBySession(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM workbenches WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("store: delete workbench by session: %w", err)
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
		`SELECT id, session_id, timestamp, type, data
		 FROM events WHERE session_id = ? ORDER BY timestamp ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query events: %w", err)
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
