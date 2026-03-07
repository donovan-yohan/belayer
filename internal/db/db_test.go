package db

import (
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer d.Close()

	if d.Conn() == nil {
		t.Fatal("Conn() returned nil")
	}
}

func TestMigrate(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	if err := d.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify tables exist
	tables := []string{"instances", "tasks", "task_repos", "leads", "events", "agentic_decisions"}
	for _, table := range tables {
		var name string
		err := d.Conn().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	if err := d.Migrate(); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}

	if err := d.Migrate(); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	// Verify migration version recorded once
	var count int
	err = d.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("counting migrations: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 migration record, got %d", count)
	}

	// Running again should not add more records
	var countAfter int
	err = d.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&countAfter)
	if err != nil {
		t.Fatalf("counting migrations after second run: %v", err)
	}
	if countAfter != count {
		t.Errorf("migration count changed from %d to %d after idempotent run", count, countAfter)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	var fk int
	err = d.Conn().QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil {
		t.Fatalf("querying foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fk)
	}
}
