package memory

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteMemory is a SQLite-backed implementation of MemoryStore.
type SQLiteMemory struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at dbPath, enables WAL mode,
// and runs migrations. Use ":memory:" for ephemeral/test databases.
func Open(dbPath string) (*SQLiteMemory, error) {
	dsn := dbPath
	if dbPath != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=journal_mode(wal)", dbPath)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memory: open db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory: migrate: %w", err)
	}

	return &SQLiteMemory{db: db}, nil
}

// Close closes the underlying database connection.
func (m *SQLiteMemory) Close() error {
	return m.db.Close()
}

// migrate applies the schema idempotently. Safe to call on every Open.
func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS core_memory (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			key        TEXT NOT NULL,
			value      TEXT NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(session_id, key)
		)`,

		`CREATE TABLE IF NOT EXISTS archival_memory (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			content    TEXT NOT NULL,
			tags       TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL
		)`,

		// FTS5 virtual table for full-text search over content and tags.
		`CREATE VIRTUAL TABLE IF NOT EXISTS archival_memory_fts
			USING fts5(content, tags, content=archival_memory, content_rowid=id)`,

		// Keep FTS5 in sync with archival_memory.
		`CREATE TRIGGER IF NOT EXISTS archival_ai AFTER INSERT ON archival_memory BEGIN
			INSERT INTO archival_memory_fts(rowid, content, tags)
			VALUES (new.id, new.content, new.tags);
		END`,

		`CREATE TRIGGER IF NOT EXISTS archival_ad AFTER DELETE ON archival_memory BEGIN
			INSERT INTO archival_memory_fts(archival_memory_fts, rowid, content, tags)
			VALUES ('delete', old.id, old.content, old.tags);
		END`,

		`CREATE TRIGGER IF NOT EXISTS archival_au AFTER UPDATE ON archival_memory BEGIN
			INSERT INTO archival_memory_fts(archival_memory_fts, rowid, content, tags)
			VALUES ('delete', old.id, old.content, old.tags);
			INSERT INTO archival_memory_fts(rowid, content, tags)
			VALUES (new.id, new.content, new.tags);
		END`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// WriteCore upserts a key-value pair for the given session. If the key
// already exists for the session the value and updated_at are updated.
func (m *SQLiteMemory) WriteCore(sessionID, key, value string) error {
	_, err := m.db.Exec(
		`INSERT INTO core_memory (session_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id, key) DO UPDATE SET
		   value      = excluded.value,
		   updated_at = excluded.updated_at`,
		sessionID, key, value, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("memory: write core: %w", err)
	}
	return nil
}

// ReadCore returns all core entries for a session ordered by key.
// Returns an empty (non-nil) slice if the session has no entries.
func (m *SQLiteMemory) ReadCore(sessionID string) ([]CoreEntry, error) {
	rows, err := m.db.Query(
		`SELECT id, session_id, key, value, updated_at
		 FROM core_memory
		 WHERE session_id = ?
		 ORDER BY key ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("memory: read core: %w", err)
	}
	defer rows.Close()

	entries := []CoreEntry{}
	for rows.Next() {
		var e CoreEntry
		var updatedAt string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Key, &e.Value, &updatedAt); err != nil {
			return nil, fmt.Errorf("memory: read core scan: %w", err)
		}
		e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read core rows: %w", err)
	}
	return entries, nil
}

// WriteArchival appends a new archival entry for the given session.
func (m *SQLiteMemory) WriteArchival(sessionID, content, tags, source string) error {
	_, err := m.db.Exec(
		`INSERT INTO archival_memory (session_id, content, tags, source, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, content, tags, source, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("memory: write archival: %w", err)
	}
	return nil
}

// SearchArchival performs a full-text search across all archival entries using
// FTS5 MATCH, returning at most limit results ordered by rank (relevance).
// Returns an empty (non-nil) slice when nothing matches.
func (m *SQLiteMemory) SearchArchival(query string, limit int) ([]ArchivalEntry, error) {
	rows, err := m.db.Query(
		`SELECT a.id, a.session_id, a.content, a.tags, a.source, a.created_at
		 FROM archival_memory a
		 JOIN archival_memory_fts f ON f.rowid = a.id
		 WHERE archival_memory_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("memory: search archival: %w", err)
	}
	defer rows.Close()

	entries := []ArchivalEntry{}
	for rows.Next() {
		var e ArchivalEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Content, &e.Tags, &e.Source, &createdAt); err != nil {
			return nil, fmt.Errorf("memory: search archival scan: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: search archival rows: %w", err)
	}
	return entries, nil
}

// ListArchival returns all archival entries ordered by created_at ASC.
// Pass limit=0 for no limit.
func (m *SQLiteMemory) ListArchival(limit int) ([]ArchivalEntry, error) {
	query := `SELECT id, session_id, content, tags, source, created_at
	          FROM archival_memory ORDER BY created_at ASC`
	var args []any
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: list archival: %w", err)
	}
	defer rows.Close()

	entries := []ArchivalEntry{}
	for rows.Next() {
		var e ArchivalEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Content, &e.Tags, &e.Source, &createdAt); err != nil {
			return nil, fmt.Errorf("memory: list archival scan: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: list archival rows: %w", err)
	}
	return entries, nil
}

// Recall combines ReadCore for the session with SearchArchival for the query.
func (m *SQLiteMemory) Recall(sessionID, query string) (RecallResult, error) {
	core, err := m.ReadCore(sessionID)
	if err != nil {
		return RecallResult{}, fmt.Errorf("memory: recall core: %w", err)
	}

	archival, err := m.SearchArchival(query, 20)
	if err != nil {
		return RecallResult{}, fmt.Errorf("memory: recall archival: %w", err)
	}

	return RecallResult{
		Core:     core,
		Archival: archival,
	}, nil
}
