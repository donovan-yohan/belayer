package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
)

// Config holds daemon startup configuration.
type Config struct {
	SocketPath  string
	DBPath      string
	BelayerRoot string // directory containing hermes_bridge/ (for PYTHONPATH)
}

// DefaultConfig returns config using ~/.belayer/ paths.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".belayer")
	return Config{
		SocketPath: filepath.Join(base, "daemon.sock"),
		DBPath:     filepath.Join(base, "belayer.db"),
	}
}

// Daemon is the long-running belayer supervisor process.
type Daemon struct {
	store    *store.Store
	listener net.Listener
	server   *http.Server
	config   Config
	broker   broker.Broker

	spawnBridgeAgent func(req agentSpawnRequest) (*bridge.Process, error)

	// Bridge process tracking: sessionID/agentName -> *bridge.Process
	bridgeMu    sync.RWMutex
	bridgeProcs map[string]*bridge.Process

	// Tool registry: per-session tool specs, protected by toolsMu.
	toolsMu sync.RWMutex
	tools   map[string][]agent.ToolSpec
}

// New creates a Daemon with the given config. Call Start to begin serving.
func New(cfg Config) (*Daemon, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create db dir: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: open store: %w", err)
	}

	d := &Daemon{
		store:       st,
		config:      cfg,
		tools:       make(map[string][]agent.ToolSpec),
		bridgeProcs: make(map[string]*bridge.Process),
	}
	d.broker = broker.NewMemoryBroker(st)
	d.spawnBridgeAgent = d.bridgeLaunchAgent
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
	return d, nil
}

// Start begins listening on the Unix socket. Blocks until the server is stopped.
func (d *Daemon) Start(ctx context.Context) error {
	// Remove stale socket file.
	if err := os.MkdirAll(filepath.Dir(d.config.SocketPath), 0o700); err != nil {
		return fmt.Errorf("daemon: create socket dir: %w", err)
	}
	os.Remove(d.config.SocketPath)

	ln, err := net.Listen("unix", d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("daemon: listen: %w", err)
	}
	os.Chmod(d.config.SocketPath, 0o600)
	d.listener = ln

	// Shut down gracefully when ctx is cancelled.
	go func() {
		<-ctx.Done()
		d.Shutdown(context.Background())
	}()

	if err := d.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("daemon: serve: %w", err)
	}
	return nil
}

// Shutdown gracefully drains in-flight requests and closes everything.
func (d *Daemon) Shutdown(ctx context.Context) error {
	shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := d.server.Shutdown(shutCtx)
	d.store.Close()
	os.Remove(d.config.SocketPath)
	return err
}

// --- HTTP handlers ---

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// sessionAPIResponse is the JSON-serializable session type returned by the API.
// It expands the store.Session.Repos JSON string into a proper map.
type sessionAPIResponse struct {
	ID           string            `json:"ID"`
	Name         string            `json:"Name"`
	Status       string            `json:"Status"`
	Template     string            `json:"Template"`
	Repos        map[string]string `json:"Repos"`
	WorkspaceDir string            `json:"WorkspaceDir"`
	CreatedAt    time.Time         `json:"CreatedAt"`
	UpdatedAt    time.Time         `json:"UpdatedAt"`
}

// sessionToAPIResponse converts a store.Session to the API response type,
// parsing the JSON-encoded Repos string into a map.
func sessionToAPIResponse(s store.Session) sessionAPIResponse {
	repos := map[string]string{}
	if s.Repos != "" && s.Repos != "{}" {
		_ = json.Unmarshal([]byte(s.Repos), &repos)
	}
	return sessionAPIResponse{
		ID:           s.ID,
		Name:         s.Name,
		Status:       s.Status,
		Template:     s.Template,
		Repos:        repos,
		WorkspaceDir: s.WorkspaceDir,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

type createSessionRequest struct {
	Name         string            `json:"name"`
	Template     string            `json:"template,omitempty"`
	Repos        map[string]string `json:"repos,omitempty"`
	WorkspaceDir string            `json:"workspace_dir,omitempty"`
}

func (d *Daemon) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	reposJSON := "{}"
	if len(req.Repos) > 0 {
		b, err := json.Marshal(req.Repos)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repos"})
			return
		}
		reposJSON = string(b)
	}

	sess := store.Session{
		Name:         req.Name,
		Template:     req.Template,
		Status:       "pending",
		Repos:        reposJSON,
		WorkspaceDir: req.WorkspaceDir,
	}
	id, err := d.store.CreateSession(sess)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	created, err := d.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	d.store.LogEvent(store.SessionEvent{
		SessionID: id,
		Type:      "session_created",
		Data:      mustJSON(map[string]string{"name": req.Name, "template": req.Template}),
	})

	writeJSON(w, http.StatusCreated, sessionToAPIResponse(created))
}

func (d *Daemon) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := d.store.ListSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := make([]sessionAPIResponse, len(sessions))
	for i, s := range sessions {
		resp[i] = sessionToAPIResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (d *Daemon) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := d.store.GetSession(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sessionToAPIResponse(sess))
}

type updateSessionRequest struct {
	Status       string `json:"status"`
	WorkspaceDir string `json:"workspace_dir,omitempty"`
}

func (d *Daemon) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Status == "" && req.WorkspaceDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status or workspace_dir is required"})
		return
	}

	if req.Status != "" {
		if err := d.store.UpdateSessionStatus(id, req.Status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		d.store.LogEvent(store.SessionEvent{
			SessionID: id,
			Type:      "session_status_changed",
			Data:      mustJSON(map[string]string{"status": req.Status}),
		})
	}

	if req.WorkspaceDir != "" {
		if err := d.store.UpdateSessionWorkspaceDir(id, req.WorkspaceDir); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	sess, err := d.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sessionToAPIResponse(sess))
}

func (d *Daemon) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	afterID, waitFor, err := parseEventCursor(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	events, err := d.querySessionEvents(r.Context(), id, afterID, waitFor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

type logEventRequest struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

func (d *Daemon) handleLogEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req logEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required"})
		return
	}

	evt := store.SessionEvent{
		SessionID: id,
		Type:      req.Type,
		Data:      req.Data,
	}
	if err := d.store.LogEvent(evt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Process bridge events for side effects (status updates, supervisor notifications).
	if strings.HasPrefix(req.Type, "bridge:") {
		d.processBridgeEvent(id, req.Type, req.Data)
	}

	// Process agent_status events for side effects (incomplete → escalation).
	if strings.HasPrefix(req.Type, "agent_status:") {
		d.processAgentStatusEvent(id, req.Type, req.Data)
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "logged"})
}

func (d *Daemon) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	sessionIDs := strings.Split(strings.TrimSpace(r.URL.Query().Get("sessions")), ",")
	filtered := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID != "" {
			filtered = append(filtered, sessionID)
		}
	}
	if len(filtered) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessions is required"})
		return
	}

	afterID, waitFor, err := parseEventCursor(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("after")) == "" {
		existing, err := d.store.QueryEventsForSessionsAfter(filtered, 0)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(existing) > 0 {
			afterID = existing[len(existing)-1].ID
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	enc := json.NewEncoder(w)
	lastID := afterID
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	deadline := time.Time{}
	if waitFor > 0 {
		deadline = time.Now().Add(waitFor)
	}

	for {
		events, err := d.store.QueryEventsForSessionsAfter(filtered, lastID)
		if err != nil {
			errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()
			return
		}
		if len(events) > 0 {
			for _, evt := range events {
				fmt.Fprint(w, "event: session_event\ndata: ")
				if err := enc.Encode(evt); err != nil {
					return
				}
				fmt.Fprint(w, "\n")
				lastID = evt.ID
			}
			flusher.Flush()
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprint(w, ": timeout\n\n")
			flusher.Flush()
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-keepalive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func parseEventCursor(r *http.Request) (int64, time.Duration, error) {
	afterRaw := strings.TrimSpace(r.URL.Query().Get("after"))
	waitRaw := strings.TrimSpace(r.URL.Query().Get("wait"))

	var afterID int64
	if afterRaw != "" {
		parsed, err := strconv.ParseInt(afterRaw, 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("after must be a non-negative integer")
		}
		afterID = parsed
	}

	var waitFor time.Duration
	if waitRaw != "" {
		parsed, err := time.ParseDuration(waitRaw)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("wait must be a valid non-negative duration")
		}
		waitFor = parsed
	}

	return afterID, waitFor, nil
}

func (d *Daemon) querySessionEvents(ctx context.Context, sessionID string, afterID int64, waitFor time.Duration) ([]store.SessionEvent, error) {
	if waitFor <= 0 {
		if afterID > 0 {
			return d.store.QueryEventsAfter(sessionID, afterID)
		}
		return d.store.QueryEvents(sessionID)
	}

	deadline := time.Now().Add(waitFor)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		var (
			events []store.SessionEvent
			err    error
		)
		if afterID > 0 {
			events, err = d.store.QueryEventsAfter(sessionID, afterID)
		} else {
			events, err = d.store.QueryEvents(sessionID)
		}
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
		if time.Now().After(deadline) {
			return []store.SessionEvent{}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	events, err := d.store.SearchEvents(q)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// bridgeKey returns the map key for a bridge process given session and agent name.
func bridgeKey(sessionID, agentName string) string {
	return sessionID + "/" + agentName
}
