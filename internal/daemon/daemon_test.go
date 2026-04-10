package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/docker"
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

	d := &Daemon{store: st, config: Config{}, tools: make(map[string][]agent.ToolSpec)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.handleHealth)
	mux.HandleFunc("POST /sessions", d.handleCreateSession)
	mux.HandleFunc("GET /sessions", d.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", d.handleGetSession)
	mux.HandleFunc("PATCH /sessions/{id}", d.handleUpdateSession)
	mux.HandleFunc("GET /sessions/{id}/events", d.handleGetEvents)
	mux.HandleFunc("POST /sessions/{id}/events", d.handleLogEvent)
	mux.HandleFunc("GET /events/stream", d.handleStreamEvents)
	mux.HandleFunc("GET /search", d.handleSearch)
	mux.HandleFunc("POST /sessions/{id}/workbench", d.handleCreateWorkbench)
	mux.HandleFunc("GET /sessions/{id}/workbench", d.handleGetWorkbench)
	mux.HandleFunc("DELETE /sessions/{id}/workbench", d.handleDeleteWorkbench)
	mux.HandleFunc("POST /sessions/{id}/tools", d.handleRegisterTool)
	mux.HandleFunc("GET /sessions/{id}/tools", d.handleListTools)
	mux.HandleFunc("POST /sessions/{id}/tools/{name}", d.handleExecuteTool)
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

func TestGetEventsAfter(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "events-after"})
	created := decodeJSON[store.Session](t, createRR)
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{Type: "first"})

	allEventsRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/events", nil)
	allEvents := decodeJSON[[]store.SessionEvent](t, allEventsRR)
	lastID := allEvents[len(allEvents)-1].ID

	doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{Type: "second"})
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{Type: "third"})

	rr := doRequest(t, d, "GET", "/sessions/"+created.ID+"/events?after="+fmt.Sprint(lastID), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "second" || events[1].Type != "third" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestGetEventsWait_LongPollsUntilEventArrives(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "events-wait"})
	created := decodeJSON[store.Session](t, createRR)
	initialEventsRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/events", nil)
	initialEvents := decodeJSON[[]store.SessionEvent](t, initialEventsRR)
	lastID := initialEvents[len(initialEvents)-1].ID

	go func() {
		time.Sleep(150 * time.Millisecond)
		doRequest(t, d, "POST", "/sessions/"+created.ID+"/events", logEventRequest{Type: "delayed"})
	}()

	req := httptest.NewRequest("GET", "/sessions/"+created.ID+"/events?after="+fmt.Sprint(lastID)+"&wait=1s", nil)
	rr := httptest.NewRecorder()
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) != 1 || events[0].Type != "delayed" {
		t.Fatalf("unexpected long-poll events: %#v", events)
	}
}

func TestStreamEventsSSE_EmitsMultiplexedEvents(t *testing.T) {
	d := testDaemon(t)
	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	sess1 := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "watch-1"}))
	sess2 := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "watch-2"}))
	initial1 := decodeJSON[[]store.SessionEvent](t, doRequest(t, d, "GET", "/sessions/"+sess1.ID+"/events", nil))
	initial2 := decodeJSON[[]store.SessionEvent](t, doRequest(t, d, "GET", "/sessions/"+sess2.ID+"/events", nil))
	afterID := initial1[len(initial1)-1].ID
	if last := initial2[len(initial2)-1].ID; last > afterID {
		afterID = last
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan string, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/events/stream?sessions=%s,%s&after=%d&wait=1s", server.URL, sess1.ID, sess2.ID, afterID), nil)
		if err != nil {
			resultCh <- "request-error:" + err.Error()
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resultCh <- "do-error:" + err.Error()
			return
		}
		defer resp.Body.Close()

		buf := make([]byte, 2048)
		n, _ := resp.Body.Read(buf)
		resultCh <- string(buf[:n])
	}()

	time.Sleep(150 * time.Millisecond)
	doRequest(t, d, "POST", "/sessions/"+sess2.ID+"/events", logEventRequest{Type: "streamed"})

	select {
	case payload := <-resultCh:
		if !strings.Contains(payload, "event: session_event") {
			t.Fatalf("expected SSE event header, got %q", payload)
		}
		if !strings.Contains(payload, `"Type":"streamed"`) {
			t.Fatalf("expected streamed event payload, got %q", payload)
		}
		if !strings.Contains(payload, sess2.ID) {
			t.Fatalf("expected session ID %s in payload, got %q", sess2.ID, payload)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for SSE payload")
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

func installFakeDocker(t *testing.T, statusesJSON string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := fmt.Sprintf(`#!/bin/sh
set -eu
printf "%%s\n" "$@" >> "%s"
if [ "$1" = "compose" ] && [ "$4" = "up" ]; then
  exit 0
fi
if [ "$1" = "compose" ] && [ "$4" = "ps" ]; then
  printf '%%s' '%s'
  exit 0
fi
if [ "$1" = "compose" ] && [ "$4" = "down" ]; then
  exit 0
fi
exit 0
`, filepath.Join(dir, "docker.log"), statusesJSON)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCreateWorkbench_ProvisionsAndWaitsForHealthy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeDocker(t, `[{"name":"extend-api","state":"running","health":"healthy"}]`)

	d := testDaemon(t)
	sessID := createTestSession(t, d)

	sandboxPath, err := sandboxDir(sessID)
	if err != nil {
		t.Fatalf("sandboxDir: %v", err)
	}
	if err := os.MkdirAll(sandboxPath, 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandboxPath, "docker-compose.yml"), []byte("version: '3.9'"), 0o600); err != nil {
		t.Fatalf("write sandbox compose: %v", err)
	}
	err = docker.WriteRuntimeMetadata(sandboxPath, docker.RuntimeMetadata{
		SessionID: sessID,
		Workbench: &docker.WorkbenchConfigSpec{
			Timeout: "1s",
			Services: []docker.ServiceDecl{
				{Name: "extend-api", Image: "example/api:latest", Ports: []string{"8080"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteRuntimeMetadata: %v", err)
	}

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/workbench", createWorkbenchRequest{})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	wb := decodeJSON[store.WorkbenchState](t, rr)
	if wb.Status != "ready" {
		t.Fatalf("expected ready status, got %q", wb.Status)
	}
	if !strings.Contains(wb.Endpoints, "extend-api") || !strings.Contains(wb.Endpoints, "http://extend-api:8080") {
		t.Fatalf("unexpected endpoints: %s", wb.Endpoints)
	}
}

func TestDeleteWorkbench_TearsDownComposeArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeDocker(t, `[]`)

	d := testDaemon(t)
	sessID := createTestSession(t, d)

	workbenchDir := filepath.Join(home, ".belayer", "workbenches", sessID)
	if err := os.MkdirAll(workbenchDir, 0o700); err != nil {
		t.Fatalf("mkdir workbench dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workbenchDir, "docker-compose.yml"), []byte("version: '3.9'"), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	if _, err := d.store.CreateWorkbench(store.WorkbenchState{
		SessionID: sessID,
		Status:    "ready",
		Spec:      "{}",
		Endpoints: "{}",
	}); err != nil {
		t.Fatalf("CreateWorkbench: %v", err)
	}

	rr := doRequest(t, d, "DELETE", "/sessions/"+sessID+"/workbench", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if _, err := os.Stat(workbenchDir); !os.IsNotExist(err) {
		t.Fatalf("expected workbench dir to be removed, stat err=%v", err)
	}
	if _, err := d.store.GetWorkbenchBySession(sessID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected workbench row to be deleted, got err=%v", err)
	}
}
