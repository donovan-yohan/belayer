package store

import "database/sql"

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
			id           TEXT PRIMARY KEY,
			session_id   TEXT NOT NULL,
			name         TEXT NOT NULL,
			role         TEXT NOT NULL DEFAULT '',
			profile      TEXT NOT NULL DEFAULT '',
			repo_scope   TEXT NOT NULL DEFAULT '',
			workdir      TEXT NOT NULL DEFAULT '',
			transport    TEXT NOT NULL DEFAULT 'tmux',
			tmux_session TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'starting',
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL,
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
	return nil
}
