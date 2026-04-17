package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemon creates a Daemon backed by an in-memory store for use in tests.
func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "belayer.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	reg := &sandbox.Registry{}
	reg.Register(sandbox.DefaultMode, &sandbox.Noop{})
	d := &Daemon{
		store:            st,
		config:           Config{},
		tools:            make(map[string][]agent.ToolSpec),
		bridgeProcs:      make(map[string]*bridge.Process),
		sessionSandboxes: make(map[string]sessionSandbox),
		sandboxDrivers:   reg,
		runtime:          &runtime.Noop{},
		startCtx:         context.Background(),
		archiver:         newArchiveManager(st),
	}
	d.broker = broker.NewMemoryBroker(st)
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) { return nil, nil }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.handleHealth)
	mux.HandleFunc("POST /sessions", d.handleCreateSession)
	mux.HandleFunc("GET /sessions", d.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", d.handleGetSession)
	mux.HandleFunc("PATCH /sessions/{id}", d.handleUpdateSession)
	mux.HandleFunc("GET /sessions/{id}/events", d.handleGetEvents)
	mux.HandleFunc("POST /sessions/{id}/events", d.handleLogEvent)
	mux.HandleFunc("GET /events/stream", d.handleStreamEvents)
	mux.HandleFunc("POST /sessions/{id}/messages", d.handleSendMessage)
	mux.HandleFunc("POST /sessions/{id}/messages/broadcast", d.handleBroadcastMessage)
	mux.HandleFunc("GET /sessions/{id}/messages", d.handleListMessages)
	mux.HandleFunc("POST /sessions/{id}/agents", d.handleSpawnAgent)
	mux.HandleFunc("GET /sessions/{id}/agents", d.handleListAgents)
	mux.HandleFunc("POST /sessions/{id}/agents/{name}/finish", d.handleFinishAgent)
	mux.HandleFunc("POST /sessions/{id}/artifacts", d.handleCreateArtifact)
	mux.HandleFunc("GET /sessions/{id}/artifacts", d.handleListArtifacts)
	mux.HandleFunc("GET /search", d.handleSearch)
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
	sess := decodeJSON[sessionAPIResponse](t, rr)
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
	sessions := decodeJSON[[]sessionAPIResponse](t, rr)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestGetSession(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "lookup"})
	created := decodeJSON[sessionAPIResponse](t, createRR)

	// Retrieve it.
	rr := doRequest(t, d, "GET", "/sessions/"+created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)
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
	created := decodeJSON[sessionAPIResponse](t, createRR)

	rr := doRequest(t, d, "PATCH", "/sessions/"+created.ID, updateSessionRequest{Status: "active"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, rr)
	if sess.Status != "active" {
		t.Fatalf("expected status=active, got %s", sess.Status)
	}
}

func TestSpawnAgentAndListRoster(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "spawn-test"}))

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "nightshift-supervisor",
		Workdir: "/tmp/workdir",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run := decodeJSON[store.AgentRun](t, rr)
	if run.Name != "supervisor" || run.Profile != "nightshift-supervisor" {
		t.Fatalf("unexpected agent run: %#v", run)
	}
	if run.Status != "running" {
		t.Fatalf("expected running status, got %q", run.Status)
	}

	rosterRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/agents", nil)
	if rosterRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rosterRR.Code)
	}
	roster := decodeJSON[[]store.AgentRun](t, rosterRR)
	if len(roster) != 1 || roster[0].Name != "supervisor" {
		t.Fatalf("unexpected roster: %#v", roster)
	}
}

func TestSendMessageDeliversToSpawnedAgent(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "msg-test"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "api", Role: "api", Profile: "nightshift-api"})

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "api", Content: "hello api", Type: "instruction", From: "supervisor", Interrupt: true})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Bridge agents use pull-based delivery: messages are persisted in the store
	// and pulled via GET /sessions/{id}/messages?for=api&pending=true.
	msgsRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=api", nil)
	if msgsRR.Code != http.StatusOK {
		t.Fatalf("expected 200 listing messages, got %d: %s", msgsRR.Code, msgsRR.Body.String())
	}
	msgs := decodeJSON[[]store.Message](t, msgsRR)
	if len(msgs) == 0 || msgs[len(msgs)-1].Content != "hello api" {
		t.Fatalf("expected persisted message, got %#v", msgs)
	}
}

func TestFinishAgentMarksComplete(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "finish-test"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "api", Role: "api", Profile: "nightshift-api"})

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents/api/finish", finishAgentRequest{Summary: "done"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	run := decodeJSON[store.AgentRun](t, rr)
	if run.Status != "complete" {
		t.Fatalf("expected complete status, got %q", run.Status)
	}
}

func TestCreateAndListArtifacts(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "artifact-test"}))

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/artifacts", artifactCreateRequest{
		Kind:     "shared-contract",
		Path:     "artifacts/shared-contract.md",
		Producer: "api",
		Summary:  "Actual implemented API contract",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	artifact := decodeJSON[store.Artifact](t, rr)
	if artifact.Kind != "shared-contract" || artifact.Producer != "api" {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}

	listRR := doRequest(t, d, "GET", "/sessions/"+created.ID+"/artifacts", nil)
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRR.Code)
	}
	artifacts := decodeJSON[[]store.Artifact](t, listRR)
	if len(artifacts) != 1 || artifacts[0].Path != "artifacts/shared-contract.md" {
		t.Fatalf("unexpected artifacts: %#v", artifacts)
	}
}

func TestGetEvents(t *testing.T) {
	d := testDaemon(t)

	// Create session — this also logs a session_created event.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "events-test"})
	created := decodeJSON[sessionAPIResponse](t, createRR)

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
	created := decodeJSON[sessionAPIResponse](t, createRR)
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
	created := decodeJSON[sessionAPIResponse](t, createRR)
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

	sess1 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "watch-1"}))
	sess2 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "watch-2"}))
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
	created := decodeJSON[sessionAPIResponse](t, createRR)

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
	// Phase (a) contract: the draining flag must be set. Without this assertion,
	// a future refactor that moves d.draining.Store(true) below server.Shutdown
	// would silently pass CI and break the external SSE-consumer contract.
	if !d.draining.Load() {
		t.Fatal("Shutdown did not set draining flag")
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	d := testDaemon(t)
	if err := d.Shutdown(context.Background()); err != nil {
		_ = err
	}
	// Second Shutdown must be a no-op: no panic on double store.Close, no
	// duplicate server.Shutdown error propagation. Phase (a) guards via
	// d.draining.Swap — second call returns nil early.
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown returned error: %v", err)
	}
}

func TestSearchEvents(t *testing.T) {
	d := testDaemon(t)

	// Create a session and log searchable events.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "search-test"})
	created := decodeJSON[sessionAPIResponse](t, createRR)

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

// --- Pull-based message delivery (pending messages) ---

func TestListMessagesPendingReturnsPendingMessages(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "pending-msgs"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "worker", Role: "worker", Profile: "default"})

	// Send two messages to the worker agent.
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "worker", Content: "first task", Type: "instruction", From: "supervisor"})
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "worker", Content: "second task", Type: "instruction", From: "supervisor"})

	rr := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=worker&pending=true", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	msgs := decodeJSON[[]store.Message](t, rr)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 pending messages, got %d", len(msgs))
	}
	if msgs[0].Content != "first task" || msgs[1].Content != "second task" {
		t.Fatalf("unexpected message contents: %#v", msgs)
	}
}

func TestListMessagesPendingMarksDelivered(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "mark-delivered"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "worker", Role: "worker", Profile: "default"})

	doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "worker", Content: "do the thing", Type: "instruction", From: "supervisor"})

	// First fetch with pending=true — should return the message and mark it delivered.
	rr1 := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=worker&pending=true", nil)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first fetch: expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	msgs1 := decodeJSON[[]store.Message](t, rr1)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 message on first fetch, got %d", len(msgs1))
	}

	// Second fetch — message was marked delivered, should now return empty.
	rr2 := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=worker&pending=true", nil)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second fetch: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	msgs2 := decodeJSON[[]store.Message](t, rr2)
	if len(msgs2) != 0 {
		t.Fatalf("expected 0 messages on second fetch (all delivered), got %d", len(msgs2))
	}
}

func TestListMessagesPendingFalseDoesNotMarkDelivered(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "peek-msgs"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "worker", Role: "worker", Profile: "default"})

	doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "worker", Content: "peek task", Type: "instruction", From: "supervisor"})

	// Fetch without pending=true — should return the message but NOT mark it delivered.
	rr1 := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=worker", nil)
	if rr1.Code != http.StatusOK {
		t.Fatalf("peek fetch: expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	msgs1 := decodeJSON[[]store.Message](t, rr1)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 message on peek fetch, got %d", len(msgs1))
	}

	// Fetch again — message should still be pending since we did not mark delivered.
	rr2 := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages?for=worker", nil)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second peek: expected 200, got %d", rr2.Code)
	}
	msgs2 := decodeJSON[[]store.Message](t, rr2)
	if len(msgs2) != 1 {
		t.Fatalf("expected 1 message still pending, got %d", len(msgs2))
	}
}

func TestListMessagesWithoutForReturnsEventLog(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "event-log-msgs"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "worker", Role: "worker", Profile: "default"})

	doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "worker", Content: "hello", Type: "instruction", From: "supervisor"})

	// Without ?for= the original event-log behavior should be used.
	rr := doRequest(t, d, "GET", "/sessions/"+created.ID+"/messages", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) == 0 {
		t.Fatal("expected at least one message_ event in event log")
	}
	for _, e := range events {
		if !strings.HasPrefix(e.Type, "message_") {
			t.Errorf("unexpected non-message event type %q in message list", e.Type)
		}
	}
}

// capturingSandbox records the last sandbox.Config passed to Create so tests
// can assert the daemon populated it correctly.
type capturingSandbox struct {
	sandbox.Noop
	lastCreateConfig sandbox.Config
}

func (c *capturingSandbox) Create(ctx context.Context, cfg sandbox.Config) (sandbox.Handle, error) {
	c.lastCreateConfig = cfg
	return sandbox.Handle{ID: cfg.Name}, nil
}

func TestEnsureSandboxHandlePassesRuntimeEndpoints(t *testing.T) {
	d := testDaemon(t)
	fake := &capturingSandbox{}
	reg := &sandbox.Registry{}
	reg.Register(sandbox.DefaultMode, fake)
	d.sandboxDrivers = reg
	d.runtimeEndpoints = []runtime.Endpoint{
		{Name: "api", Host: "localhost", Port: 4000},
		{Name: "web", Host: "localhost", Port: 3000},
	}

	sess := store.Session{ID: "sess-1", WorkspaceDir: "/tmp/ws"}
	if _, err := d.ensureSandboxHandle(context.Background(), sess); err != nil {
		t.Fatalf("ensureSandboxHandle: %v", err)
	}

	want := []sandbox.TCPEndpoint{
		{Name: "api", Host: "localhost", Port: 4000},
		{Name: "web", Host: "localhost", Port: 3000},
	}
	if len(fake.lastCreateConfig.Endpoints) != len(want) {
		t.Fatalf("sandbox.Create got %d endpoints, want %d", len(fake.lastCreateConfig.Endpoints), len(want))
	}
	for i, got := range fake.lastCreateConfig.Endpoints {
		if got != want[i] {
			t.Errorf("endpoints[%d] = %+v, want %+v", i, got, want[i])
		}
	}
	if fake.lastCreateConfig.Workspace != "/tmp/ws" {
		t.Errorf("sandbox.Create workspace = %q, want /tmp/ws", fake.lastCreateConfig.Workspace)
	}
}

func TestEnsureSandboxHandleNilEndpointsWhenRuntimeReportsNone(t *testing.T) {
	d := testDaemon(t)
	fake := &capturingSandbox{}
	reg := &sandbox.Registry{}
	reg.Register(sandbox.DefaultMode, fake)
	d.sandboxDrivers = reg
	// d.runtimeEndpoints left nil — mirrors Noop provider's Up return.

	sess := store.Session{ID: "sess-1"}
	if _, err := d.ensureSandboxHandle(context.Background(), sess); err != nil {
		t.Fatalf("ensureSandboxHandle: %v", err)
	}
	if fake.lastCreateConfig.Endpoints != nil {
		t.Errorf("sandbox.Create endpoints = %v, want nil", fake.lastCreateConfig.Endpoints)
	}
}

// unavailableDriverStub fails every Driver call with a clamshell-style
// unavailable error, mimicking the default-build clamshell_stub for tests
// that need to assert the daemon routes errors back to callers without
// tearing itself down.
type unavailableDriverStub struct{ msg string }

func (u *unavailableDriverStub) Create(context.Context, sandbox.Config) (sandbox.Handle, error) {
	return sandbox.Handle{}, errStub(u.msg)
}
func (u *unavailableDriverStub) Exec(context.Context, sandbox.Handle, []string, sandbox.ExecOpts) (sandbox.Process, error) {
	return nil, errStub(u.msg)
}
func (u *unavailableDriverStub) Stop(context.Context, sandbox.Handle) error {
	return errStub(u.msg)
}

type errStub string

func (e errStub) Error() string { return string(e) }

// writeSandboxConfig writes .belayer/config.yaml under workspaceDir with the
// given sandbox.mode. Used by the resolution tests.
func writeSandboxConfig(t *testing.T, workspaceDir, mode string) {
	t.Helper()
	belayerDir := filepath.Join(workspaceDir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o700); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	contents := "sandbox:\n  mode: " + mode + "\n"
	if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

func TestEnsureSandboxHandleResolvesModeNoop(t *testing.T) {
	d := testDaemon(t)
	fake := &capturingSandbox{}
	reg := &sandbox.Registry{}
	reg.Register("noop", fake)
	reg.Register("clamshell", &unavailableDriverStub{msg: "clamshell not compiled in"})
	d.sandboxDrivers = reg

	ws := t.TempDir()
	writeSandboxConfig(t, ws, "noop")

	sess := store.Session{ID: "sess-noop", WorkspaceDir: ws}
	ss, err := d.ensureSandboxHandle(context.Background(), sess)
	if err != nil {
		t.Fatalf("ensureSandboxHandle noop: %v", err)
	}
	if ss.handle.ID != "sess-noop" {
		t.Errorf("handle ID = %q, want sess-noop", ss.handle.ID)
	}
	if fake.lastCreateConfig.Name != "sess-noop" {
		t.Errorf("noop driver was not invoked for sandbox.mode=noop (captured name %q)", fake.lastCreateConfig.Name)
	}
}

func TestEnsureSandboxHandleResolvesDefaultWhenConfigMissing(t *testing.T) {
	d := testDaemon(t)
	fake := &capturingSandbox{}
	reg := &sandbox.Registry{}
	reg.Register("noop", fake)
	d.sandboxDrivers = reg

	sess := store.Session{ID: "sess-default", WorkspaceDir: t.TempDir()} // no .belayer/config.yaml
	if _, err := d.ensureSandboxHandle(context.Background(), sess); err != nil {
		t.Fatalf("ensureSandboxHandle default: %v", err)
	}
	if fake.lastCreateConfig.Name != "sess-default" {
		t.Errorf("default-mode fallback did not route to noop (captured name %q)", fake.lastCreateConfig.Name)
	}
}

func TestEnsureSandboxHandleSurfacesUnavailableDriver(t *testing.T) {
	d := testDaemon(t)
	reg := &sandbox.Registry{}
	reg.Register("noop", &sandbox.Noop{})
	reg.Register("clamshell", &unavailableDriverStub{msg: `sandbox driver "clamshell" is unavailable: this binary was built without -tags clamshell`})
	d.sandboxDrivers = reg

	// Session A wants clamshell; must fail with the unavailable message.
	wsA := t.TempDir()
	writeSandboxConfig(t, wsA, "clamshell")
	sessA := store.Session{ID: "sess-unavailable", WorkspaceDir: wsA}
	if _, err := d.ensureSandboxHandle(context.Background(), sessA); err == nil {
		t.Fatal("ensureSandboxHandle clamshell: expected error, got nil")
	} else if !strings.Contains(err.Error(), "unavailable") || !strings.Contains(err.Error(), "-tags clamshell") {
		t.Errorf("error %q missing unavailable / -tags clamshell text", err.Error())
	}

	// Session B keeps working — one bad session must not poison the daemon.
	sessB := store.Session{ID: "sess-healthy", WorkspaceDir: t.TempDir()}
	if _, err := d.ensureSandboxHandle(context.Background(), sessB); err != nil {
		t.Fatalf("ensureSandboxHandle healthy session after clamshell failure: %v", err)
	}
}

func TestEnsureSandboxHandleUnknownModeReturnsError(t *testing.T) {
	d := testDaemon(t)
	reg := &sandbox.Registry{}
	reg.Register("noop", &sandbox.Noop{})
	d.sandboxDrivers = reg

	ws := t.TempDir()
	writeSandboxConfig(t, ws, "does-not-exist")
	sess := store.Session{ID: "sess-unknown", WorkspaceDir: ws}
	_, err := d.ensureSandboxHandle(context.Background(), sess)
	if err == nil {
		t.Fatal("expected not-registered error, got nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error %q does not mention \"not registered\"", err.Error())
	}
}

// fakeRuntime returns a preset endpoint list from Up and signals Up/Down via
// channels so tests can establish happens-before without races. sync.Once
// guards the closes so callers can invoke Up/Down more than once without
// panicking — useful if a future Start path retries provisioning.
type fakeRuntime struct {
	endpoints []runtime.Endpoint
	upOnce    sync.Once
	upDone    chan struct{}
	downOnce  sync.Once
	downDone  chan struct{}
}

func newFakeRuntime(eps []runtime.Endpoint) *fakeRuntime {
	return &fakeRuntime{
		endpoints: eps,
		upDone:    make(chan struct{}),
		downDone:  make(chan struct{}),
	}
}

func (f *fakeRuntime) Up(ctx context.Context) ([]runtime.Endpoint, error) {
	f.upOnce.Do(func() { close(f.upDone) })
	return f.endpoints, nil
}
func (f *fakeRuntime) Health(ctx context.Context) error { return nil }
func (f *fakeRuntime) Down(ctx context.Context) error {
	f.downOnce.Do(func() { close(f.downDone) })
	return nil
}

func TestStartCapturesRuntimeEndpoints(t *testing.T) {
	// Start binds a Unix socket; Darwin limits sun_path to 104 bytes so we use
	// a short /tmp path rather than t.TempDir().
	socketDir, err := os.MkdirTemp("/tmp", "bl")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "d.sock")
	dbPath := filepath.Join(t.TempDir(), "belayer.db")

	fake := newFakeRuntime([]runtime.Endpoint{
		{Name: "api", Host: "localhost", Port: 4000},
	})
	d, err := New(Config{SocketPath: socketPath, DBPath: dbPath, Runtime: fake})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- d.Start(ctx) }()

	// Wait until the daemon is accepting connections so we know Start is past
	// runtime.Up and into Serve. Drain serveErr to surface any fast-fail.
	ready := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-serveErr:
			t.Fatalf("Start returned early: %v", err)
		default:
		}
		c, dialErr := net.Dial("unix", socketPath)
		if dialErr == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("daemon never became reachable on its Unix socket")
	}

	select {
	case <-fake.upDone:
	case <-time.After(time.Second):
		t.Fatal("runtime.Up was not called by Start")
	}

	cancel()
	select {
	case <-serveErr:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	// After <-serveErr, the Start goroutine has returned, giving happens-before
	// for the endpoints written during runtime.Up.
	if len(d.runtimeEndpoints) != 1 || d.runtimeEndpoints[0].Port != 4000 {
		t.Errorf("runtimeEndpoints = %+v, want single endpoint on :4000", d.runtimeEndpoints)
	}
	select {
	case <-fake.downDone:
	case <-time.After(2 * time.Second):
		t.Error("runtime.Down was not called during shutdown")
	}
}

