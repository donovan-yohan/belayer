package store

import (
	"database/sql"
	"fmt"
	"strings"
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
			kind           TEXT NOT NULL DEFAULT 'main',
			profile        TEXT NOT NULL DEFAULT '',
			repo_scope     TEXT NOT NULL DEFAULT '',
			workdir        TEXT NOT NULL DEFAULT '',
			branch         TEXT NOT NULL DEFAULT '',
			worktree_path  TEXT NOT NULL DEFAULT '',
			transport      TEXT NOT NULL DEFAULT 'bridge',
			tmux_session   TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL DEFAULT 'starting',
			outcome        TEXT NOT NULL DEFAULT 'active',
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
			delivered_at DATETIME,
			acknowledged_at DATETIME,
			created_at   DATETIME NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,

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

		`CREATE TABLE IF NOT EXISTS reader_cursors (
			reader_id  TEXT NOT NULL,
			session_id TEXT NOT NULL,
			last_id    INTEGER NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (reader_id, session_id)
		)`,
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
	if err := addColumnIfNotExists(db, "agent_runs", "kind", "TEXT NOT NULL DEFAULT 'main'"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "branch", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "worktree_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "events", "trace_file", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "events", "trace_offset", "INTEGER"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "events", "trace_length", "INTEGER"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "destructive_actions", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "last_destructive_cmd", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "agent_runs", "outcome", "TEXT NOT NULL DEFAULT 'active'"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "messages", "delivered_at", "DATETIME"); err != nil {
		return err
	}
	if err := addColumnIfNotExists(db, "messages", "acknowledged_at", "DATETIME"); err != nil {
		return err
	}

	if _, err := db.Exec(`UPDATE messages
		SET delivered_at = COALESCE(delivered_at, created_at),
		    delivered = 1
		WHERE delivered = 1 AND delivered_at IS NULL`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE messages
		SET delivered = CASE WHEN delivered_at IS NULL THEN 0 ELSE 1 END
		WHERE (delivered = 1 AND delivered_at IS NULL)
		   OR (delivered = 0 AND delivered_at IS NOT NULL)`); err != nil {
		return err
	}

	if err := ensureIndexSQL(db, "idx_messages_pending", `CREATE INDEX idx_messages_pending
		ON messages(session_id, recipient_id, created_at)
		WHERE delivered_at IS NULL`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_unacked
		ON messages(session_id, recipient_id, created_at)
		WHERE acknowledged_at IS NULL`); err != nil {
		return err
	}

	return nil
}

func ensureIndexSQL(db *sql.DB, name, createSQL string) error {
	var existing sql.NullString
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&existing)
	if err == sql.ErrNoRows {
		_, err = db.Exec(createSQL)
		return err
	}
	if err != nil {
		return err
	}
	if normalizeSQL(existing.String) == normalizeSQL(createSQL) {
		return nil
	}
	if _, err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", name)); err != nil {
		return err
	}
	_, err = db.Exec(createSQL)
	return err
}

func normalizeSQL(sqlText string) string {
	return strings.ToLower(strings.Join(strings.Fields(sqlText), " "))
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
