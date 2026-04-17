package daemon

// health_test.go covers eng-review test-plan items:
//   T19 — daemon_instance_id changes across daemon restart (two New() calls)

import (
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// TestHealth_T19_DaemonInstanceIDChangesAcrossRestart verifies that two
// separate Daemon instances (simulating a daemon restart) produce different
// daemon_instance_id values. This is the epoch-change signal cragd uses to
// detect that the daemon restarted and flush its SSE cursor.
//
// Test-plan item: T19 — daemon_instance_id changes across restart.
func TestHealth_T19_DaemonInstanceIDChangesAcrossRestart(t *testing.T) {
	// Create two fully independent daemon instances backed by their own stores,
	// simulating what happens when the daemon process restarts.
	newTestDaemonInstance := func() *Daemon {
		st, err := store.Open(filepath.Join(t.TempDir(), "belayer.db"))
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { st.Close() })
		d, err := New(Config{
			DBPath:     filepath.Join(t.TempDir(), "x.db"),
			SocketPath: filepath.Join(t.TempDir(), "d.sock"),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return d
	}

	d1 := newTestDaemonInstance()
	d2 := newTestDaemonInstance()

	id1 := d1.DaemonInstanceID()
	id2 := d2.DaemonInstanceID()

	if id1 == "" {
		t.Fatal("first daemon: daemon_instance_id is empty")
	}
	if id2 == "" {
		t.Fatal("second daemon: daemon_instance_id is empty")
	}
	if id1 == id2 {
		t.Errorf("daemon_instance_id identical across two New() calls: %q — cragd cannot detect restarts", id1)
	}
}
