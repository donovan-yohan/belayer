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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/docker"
	"github.com/donovan-yohan/belayer/internal/store"
	"gopkg.in/yaml.v3"
)

// Config holds daemon startup configuration.
type Config struct {
	SocketPath string
	DBPath     string
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

	d := &Daemon{store: st, config: cfg, tools: make(map[string][]agent.ToolSpec)}
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
	mux.HandleFunc("GET /search", d.handleSearch)
	mux.HandleFunc("POST /sessions/{id}/workbench", d.handleCreateWorkbench)
	mux.HandleFunc("GET /sessions/{id}/workbench", d.handleGetWorkbench)
	mux.HandleFunc("DELETE /sessions/{id}/workbench", d.handleDeleteWorkbench)
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

type createSessionRequest struct {
	Name     string `json:"name"`
	Template string `json:"template,omitempty"`
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

	sess := store.Session{
		Name:     req.Name,
		Template: req.Template,
		Status:   "pending",
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

	writeJSON(w, http.StatusCreated, created)
}

func (d *Daemon) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := d.store.ListSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sessions)
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
	writeJSON(w, http.StatusOK, sess)
}

type updateSessionRequest struct {
	Status string `json:"status"`
}

func (d *Daemon) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Status == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
		return
	}

	if err := d.store.UpdateSessionStatus(id, req.Status); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	d.store.LogEvent(store.SessionEvent{
		SessionID: id,
		Type:      "session_status_changed",
		Data:      mustJSON(map[string]string{"status": req.Status}),
	})

	sess, err := d.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sess)
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
	if waitFor <= 0 {
		waitFor = 30 * time.Second
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
			fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
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

type createWorkbenchRequest struct {
	Spec      string `json:"spec,omitempty"`
	Endpoints string `json:"endpoints,omitempty"`
}

func (d *Daemon) handleCreateWorkbench(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Verify session exists before creating workbench.
	if _, err := d.store.GetSession(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var req createWorkbenchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if existing, err := d.store.GetWorkbenchBySession(id); err == nil && existing.Status == "ready" {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	sandboxPath, err := sandboxDir(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	meta, err := docker.LoadRuntimeMetadata(sandboxPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("load runtime metadata: %v", err)})
		return
	}

	spec := docker.WorkbenchConfigSpec{}
	if meta.Workbench != nil {
		spec = *meta.Workbench
	}
	if req.Spec != "" && req.Spec != "{}" {
		if err := yaml.Unmarshal([]byte(req.Spec), &spec); err != nil {
			if err := json.Unmarshal([]byte(req.Spec), &spec); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid workbench spec: %v", err)})
				return
			}
		}
	}
	if len(spec.Services) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no workbench services configured for this session"})
		return
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal workbench spec: %v", err)})
		return
	}

	workbenchState, err := d.store.GetWorkbenchBySession(id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		workbenchState = store.WorkbenchState{
			SessionID: id,
			Status:    "provisioning",
			Spec:      string(specJSON),
			Endpoints: "{}",
		}
		if _, err := d.store.CreateWorkbench(workbenchState); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		workbenchState, err = d.store.GetWorkbenchBySession(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_ = d.store.UpdateWorkbenchStatus(workbenchState.ID, "provisioning")
	}

	workbench, err := docker.NewWorkbench(docker.WorkbenchConfig{
		SessionID:     id,
		Spec:          spec,
		WorktreePaths: meta.RepoWorktrees,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := workbench.Create(); err != nil {
		_ = d.store.UpdateWorkbenchStatus(workbenchState.ID, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := workbench.Start(); err != nil {
		_ = d.store.UpdateWorkbenchStatus(workbenchState.ID, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	timeout, err := time.ParseDuration(spec.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if err := workbench.WaitForHealthy(timeout); err != nil {
		_ = d.store.UpdateWorkbenchStatus(workbenchState.ID, "failed")
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": err.Error()})
		return
	}

	endpointsJSON, err := json.Marshal(workbench.Endpoints())
	if err != nil {
		_ = d.store.UpdateWorkbenchStatus(workbenchState.ID, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("marshal workbench endpoints: %v", err)})
		return
	}
	if err := d.store.UpdateWorkbenchStatus(workbenchState.ID, "ready"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := d.store.UpdateWorkbenchEndpoints(workbenchState.ID, string(endpointsJSON)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	created, err := d.store.GetWorkbenchBySession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	d.store.LogEvent(store.SessionEvent{
		SessionID: id,
		Type:      "workbench_created",
		Data:      mustJSON(map[string]string{"workbench_id": created.ID, "status": created.Status}),
	})

	writeJSON(w, http.StatusCreated, created)
}

func (d *Daemon) handleGetWorkbench(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wb, err := d.store.GetWorkbenchBySession(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "workbench not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, wb)
}

func (d *Daemon) handleDeleteWorkbench(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if home, err := os.UserHomeDir(); err == nil {
		workbenchDir := filepath.Join(home, ".belayer", "workbenches", id)
		composePath := filepath.Join(workbenchDir, "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			_ = exec.Command("docker", "compose", "-f", composePath, "down").Run()
		}
		_ = os.RemoveAll(workbenchDir)
	}
	if err := d.store.DeleteWorkbenchBySession(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
