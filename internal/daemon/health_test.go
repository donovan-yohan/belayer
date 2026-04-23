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

// TestHealth_AdvertisesFullCapabilityManifest verifies that every shipped
// feature is declared in /health.capabilities — this is the authoritative
// manifest that dashboards use for negotiation. Drift between this block
// and what the daemon actually serves means consumers get surprises in
// production.
//
// Addresses codex review concern (phases 4-8): prior manifest was missing
// sse_filters, cursor_reader_id, compact_tsv, aggregates, transcripts,
// traces, artifacts_bytes, link_next, and session_digest.
func TestHealth_AdvertisesFullCapabilityManifest(t *testing.T) {
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
			SearchPredicates []string `json:"search_predicates"`
			ArchiveHTTP      bool     `json:"archive_http"`
			SSEControlFrames []string `json:"sse_control_frames"`
			SSEFilters       []string `json:"sse_filters"`
			CursorReaderID   bool     `json:"cursor_reader_id"`
			CompactTSV       bool     `json:"compact_tsv"`
			Aggregates       bool     `json:"aggregates"`
			Transcripts      bool     `json:"transcripts"`
			Traces           bool     `json:"traces"`
			ArtifactsBytes   bool     `json:"artifacts_bytes"`
			LinkNext         bool     `json:"link_next"`
			LogLevels        []string `json:"log_levels"`
			WebUI            bool     `json:"web_ui"`
		} `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	c := body.Capabilities

	// Boolean feature flags.
	for _, tc := range []struct {
		name string
		got  bool
	}{
		{"archive_http", c.ArchiveHTTP},
		{"cursor_reader_id", c.CursorReaderID},
		{"compact_tsv", c.CompactTSV},
		{"aggregates", c.Aggregates},
		{"transcripts", c.Transcripts},
		{"traces", c.Traces},
		{"artifacts_bytes", c.ArtifactsBytes},
		{"link_next", c.LinkNext},
		{"web_ui", c.WebUI},
	} {
		if !tc.got {
			t.Errorf("capability %q: expected true, got false", tc.name)
		}
	}

	// List membership (order is not contractual).
	hasAll := func(got, want []string) bool {
		set := make(map[string]struct{}, len(got))
		for _, s := range got {
			set[s] = struct{}{}
		}
		for _, w := range want {
			if _, ok := set[w]; !ok {
				return false
			}
		}
		return true
	}
	if !hasAll(c.SSEControlFrames, []string{"daemon_hello", "daemon_draining", "session_digest"}) {
		t.Errorf("sse_control_frames missing entries: got %v", c.SSEControlFrames)
	}
	if !hasAll(c.SSEFilters, []string{"agent", "type_prefix", "tier", "digest"}) {
		t.Errorf("sse_filters missing entries: got %v", c.SSEFilters)
	}
	if !hasAll(c.LogLevels, []string{"standard", "verbose", "trace"}) {
		t.Errorf("log_levels missing entries: got %v", c.LogLevels)
	}
	if !hasAll(c.SearchPredicates, []string{"q", "session", "type_prefix", "agent", "after", "before"}) {
		t.Errorf("search_predicates missing entries: got %v", c.SearchPredicates)
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
