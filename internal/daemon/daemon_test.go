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
	"sync"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/docker"
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

	d := &Daemon{
		store:             st,
		config:            Config{},
		tools:             make(map[string][]agent.ToolSpec),
		idlePollInterval:  15 * time.Second,
		idleTimeout:       2 * time.Minute,
		idleNudgeCooldown: 1 * time.Minute,
		now:               time.Now,
	}
	d.broker = broker.NewMemoryBroker(st)
	d.launchAgent = func(req agentSpawnRequest) (string, error) { return req.Name + "-tmux", nil }
	d.deliverMessage = func(_ store.AgentRun, _ broker.Message) error { return nil }
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

type immediateRunner struct{}

func (immediateRunner) CreateSession(name, cmd string) error { return nil }
func (immediateRunner) SendKeys(session, keys string, bracketed bool) error { return nil }
func (immediateRunner) SendEnter(session string) error { return nil }
func (immediateRunner) CapturePane(session string) (string, error) { return "", nil }
func (immediateRunner) KillSession(session string) error { return nil }
func (immediateRunner) WaitForSession(session string, timeout time.Duration) error { return nil }
func (immediateRunner) ListSessions() ([]string, error) { return nil, nil }

type paneSequenceRunner struct {
	mu    sync.Mutex
	panes []string
	idx   int
}

func (r *paneSequenceRunner) CreateSession(name, cmd string) error { return nil }
func (r *paneSequenceRunner) SendKeys(session, keys string, bracketed bool) error { return nil }
func (r *paneSequenceRunner) SendEnter(session string) error { return nil }
func (r *paneSequenceRunner) KillSession(session string) error { return nil }
func (r *paneSequenceRunner) WaitForSession(session string, timeout time.Duration) error {
	return fmt.Errorf("still running")
}
func (r *paneSequenceRunner) ListSessions() ([]string, error) { return nil, nil }
func (r *paneSequenceRunner) CapturePane(session string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.panes) == 0 {
		return "", nil
	}
	if r.idx >= len(r.panes) {
		return r.panes[len(r.panes)-1], nil
	}
	pane := r.panes[r.idx]
	r.idx++
	return pane, nil
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

func TestSpawnAgentAndListRoster(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "spawn-test"}))

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{
		Name:    "planner",
		Role:    "planner",
		Profile: "nightshift-planner",
		Workdir: "/tmp/workdir",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	run := decodeJSON[store.AgentRun](t, rr)
	if run.Name != "planner" || run.Profile != "nightshift-planner" {
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
	if len(roster) != 1 || roster[0].Name != "planner" {
		t.Fatalf("unexpected roster: %#v", roster)
	}
}

func TestSendMessageDeliversToSpawnedAgent(t *testing.T) {
	d := testDaemon(t)
	var delivered []string
	d.deliverMessage = func(_ store.AgentRun, msg broker.Message) error {
		delivered = append(delivered, msg.Content)
		return nil
	}
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "msg-test"}))
	doRequest(t, d, "POST", "/sessions/"+created.ID+"/agents", agentSpawnRequest{Name: "api", Role: "api", Profile: "nightshift-api"})

	rr := doRequest(t, d, "POST", "/sessions/"+created.ID+"/messages", sendMessageRequest{To: "api", Content: "hello api", Type: "instruction", From: "planner", Interrupt: true})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(delivered) == 0 || delivered[len(delivered)-1] != "hello api" {
		t.Fatalf("expected delivered message, got %#v", delivered)
	}
}

func TestFinishAgentMarksComplete(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "finish-test"}))
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
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "artifact-test"}))

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

func TestWatchAgentExitMarksBlockedWithoutFinish(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "exit-watch-test"}))
	d.runner = immediateRunner{}
	_, err := d.store.CreateAgentRun(store.AgentRun{SessionID: created.ID, Name: "api", Role: "api", Profile: "default", Workdir: t.TempDir(), TmuxSession: "exit-watch", Status: "running", Transport: "tmux"})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	run, err := d.store.GetAgentRun(created.ID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	d.watchAgentExit(run)
	time.Sleep(100 * time.Millisecond)
	updated, err := d.store.GetAgentRun(created.ID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun updated: %v", err)
	}
	if updated.Status != "blocked" {
		t.Fatalf("expected blocked status, got %q", updated.Status)
	}
}

func TestWatchAgentIdleNudgesIdleAgent(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "idle-watch-test"}))
	d.runner = &paneSequenceRunner{panes: []string{"still thinking", "still thinking", "still thinking", "still thinking"}}
	d.idlePollInterval = 10 * time.Millisecond
	d.idleTimeout = 20 * time.Millisecond
	d.idleNudgeCooldown = time.Hour

	delivered := make(chan broker.Message, 4)
	d.deliverMessage = func(_ store.AgentRun, msg broker.Message) error {
		delivered <- msg
		_ = d.store.UpdateAgentRunStatus(created.ID, "api", "complete")
		return nil
	}

	_, err := d.store.CreateAgentRun(store.AgentRun{SessionID: created.ID, Name: "api", Role: "api", Profile: "default", Workdir: t.TempDir(), TmuxSession: "idle-watch", Status: "running", Transport: "tmux"})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	run, err := d.store.GetAgentRun(created.ID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}

	d.watchAgentIdle(run)

	select {
	case msg := <-delivered:
		if msg.RecipientID != "api" {
			t.Fatalf("expected recipient api, got %q", msg.RecipientID)
		}
		if !strings.Contains(msg.Content, "session appears idle") {
			t.Fatalf("expected idle nudge message, got %q", msg.Content)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected idle nudge to be delivered")
	}

	events, err := d.store.QueryEvents(created.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == "agent_idle_nudged" && strings.Contains(event.Data, "api") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected agent_idle_nudged event, got %#v", events)
	}
}

func TestWatchAgentIdleDoesNotNudgeWhenPaneKeepsChanging(t *testing.T) {
	d := testDaemon(t)
	created := decodeJSON[store.Session](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "idle-active-test"}))
	d.runner = &paneSequenceRunner{panes: []string{"thinking 1", "thinking 2", "thinking 3", "thinking 4", "thinking 5", "thinking 6", "thinking 7", "thinking 8"}}
	d.idlePollInterval = 10 * time.Millisecond
	d.idleTimeout = 25 * time.Millisecond
	d.idleNudgeCooldown = time.Hour

	delivered := make(chan broker.Message, 1)
	d.deliverMessage = func(_ store.AgentRun, msg broker.Message) error {
		delivered <- msg
		return nil
	}

	_, err := d.store.CreateAgentRun(store.AgentRun{SessionID: created.ID, Name: "api", Role: "api", Profile: "default", Workdir: t.TempDir(), TmuxSession: "idle-active", Status: "running", Transport: "tmux"})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	run, err := d.store.GetAgentRun(created.ID, "api")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}

	d.watchAgentIdle(run)
	time.Sleep(80 * time.Millisecond)
	_ = d.store.UpdateAgentRunStatus(created.ID, "api", "complete")

	select {
	case msg := <-delivered:
		t.Fatalf("did not expect idle nudge, got %#v", msg)
	default:
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
	wb := decodeJSON[workbenchResponse](t, rr)
	if wb.Status != "ready" {
		t.Fatalf("expected ready status, got %q", wb.Status)
	}
	if got := wb.Endpoints["extend-api"]; got != "http://extend-api:8080" {
		t.Fatalf("unexpected endpoints: %#v", wb.Endpoints)
	}
	if len(wb.Services) != 1 {
		t.Fatalf("expected one service status, got %#v", wb.Services)
	}
	if wb.Services[0].Name != "extend-api" || wb.Services[0].State != "running" || wb.Services[0].Health != "healthy" {
		t.Fatalf("unexpected service status: %#v", wb.Services[0])
	}
}

func TestGetWorkbench_ReturnsStructuredStatusAndServices(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeDocker(t, `[{"name":"extend-api","state":"running","health":"healthy"}]`)

	d := testDaemon(t)
	sessID := createTestSession(t, d)
	if _, err := d.store.CreateWorkbench(store.WorkbenchState{
		SessionID: sessID,
		Status:    "ready",
		Spec:      `{"services":[{"name":"extend-api","image":"example/api:latest","ports":["8080"]}]}`,
		Endpoints: `{"extend-api":{"service":"extend-api","url":"http://extend-api:8080"}}`,
	}); err != nil {
		t.Fatalf("CreateWorkbench: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/workbench", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	wb := decodeJSON[workbenchResponse](t, rr)
	if got := wb.Endpoints["extend-api"]; got != "http://extend-api:8080" {
		t.Fatalf("unexpected endpoints: %#v", wb.Endpoints)
	}
	if len(wb.Services) != 1 || wb.Services[0].Health != "healthy" {
		t.Fatalf("unexpected services: %#v", wb.Services)
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
