package daemon

// health_test.go covers eng-review test-plan items:
//   T19 — daemon_instance_id changes across daemon restart (two New() calls)
//   T1.4 — /health capabilities advertise supported log_levels

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// TestHealth_AdvertisesLogLevels verifies that GET /health capabilities block
// includes a "log_levels" field listing ["standard","verbose","trace"].
//
// Test-plan item: T1.4 — /health capabilities advertise supported log levels.
func TestHealth_AdvertisesLogLevels(t *testing.T) {
	d := testDaemon(t)
	srv := httptest.NewServer(d.server.Handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Capabilities struct {
			LogLevels []string `json:"log_levels"`
		} `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	want := []string{"standard", "verbose", "trace"}
	if len(body.Capabilities.LogLevels) != len(want) {
		t.Fatalf("log_levels: got %v, want %v", body.Capabilities.LogLevels, want)
	}
	for i, v := range want {
		if body.Capabilities.LogLevels[i] != v {
			t.Fatalf("log_levels[%d]: got %q, want %q", i, body.Capabilities.LogLevels[i], v)
		}
	}
}

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
