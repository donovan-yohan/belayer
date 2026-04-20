package daemon

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// TestHandlePhase covers GET /sessions/{id}/phase.
func TestHandlePhase(t *testing.T) {
	// Register the route on the testDaemon fixture mux.
	// testDaemon does NOT register /phase — we add it here via a wrapper daemon.
	setup := func(t *testing.T) *Daemon {
		t.Helper()
		d := testDaemon(t)
		// Inject the phase route into the existing mux.
		d.server.Handler.(*http.ServeMux).HandleFunc("GET /sessions/{id}/phase", d.handlePhase)
		return d
	}

	t.Run("most_recent_phase_event_wins", func(t *testing.T) {
		d := setup(t)

		// Create a session.
		sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "phase-test"}))

		// Seed three events: two phase events at t0/t1, one non-phase at t2.
		t0 := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
		t1 := t0.Add(5 * time.Minute)
		t2 := t1.Add(5 * time.Minute)

		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sess.ID,
			Type:      "agent_status:planning",
			Data:      `{}`,
			Timestamp: t0,
		}); err != nil {
			t.Fatalf("LogEvent t0: %v", err)
		}
		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sess.ID,
			Type:      "agent_status:implementing",
			Data:      `{}`,
			Timestamp: t1,
		}); err != nil {
			t.Fatalf("LogEvent t1: %v", err)
		}
		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sess.ID,
			Type:      "bridge:tool_started",
			Data:      `{}`,
			Timestamp: t2,
		}); err != nil {
			t.Fatalf("LogEvent t2: %v", err)
		}

		rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/phase", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp phaseResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if resp.Phase != "implement" {
			t.Errorf("phase: got %q, want %q", resp.Phase, "implement")
		}
		if resp.Since == nil {
			t.Fatal("since: expected non-nil, got nil")
		}
		if !resp.Since.Equal(t1) {
			t.Errorf("since: got %v, want %v", *resp.Since, t1)
		}
	})

	t.Run("no_phase_events_returns_unknown", func(t *testing.T) {
		d := setup(t)

		sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "no-phase"}))

		// Seed only non-phase bridge events.
		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sess.ID,
			Type:      "bridge:tool_called",
			Data:      `{}`,
		}); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}
		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sess.ID,
			Type:      "bridge:tool_result",
			Data:      `{}`,
		}); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}

		rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/phase", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		// Decode into a raw map to verify "since" key is absent entirely.
		var raw map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
			t.Fatalf("decode raw response: %v", err)
		}

		phase, ok := raw["phase"]
		if !ok {
			t.Fatal("response missing \"phase\" key")
		}
		if phase != "unknown" {
			t.Errorf("phase: got %q, want %q", phase, "unknown")
		}
		if _, hasSince := raw["since"]; hasSince {
			t.Error("since: expected key to be omitted when no phase event, but it was present")
		}
	})

	t.Run("unknown_session_returns_404", func(t *testing.T) {
		d := setup(t)

		rr := doRequest(t, d, "GET", "/sessions/nonexistent-session-id/phase", nil)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
