package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/trace"
)

// testDaemonWithTrace creates a Daemon backed by a filesystem store (not
// in-memory) with a real trace.Writer rooted at <tmpdir>/traces, so spill
// tests can inspect trace fragment files.
func testDaemonWithTrace(t *testing.T) (*Daemon, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "belayer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	traceBase := filepath.Join(dir, "traces")
	tw, err := trace.NewWriter(traceBase)
	if err != nil {
		t.Fatalf("trace.NewWriter: %v", err)
	}

	reg := &sandbox.Registry{}
	reg.Register(sandbox.DefaultMode, &sandbox.Noop{})
	d := &Daemon{
		store:                      st,
		config:                     Config{DBPath: dbPath},
		daemonInstanceID:           "test-daemon-trace",
		tools:                      make(map[string][]agent.ToolSpec),
		bridgeProcs:                make(map[string]*bridge.Process),
		bridgeShuttingDownSessions: make(map[string]bool),
		sessionSandboxes:           make(map[string]sessionSandbox),
		sseSubscribers:             make(map[*sseSubscriber]struct{}),
		sandboxDrivers:             reg,
		runtime:                    &runtime.Noop{},
		startCtx:                   context.Background(),
		archiver:                   newArchiveManager(st, "test-trace-instance"),
		archiveDrainTimeout:        30 * time.Second,
		shutdownHTTPTimeout:        5 * time.Second,
		sseKeepaliveInterval:       15 * time.Second,
		traceWriter:                tw,
		traceBase:                  traceBase,
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
	d.server = &http.Server{Handler: mux}

	t.Cleanup(func() {
		if err := tw.Close(); err != nil {
			t.Logf("trace writer close: %v", err)
		}
		if d.archiver != nil {
			d.archiver.inflight.Wait()
		}
		st.Close()
	})
	return d, dir
}

// TestHandleLogEvent_TraceSpill verifies that a ≥64 KB payload at trace tier
// is spilled to a fragment file and the event row data is replaced with the
// sentinel JSON, and trace columns are populated.
func TestHandleLogEvent_TraceSpill(t *testing.T) {
	d, dir := testDaemonWithTrace(t)

	// Create a trace-tier session directly in the store.
	sessID, err := d.store.CreateSession(store.Session{
		Name:     "spill-session",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Build a 70 KB payload with an agent field.
	bigData := strings.Repeat("A", 70*1024-50)
	payload := fmt.Sprintf(`{"agent":"implementer","big":%q}`, bigData)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Read event from store.
	events, err := d.store.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	// The data in the DB row should be the sentinel JSON.
	var sentinel struct {
		Agent string `json:"agent"`
		Trace bool   `json:"_trace"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
		t.Fatalf("unmarshal event data %q: %v", evt.Data, err)
	}
	if sentinel.Agent != "implementer" {
		t.Errorf("sentinel agent: got %q, want %q", sentinel.Agent, "implementer")
	}
	if !sentinel.Trace {
		t.Errorf("sentinel _trace: expected true")
	}

	// Assert trace columns are populated.
	var traceFilePath string
	var traceOffset, traceLength int64
	var traceFileNull, traceOffsetNull, traceLengthNull bool

	row := d.store.DB().QueryRow(
		`SELECT trace_file IS NULL, trace_offset IS NULL, trace_length IS NULL,
		        COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0)
		 FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sessID,
	)
	if err := row.Scan(&traceFileNull, &traceOffsetNull, &traceLengthNull,
		&traceFilePath, &traceOffset, &traceLength); err != nil {
		t.Fatalf("scan trace columns: %v", err)
	}

	if traceFileNull {
		t.Fatalf("trace_file is NULL; expected fragment path")
	}
	if traceOffsetNull {
		t.Errorf("trace_offset is NULL")
	}
	if traceLengthNull {
		t.Errorf("trace_length is NULL")
	}
	if traceFilePath == "" {
		t.Fatalf("trace_file is empty string")
	}

	// Verify the fragment file exists and contains the scrubbed payload at the given offset/length.
	fileData, err := os.ReadFile(traceFilePath)
	if err != nil {
		t.Fatalf("read fragment file %q: %v", traceFilePath, err)
	}
	if traceOffset+traceLength > int64(len(fileData)) {
		t.Fatalf("fragment bounds [%d:%d] exceed file size %d",
			traceOffset, traceOffset+traceLength, len(fileData))
	}
	fragContent := string(fileData[traceOffset : traceOffset+traceLength])
	wantContent := Scrub(payload)
	if fragContent != wantContent {
		limit := 200
		got := fragContent
		if len(got) > limit {
			got = got[:limit] + "..."
		}
		want := wantContent
		if len(want) > limit {
			want = want[:limit] + "..."
		}
		t.Errorf("fragment content mismatch\ngot:  %s\nwant: %s", got, want)
	}

	_ = dir // keep dir alive for t.TempDir cleanup
}

// TestHandleLogEvent_StandardTruncate verifies that a ≥64 KB payload at
// standard tier is truncated (not spilled), stored as a structured JSON sentinel,
// and trace columns remain NULL.
// This is also TestHandleLogEvent_StandardSentinelIsValidJSON.
func TestHandleLogEvent_StandardTruncate(t *testing.T) {
	d, _ := testDaemonWithTrace(t)

	// Create a standard-tier session.
	sessID, err := d.store.CreateSession(store.Session{
		Name:     "truncate-session",
		LogLevel: LogLevelStandard,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	bigData := strings.Repeat("B", 70*1024-50)
	payload := fmt.Sprintf(`{"agent":"implementer","big":%q}`, bigData)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	events, err := d.store.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	// Data must be valid parseable JSON sentinel (not a prose string).
	var sentinel struct {
		Truncated    bool   `json:"_truncated"`
		Tier         string `json:"tier"`
		OriginalSize int    `json:"original_size"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
		t.Fatalf("event data is not valid JSON: %v\ndata: %s", err, evt.Data)
	}
	if !sentinel.Truncated {
		t.Errorf("sentinel._truncated: expected true, got false")
	}
	if sentinel.Tier != "standard" {
		t.Errorf("sentinel.tier: got %q, want %q", sentinel.Tier, "standard")
	}
	if sentinel.OriginalSize <= 65536 {
		t.Errorf("sentinel.original_size: got %d, expected > 65536", sentinel.OriginalSize)
	}
	if sentinel.Reason == "" {
		t.Errorf("sentinel.reason: expected non-empty")
	}

	// Trace columns must be NULL (no spill for standard tier).
	var traceFileNull, traceOffsetNull, traceLengthNull bool
	row := d.store.DB().QueryRow(
		`SELECT trace_file IS NULL, trace_offset IS NULL, trace_length IS NULL
		 FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sessID,
	)
	if err := row.Scan(&traceFileNull, &traceOffsetNull, &traceLengthNull); err != nil {
		t.Fatalf("scan trace columns: %v", err)
	}
	if !traceFileNull {
		t.Errorf("trace_file: expected NULL for standard tier")
	}
	if !traceOffsetNull {
		t.Errorf("trace_offset: expected NULL for standard tier")
	}
	if !traceLengthNull {
		t.Errorf("trace_length: expected NULL for standard tier")
	}
}

// TestHandleLogEvent_TraceScrubsEveryPayload verifies that a sub-64KB trace-tier
// payload containing a secret is scrubbed before storage.
func TestHandleLogEvent_TraceScrubsEveryPayload(t *testing.T) {
	d, _ := testDaemonWithTrace(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "scrub-small-session",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// ~1 KB payload containing a secret — well below the 65536 spill threshold.
	secret := "secret123"
	payload := fmt.Sprintf(`{"agent":"supervisor","api_key":%q,"filler":%q}`, secret, strings.Repeat("x", 900))

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	events, err := d.store.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	if strings.Contains(evt.Data, secret) {
		t.Errorf("stored event data still contains secret %q; scrubbing failed.\ndata: %s", secret, evt.Data[:min(200, len(evt.Data))])
	}
	if !strings.Contains(evt.Data, "<redacted>") {
		t.Errorf("expected <redacted> in scrubbed data but not found.\ndata: %s", evt.Data[:min(200, len(evt.Data))])
	}
}

// TestHandleLogEvent_RejectsPathTraversalAgent verifies that a trace-tier payload
// with a path-traversal agent name (../../etc/passwd) is not spilled to disk.
// The event is stored with a structured truncation sentinel instead.
func TestHandleLogEvent_RejectsPathTraversalAgent(t *testing.T) {
	d, dir := testDaemonWithTrace(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "traversal-session",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Build a ≥65536 byte payload with a path-traversal agent name.
	filler := strings.Repeat("T", 70*1024)
	payload := fmt.Sprintf(`{"agent":"../../etc/passwd","filler":%q}`, filler)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// No trace directory should have been created under <dir>/traces/<sessID>/.
	tracePath := filepath.Join(dir, "traces", sessID)
	if _, err := os.Stat(tracePath); err == nil {
		t.Errorf("expected no trace directory for path-traversal agent, but %s exists", tracePath)
	}

	events, err := d.store.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	// Data should be a structured truncation sentinel with reason "invalid agent id".
	var sentinel struct {
		Truncated bool   `json:"_truncated"`
		Tier      string `json:"tier"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
		t.Fatalf("event data is not valid JSON: %v\ndata: %s", err, evt.Data)
	}
	if !sentinel.Truncated {
		t.Errorf("sentinel._truncated: expected true")
	}
	if sentinel.Tier != "trace" {
		t.Errorf("sentinel.tier: got %q, want %q", sentinel.Tier, "trace")
	}
	if !strings.Contains(sentinel.Reason, "invalid agent id") {
		t.Errorf("sentinel.reason: expected to contain %q, got %q", "invalid agent id", sentinel.Reason)
	}

	// Trace columns must be NULL (no spill happened).
	var traceFileNull bool
	row := d.store.DB().QueryRow(`SELECT trace_file IS NULL FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sessID)
	if err := row.Scan(&traceFileNull); err != nil {
		t.Fatalf("scan trace_file null: %v", err)
	}
	if !traceFileNull {
		t.Errorf("trace_file: expected NULL when agent validation fails")
	}
}

// TestHandleLogEvent_RejectsPathTraversalSlash verifies that an agent name
// containing '/' is rejected similarly to TestHandleLogEvent_RejectsPathTraversalAgent.
func TestHandleLogEvent_RejectsPathTraversalSlash(t *testing.T) {
	d, dir := testDaemonWithTrace(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "slash-traversal-session",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	filler := strings.Repeat("T", 70*1024)
	payload := fmt.Sprintf(`{"agent":"foo/bar","filler":%q}`, filler)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	tracePath := filepath.Join(dir, "traces", sessID)
	if _, err := os.Stat(tracePath); err == nil {
		t.Errorf("expected no trace directory for slash agent, but %s exists", tracePath)
	}

	events, err := d.store.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	var sentinel struct {
		Truncated bool   `json:"_truncated"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
		t.Fatalf("event data is not valid JSON: %v\ndata: %s", err, evt.Data)
	}
	if !sentinel.Truncated {
		t.Errorf("sentinel._truncated: expected true")
	}
	if !strings.Contains(sentinel.Reason, "invalid agent id") {
		t.Errorf("sentinel.reason: expected %q, got %q", "invalid agent id", sentinel.Reason)
	}
}

// min returns the smaller of a and b (helper for test output truncation).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
