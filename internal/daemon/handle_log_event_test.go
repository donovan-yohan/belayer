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
		config:                     Config{},
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
// standard tier is truncated (not spilled) and trace columns remain NULL.
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

	const truncSuffix = "…(truncated; upgrade to trace tier to capture)"
	if !strings.HasSuffix(evt.Data, truncSuffix) {
		tail := evt.Data
		if len(tail) > 80 {
			tail = "..." + tail[len(tail)-80:]
		}
		t.Errorf("expected data to have truncation suffix; got last 80 chars: %q", tail)
	}

	// Trace columns must be NULL.
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
