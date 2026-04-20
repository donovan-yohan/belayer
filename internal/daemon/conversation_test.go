package daemon

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
	"path/filepath"
)

// testConversationDaemon creates a Daemon wired with the conversation handler.
// All standard routes plus GET /sessions/{id}/conversation are registered.
func testConversationDaemon(t *testing.T) *Daemon {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "belayer.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	reg := &sandbox.Registry{}
	reg.Register(sandbox.DefaultMode, &sandbox.Noop{})
	d := &Daemon{
		store:                      st,
		config:                     Config{},
		daemonInstanceID:           "test-daemon-instance",
		tools:                      make(map[string][]agent.ToolSpec),
		bridgeProcs:                make(map[string]*bridge.Process),
		bridgeShuttingDownSessions: make(map[string]bool),
		sessionSandboxes:           make(map[string]sessionSandbox),
		sseSubscribers:             make(map[*sseSubscriber]struct{}),
		sandboxDrivers:             reg,
		runtime:                    &runtime.Noop{},
		startCtx:                   context.Background(),
		archiver:                   newArchiveManager(st, "test-instance"),
		archiveDrainTimeout:        30 * time.Second,
		shutdownHTTPTimeout:        5 * time.Second,
		sseKeepaliveInterval:       15 * time.Second,
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
	mux.HandleFunc("GET /sessions/{id}/bridges", d.handleListBridges)
	mux.HandleFunc("GET /sessions/{id}/bridges/{agent}/stdout", d.handleBridgeStdout)
	mux.HandleFunc("POST /sessions/{id}/artifacts", d.handleCreateArtifact)
	mux.HandleFunc("GET /sessions/{id}/artifacts", d.handleListArtifacts)
	mux.HandleFunc("GET /sessions/{id}/archive.ndjson", d.handleArchiveNDJSON)
	mux.HandleFunc("GET /sessions/{id}/archive/manifest.json", d.handleArchiveManifest)
	mux.HandleFunc("GET /sessions/{id}/archive.tar.gz", d.handleArchiveTarGz)
	mux.HandleFunc("GET /search", d.handleSearch)
	mux.HandleFunc("POST /sessions/{id}/tools", d.handleRegisterTool)
	mux.HandleFunc("GET /sessions/{id}/tools", d.handleListTools)
	mux.HandleFunc("POST /sessions/{id}/tools/{name}", d.handleExecuteTool)
	mux.HandleFunc("GET /sessions/{id}/conversation", d.handleConversation)
	d.server = &http.Server{Handler: mux}

	t.Cleanup(func() {
		if d.archiver != nil {
			d.archiver.inflight.Wait()
		}
		st.Close()
	})
	return d
}

// seedConversationMessages seeds 4 messages for the given session:
// supervisor→dev, dev→supervisor, supervisor→qa, qa→dev.
// Returns the session ID.
func seedConversationMessages(t *testing.T, d *Daemon, sessID string) {
	t.Helper()
	base := time.Now().UTC()
	msgs := []store.Message{
		{SessionID: sessID, SenderID: "supervisor", RecipientID: "dev", Content: "msg1", CreatedAt: base},
		{SessionID: sessID, SenderID: "dev", RecipientID: "supervisor", Content: "msg2", CreatedAt: base.Add(time.Second)},
		{SessionID: sessID, SenderID: "supervisor", RecipientID: "qa", Content: "msg3", CreatedAt: base.Add(2 * time.Second)},
		{SessionID: sessID, SenderID: "qa", RecipientID: "dev", Content: "msg4", CreatedAt: base.Add(3 * time.Second)},
	}
	for _, msg := range msgs {
		if _, err := d.store.CreateMessage(msg); err != nil {
			t.Fatalf("CreateMessage %q: %v", msg.Content, err)
		}
	}
}

func TestConversation_NoFilter(t *testing.T) {
	d := testConversationDaemon(t)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "conv-test"}))
	seedConversationMessages(t, d, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/conversation", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	msgs := decodeJSON[[]store.Message](t, rr)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
}

func TestConversation_AgentFilter(t *testing.T) {
	d := testConversationDaemon(t)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "conv-agent"}))
	seedConversationMessages(t, d, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/conversation?agent=qa", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	msgs := decodeJSON[[]store.Message](t, rr)
	// supervisor→qa and qa→dev
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for agent=qa, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.SenderID != "qa" && m.RecipientID != "qa" {
			t.Errorf("message %q does not involve qa (sender=%s, recipient=%s)", m.Content, m.SenderID, m.RecipientID)
		}
	}
}

func TestConversation_BetweenFilter(t *testing.T) {
	d := testConversationDaemon(t)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "conv-between"}))
	seedConversationMessages(t, d, sess.ID)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/conversation?between=supervisor,dev", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	msgs := decodeJSON[[]store.Message](t, rr)
	// supervisor→dev and dev→supervisor
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages between supervisor and dev, got %d", len(msgs))
	}
	for _, m := range msgs {
		isSupDev := (m.SenderID == "supervisor" && m.RecipientID == "dev")
		isDevSup := (m.SenderID == "dev" && m.RecipientID == "supervisor")
		if !isSupDev && !isDevSup {
			t.Errorf("unexpected message %q (sender=%s, recipient=%s)", m.Content, m.SenderID, m.RecipientID)
		}
	}
}

func TestConversation_BothParamsError(t *testing.T) {
	d := testConversationDaemon(t)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "conv-both"}))

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/conversation?between=a,b&agent=x", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConversation_MalformedBetween(t *testing.T) {
	d := testConversationDaemon(t)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "conv-malformed"}))

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/conversation?between=a,b,c", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConversation_UnknownSession(t *testing.T) {
	d := testConversationDaemon(t)

	rr := doRequest(t, d, "GET", "/sessions/does-not-exist/conversation", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
