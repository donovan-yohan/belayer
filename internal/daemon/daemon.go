package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/store"
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

	// toolsMu guards the tools map. Tools are registered per session
	// via POST /sessions/{id}/tools and looked up during execution.
	toolsMu sync.RWMutex
	tools   map[string][]agent.ToolSpec // keyed by session ID
}

// New creates a Daemon with the given config. Call Start to begin serving.
func New(cfg Config) (*Daemon, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("daemon: create db dir: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: open store: %w", err)
	}

	d := &Daemon{
		store:  st,
		config: cfg,
		tools:  make(map[string][]agent.ToolSpec),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.handleHealth)
	mux.HandleFunc("POST /sessions", d.handleCreateSession)
	mux.HandleFunc("GET /sessions", d.handleListSessions)
	mux.HandleFunc("GET /sessions/{id}", d.handleGetSession)
	mux.HandleFunc("PATCH /sessions/{id}", d.handleUpdateSession)
	mux.HandleFunc("GET /sessions/{id}/events", d.handleGetEvents)
	mux.HandleFunc("POST /sessions/{id}/events", d.handleLogEvent)
	mux.HandleFunc("POST /sessions/{id}/messages", d.handleSendMessage)
	mux.HandleFunc("POST /sessions/{id}/messages/broadcast", d.handleBroadcastMessage)
	mux.HandleFunc("GET /sessions/{id}/messages", d.handleListMessages)

	// Tool routing endpoints.
	mux.HandleFunc("POST /sessions/{id}/tools", d.handleRegisterTool)
	mux.HandleFunc("GET /sessions/{id}/tools", d.handleListTools)
	mux.HandleFunc("POST /sessions/{id}/tools/{name}", d.handleExecuteTool)

	d.server = &http.Server{Handler: mux}
	return d, nil
}

// Start begins listening on the Unix socket. Blocks until the server is stopped.
func (d *Daemon) Start(ctx context.Context) error {
	// Remove stale socket file.
	if err := os.MkdirAll(filepath.Dir(d.config.SocketPath), 0o755); err != nil {
		return fmt.Errorf("daemon: create socket dir: %w", err)
	}
	os.Remove(d.config.SocketPath)

	ln, err := net.Listen("unix", d.config.SocketPath)
	if err != nil {
		return fmt.Errorf("daemon: listen: %w", err)
	}
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

// --- Session handlers ---

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
		if strings.Contains(err.Error(), "no rows") {
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
	events, err := d.store.QueryEvents(id)
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
