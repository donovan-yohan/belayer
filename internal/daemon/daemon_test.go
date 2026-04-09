package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemon creates a Daemon backed by an in-memory store for use in tests.
func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	d := &Daemon{store: st, config: Config{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.handleHealth)
	mux.HandleFunc("POST /sessions", d.handleCreateSession)
	mux.HandleFunc("GET /sessions", d.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", d.handleGetSession)
	mux.HandleFunc("PATCH /sessions/{id}", d.handleUpdateSession)
	mux.HandleFunc("GET /sessions/{id}/events", d.handleGetEvents)
	mux.HandleFunc("POST /sessions/{id}/events", d.handleLogEvent)
	mux.HandleFunc("GET /search", d.handleSearch)
	d.server = &http.Server{Handler: mux}
	return d
}

func doRequest(t *testing.T, d *Daemon, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	d.server.Handler.ServeHTTP(rr, req)
	return rr
}

func decodeJSON[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rr.Body.String())
	}
	return v
}

func TestHealth(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "GET", "/health", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeJSON[map[string]string](t, rr)
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %s", resp["status"])
	}
}

func TestCreateSession(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "test-session",
		Template: "implement",
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[store.Session](t, rr)
	if sess.Name != "test-session" {
		t.Fatalf("expected name=test-session, got %s", sess.Name)
	}
	if sess.Template != "implement" {
		t.Fatalf("expected template=implement, got %s", sess.Template)
	}
	if sess.Status != "pending" {
		t.Fatalf("expected status=pending, got %s", sess.Status)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

func TestCreateSessionMissingName(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "POST", "/sessions", createSessionRequest{})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListSessions(t *testing.T) {
	d := testDaemon(t)

	// Create two sessions.
	doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "s1"})
	doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "s2"})

	rr := doRequest(t, d, "GET", "/sessions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	sessions := decodeJSON[[]store.Session](t, rr)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestGetSession(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "lookup"})
	created := decodeJSON[store.Session](t, createRR)

	// Retrieve it.
	rr := doRequest(t, d, "GET", "/sessions/"+created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[store.Session](t, rr)
	if sess.Name != "lookup" {
		t.Fatalf("expected name=lookup, got %s", sess.Name)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	d := testDaemon(t)
	rr := doRequest(t, d, "GET", "/sessions/nonexistent", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUpdateSessionStatus(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "update-me"})
	created := decodeJSON[store.Session](t, createRR)

	rr := doRequest(t, d, "PATCH", "/sessions/"+created.ID, updateSessionRequest{Status: "active"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[store.Session](t, rr)
	if sess.Status != "active" {
		t.Fatalf("expected status=active, got %s", sess.Status)
	}
}

func TestGetEvents(t *testing.T) {
	d := testDaemon(t)

	// Create session — this also logs a session_created event.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "events-test"})
	created := decodeJSON[store.Session](t, createRR)

	rr := doRequest(t, d, "GET", "/sessions/"+created.ID+"/events", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) == 0 {
		t.Fatal("expected at least one event from session creation")
	}
	if events[0].Type != "session_created" {
		t.Fatalf("expected first event type=session_created, got %s", events[0].Type)
	}
}

func TestLogEvent(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "log-test"})
	created := decodeJSON[store.Session](t, createRR)

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{
		Type: "custom_event",
		Data: `{"key":"value"}`,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify it's in the event list.
	eventsRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/events", nil)
	events := decodeJSON[[]store.SessionEvent](t, eventsRR)
	found := false
	for _, e := range events {
		if e.Type == "custom_event" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom_event not found in session events")
	}
}

func TestShutdown(t *testing.T) {
	d := testDaemon(t)
	// Shutdown should not panic on a daemon that never started listening.
	if err := d.Shutdown(context.Background()); err != nil {
		// server.Shutdown may return an error if Serve was never called — that's fine.
		_ = err
	}
}

func TestSearchEvents(t *testing.T) {
	d := testDaemon(t)

	// Create a session and log searchable events.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "search-test"})
	created := decodeJSON[store.Session](t, createRR)

	doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{
		Type: "node_started",
		Data: `{"node":"implementer"}`,
	})
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{
		Type: "node_completed",
		Data: `{"node":"reviewer"}`,
	})

	// Search for a term that matches one event's data.
	rr := doRequest(t, d, "GET", "/search?q=implementer", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) != 1 {
		t.Fatalf("expected 1 result, got %d", len(events))
	}
	if events[0].Type != "node_started" {
		t.Errorf("expected type=node_started, got %s", events[0].Type)
	}
}

func TestSearchEventsMissingQuery(t *testing.T) {
	d := testDaemon(t)

	rr := doRequest(t, d, "GET", "/search", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
