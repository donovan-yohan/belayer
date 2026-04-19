package daemon

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestResponseHeaders_OnGetEvents verifies that all 6 standard Belayer response
// headers are present and have expected values after seeding a session with events.
func TestResponseHeaders_OnGetEvents(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "hdr-test",
		LogLevel: "standard",
	})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, body=%s", createRR.Code, createRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Spawn an agent so X-Agent-Roster is non-empty.
	spawnRR := doRequest(t, d, "POST", "/sessions/"+id+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "default",
	})
	if spawnRR.Code != http.StatusCreated {
		t.Fatalf("spawn agent: got %d, body=%s", spawnRR.Code, spawnRR.Body.String())
	}

	// Post 3 events.
	for i := 0; i < 3; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "test:event",
			Data: `{"agent":"supervisor","msg":"hello"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("log event %d: got %d, body=%s", i, rr.Code, rr.Body.String())
		}
	}

	// GET /events.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Assert all 6 headers present.
	headers := rr.Header()

	// X-Belayer-Schema
	schema := headers.Get("X-Belayer-Schema")
	if schema != "belayer-log/v1" {
		t.Errorf("X-Belayer-Schema: expected 'belayer-log/v1', got %q", schema)
	}

	// X-Last-Event-Id: must be a non-negative integer.
	lastEventID := headers.Get("X-Last-Event-Id")
	if lastEventID == "" {
		t.Error("X-Last-Event-Id header missing")
	} else {
		v, err := strconv.ParseInt(lastEventID, 10, 64)
		if err != nil || v < 0 {
			t.Errorf("X-Last-Event-Id: expected non-negative integer, got %q", lastEventID)
		}
	}

	// X-Event-Count: must be 5 — 1 session_created + 1 agent_spawned + 3 test events.
	eventCount := headers.Get("X-Event-Count")
	if eventCount != "5" {
		t.Errorf("X-Event-Count: expected '5' (1 session_created + 1 agent_spawned + 3 test events), got %q", eventCount)
	}

	// X-Session-Status: must be non-empty.
	sessionStatus := headers.Get("X-Session-Status")
	if sessionStatus == "" {
		t.Error("X-Session-Status header missing or empty")
	}

	// X-Log-Level: must be 'standard'.
	logLevel := headers.Get("X-Log-Level")
	if logLevel != "standard" {
		t.Errorf("X-Log-Level: expected 'standard', got %q", logLevel)
	}

	// X-Agent-Roster: must contain 'supervisor'.
	roster := headers.Get("X-Agent-Roster")
	if !strings.Contains(roster, "supervisor") {
		t.Errorf("X-Agent-Roster: expected 'supervisor' in %q", roster)
	}
}

// TestResponseHeaders_UnknownSession verifies behavior on an unknown session.
// handleGetEvents returns 200 with empty events array. Schema header must still be set;
// session-specific headers (X-Session-Status, X-Log-Level, X-Agent-Roster) may be absent.
func TestResponseHeaders_UnknownSession(t *testing.T) {
	d := testDaemon(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/does-not-exist/events", nil)
	d.server.Handler.ServeHTTP(rr, req)

	// Current handleGetEvents returns 200 with empty events for unknown sessions
	// (QueryEventsWindow returns empty, not 404).
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	headers := rr.Header()

	// X-Belayer-Schema must always be present.
	if headers.Get("X-Belayer-Schema") != "belayer-log/v1" {
		t.Errorf("X-Belayer-Schema: expected 'belayer-log/v1', got %q", headers.Get("X-Belayer-Schema"))
	}

	// X-Event-Count must be 0 (no events).
	if headers.Get("X-Event-Count") != "0" {
		t.Errorf("X-Event-Count: expected '0', got %q", headers.Get("X-Event-Count"))
	}

	// X-Session-Status and X-Log-Level may be absent for unknown session — no assertion.
}
