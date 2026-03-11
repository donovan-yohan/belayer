package testutil

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
)

// SetupTestDB creates a temp-file SQLite database, runs all migrations,
// inserts a default test crag (id and name: "test-instance", path: the temp dir),
// and registers cleanup via t.Cleanup. It returns the raw *sql.DB connection.
//
// A temp file is used instead of :memory: because in-memory SQLite gives each
// connection its own empty database, which breaks goroutine-based tests where
// multiple components share the same DB.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Migrate(); err != nil {
		t.Fatalf("migrating test db: %v", err)
	}

	now := time.Now().UTC()
	_, err = database.Conn().Exec(
		`INSERT INTO crags (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"test-instance", "test-instance", tmpDir, now, now,
	)
	if err != nil {
		t.Fatalf("inserting test crag: %v", err)
	}

	return database.Conn()
}
