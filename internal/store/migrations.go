package store

import (
	"database/sql"
	"fmt"
)

// Migrate applies the schema to db idempotently. Safe to call on every Open.
func Migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'pending',
			template      TEXT,
			repos         TEXT NOT NULL DEFAULT '{}',
			workspace_dir TEXT NOT NULL DEFAULT '',
			log_level     TEXT NOT NULL DEFAULT 'standard',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			timestamp  DATETIME NOT NULL,
			type       TEXT NOT NULL,
			data       TEXT NOT NULL DEFAULT '{}'
		)`,

		`CREATE TABLE IF NOT EXISTS agent_runs (
			id             TEXT PRIMARY KEY,
			session_id     TEXT NOT NULL,
			name           TEXT NOT NULL,
			role           TEXT NOT NULL DEFAULT '',
			profile        TEXT NOT NULL DEFAULT '',
			repo_scope     TEXT NOT NULL DEFAULT '',
			workdir        TEXT NOT NULL DEFAULT '',
			branch         TEXT NOT NULL DEFAULT '',
			worktree_path  TEXT NOT NULL DEFAULT '',
			transport      TEXT NOT NULL DEFAULT 'bridge',
			tmux_session   TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL DEFAULT 'starting',
			created_at     DATETIME NOT NULL,
			updated_at     DATETIME NOT NULL,
			UNIQUE(session_id, name),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,

		`CREATE TABLE IF NOT EXISTS artifacts (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			kind       TEXT NOT NULL,
			path       TEXT NOT NULL,
			producer   TEXT NOT NULL DEFAULT '',
			summary    TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,

		`CREATE TABLE IF NOT EXISTS messages (
			id           TEXT PRIMARY KEY,
			session_id   TEXT NOT NULL,
			sender_id    TEXT NOT NULL,
			recipient_id TEXT NOT NULL DEFAULT '',
			type         TEXT NOT NULL DEFAULT 'instruction',
			content      TEXT NOT NULL,
			urgent       INTEGER NOT NULL DEFAULT 0,
			delivered    INTEGER NOT NULL DEFAULT 0,
			created_at   DATETIME NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_messages_pending ON messages(session_id, recipient_id, delivered, created_at)`,

		// FTS5 virtual table for full-text search over event type and data.
		`CREATE VIRTUAL TABLE IF NOT EXISTS events_fts
			USING fts5(type, data, content=events, content_rowid=id)`,

		// Keep FTS5 in sync with the events table.
		`CREATE TRIGGER IF NOT EXISTS events_ai AFTER INSERT ON events BEGIN
			INSERT INTO events_fts(rowid, type, data) VALUES (new.id, new.type, new.data);
		END`,

		`CREATE TRIGGER IF NOT EXISTS events_ad AFTER DELETE ON events BEGIN
			INSERT INTO events_fts(events_fts, rowid, type, data) VALUES ('delete', old.id, old.type, old.data);
		END`,

		`CREATE TRIGGER IF NOT EXISTS events_au AFTER UPDATE ON events BEGIN
			INSERT INTO events_fts(events_fts, rowid, type, data) VALUES ('delete', old.id, old.type, old.data);
			INSERT INTO events_fts(rowid, type, data) VALUES (new.id, new.type, new.data);
		END`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	// Idempotent column additions for existing tables.
	if err := addColumnIfNotExists(db, "sessions", "repos", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "sessions", "workspace_dir", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "sessions", "log_level", "TEXT NOT NULL DEFAULT 'standard'"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "hermes_session_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "branch", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "worktree_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist.
func addColumnIfNotExists(db *sql.DB, table, column, colDef string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // column already exists
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef))
	return err
}
