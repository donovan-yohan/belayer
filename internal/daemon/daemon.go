package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
)

// Config holds daemon startup configuration.
type Config struct {
	SocketPath  string
	DBPath      string
	BelayerRoot string // directory containing hermes_bridge/ (for PYTHONPATH)

	// TCPAddr, if non-empty, causes the daemon to also bind a TCP listener at
	// this address (e.g. "0.0.0.0:7523"). Used when sandbox.mode=clamshell so
	// bridge subprocesses inside Docker containers can reach the daemon via the
	// Docker host gateway (172.17.0.1). Use "host:0" to let the OS pick a port.
	TCPAddr string
	// DockerHostGateway is the IP address of the Docker host as seen from inside
	// Docker containers. Used to build BELAYER_SOCKET for clamshell bridge
	// subprocesses. Defaults to "172.17.0.1" (standard Docker bridge network).
	DockerHostGateway string

	// WorkspaceSockPath, if non-empty, causes the daemon to also bind a Unix
	// socket at this path. Used when sandbox.mode=clamshell so bridge
	// subprocesses inside Docker containers can reach the daemon via a
	// bind-mounted workspace path (e.g. /workspace/.belayer/daemon.sock).
	WorkspaceSockPath string

	// SandboxDrivers is the registry the daemon resolves per-session drivers
	// against. Each session's driver is chosen by name from .belayer/config.yaml's
	// sandbox.mode. Nil falls back to a registry containing only the noop driver,
	// which is what existing tests rely on.
	SandboxDrivers *sandbox.Registry
	// Runtime overrides the dev-stack provider.
	// Nil falls back to &runtime.Noop{}.
	Runtime runtime.Provider

	// BridgeAPIKey, BridgeBaseURL, BridgeProvider are injected as BELAYER_API_KEY /
	// BELAYER_BASE_URL / BELAYER_PROVIDER into every bridge subprocess. Used in
	// clamshell mode where the sandbox user has no Hermes provider configured.
	BridgeAPIKey  string
	BridgeBaseURL string
	BridgeProvider string
}

// DefaultConfig returns config using ~/.belayer/ paths.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".belayer")
	return Config{
		SocketPath:        filepath.Join(base, "daemon.sock"),
		DBPath:            filepath.Join(base, "belayer.db"),
		DockerHostGateway: "172.17.0.1",
	}
}

// Daemon is the long-running belayer supervisor process.
type Daemon struct {
	store    *store.Store
	listener net.Listener
	server   *http.Server
	config   Config
	broker   broker.Broker

	sandboxDrivers *sandbox.Registry
	runtime        runtime.Provider

	// runtimeEndpoints is the set of endpoints reported by runtime.Up at daemon
	// startup. Written once from Start before the HTTP server begins serving,
	// then read from sandbox-creating goroutines. The happens-before relationship
	// established by server.Serve makes the write visible without an explicit lock.
	runtimeEndpoints []runtime.Endpoint

	// Context from Start, used as the parent for sandbox/runtime lifecycle
	// calls so they observe daemon shutdown. Initialized to context.Background
	// in New so tests that skip Start still work.
	startCtx context.Context

	// tcpListener is the optional TCP listener for clamshell container access.
	// Nil when config.TCPAddr is empty.
	tcpListener *http.Server
	tcpPort     int

	// workspaceSockListener is an optional Unix socket inside the workspace
	// directory so clamshell bridge subprocesses can reach the daemon via a
	// bind-mounted path without network proxying.
	workspaceSockListener *http.Server

	// Per-session sandbox state. Each entry caches the driver resolved from
	// .belayer/config.yaml's sandbox.mode plus the Handle returned by Create.
	// Populated lazily on first agent spawn, cleared on terminal session
	// transitions and during Shutdown.
	sandboxMu        sync.Mutex
	sessionSandboxes map[string]sessionSandbox

	spawnBridgeAgent func(req agentSpawnRequest) (*bridge.Process, error)

	// Bridge process tracking: sessionID/agentName -> *bridge.Process
	bridgeMu    sync.RWMutex
	bridgeProcs map[string]*bridge.Process

	// Tool registry: per-session tool specs, protected by toolsMu.
	toolsMu sync.RWMutex
	tools   map[string][]agent.ToolSpec

	// Lifecycle / shutdown fields.
	draining             atomic.Bool   // set at start of Shutdown; readable by future /health and SSE handlers
	archiver             *archiveManager
	archiveDrainTimeout  time.Duration // max time Phase 3 archive drain may run
	shutdownHTTPTimeout  time.Duration // max time HTTP servers get to drain in-flight requests
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
		store:               st,
		config:              cfg,
		tools:               make(map[string][]agent.ToolSpec),
		bridgeProcs:         make(map[string]*bridge.Process),
		sessionSandboxes:    make(map[string]sessionSandbox),
		startCtx:            context.Background(),
		archiveDrainTimeout: 30 * time.Second,
		shutdownHTTPTimeout: 5 * time.Second,
	}
	d.archiver = newArchiveManager(st)
	if cfg.SandboxDrivers != nil {
		d.sandboxDrivers = cfg.SandboxDrivers
	} else {
		// Nil falls back to a registry with only the noop driver so tests that
		// skip the CLI wiring path still start successfully. Callers that want
		// the clamshell stub must pass the process-wide sandbox.Default.
		fallback := &sandbox.Registry{}
		fallback.Register(sandbox.DefaultMode, &sandbox.Noop{})
		d.sandboxDrivers = fallback
	}
	if cfg.Runtime != nil {
		d.runtime = cfg.Runtime
	} else {
		d.runtime = &runtime.Noop{}
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
	d.startCtx = ctx

	// cleanupExtraListeners shuts down any TCP/workspace servers started below
	// and removes the workspace socket file. Called on any early-return error
	// path so we don't leak background goroutines or bound sockets.
	cleanupExtraListeners := func() {
		if d.tcpListener != nil {
			_ = d.tcpListener.Close()
		}
		if d.workspaceSockListener != nil {
			_ = d.workspaceSockListener.Close()
		}
		if d.config.WorkspaceSockPath != "" {
			os.Remove(d.config.WorkspaceSockPath)
		}
	}

	// Optional TCP listener for clamshell container bridge access.
	if d.config.TCPAddr != "" {
		tcpLn, err := net.Listen("tcp", d.config.TCPAddr)
		if err != nil {
			ln.Close()
			return fmt.Errorf("daemon: listen tcp %s: %w", d.config.TCPAddr, err)
		}
		d.tcpPort = tcpLn.Addr().(*net.TCPAddr).Port
		d.tcpListener = &http.Server{Handler: d.server.Handler}
		go func() {
			if serveErr := d.tcpListener.Serve(tcpLn); serveErr != nil && serveErr != http.ErrServerClosed {
				log.Printf("daemon: tcp serve: %v", serveErr)
			}
		}()
		log.Printf("daemon: TCP listener on %s (port %d)", d.config.TCPAddr, d.tcpPort)
	}

	// Optional workspace Unix socket for clamshell container bridge access.
	// The workspace directory is bind-mounted into clamshell containers so the
	// bridge can reach the daemon via /workspace/.belayer/daemon.sock without
	// any network proxying.
	if d.config.WorkspaceSockPath != "" {
		if err := os.MkdirAll(filepath.Dir(d.config.WorkspaceSockPath), 0o755); err != nil {
			cleanupExtraListeners()
			ln.Close()
			return fmt.Errorf("daemon: create workspace sock dir: %w", err)
		}
		os.Remove(d.config.WorkspaceSockPath)
		wsLn, err := net.Listen("unix", d.config.WorkspaceSockPath)
		if err != nil {
			cleanupExtraListeners()
			ln.Close()
			return fmt.Errorf("daemon: listen workspace sock %s: %w", d.config.WorkspaceSockPath, err)
		}
		// Readable/writable by any user so the non-root sandbox user inside the
		// clamshell container can reach the socket through the bind mount.
		// Execute bit is meaningless on a socket, so 0o666 is strictly tighter
		// than 0o777. TODO: narrow to 0o660 via dedicated group + chown once
		// the container UID/GID mapping is stabilized.
		if err := os.Chmod(d.config.WorkspaceSockPath, 0o666); err != nil {
			wsLn.Close()
			cleanupExtraListeners()
			ln.Close()
			return fmt.Errorf("daemon: chmod workspace sock %s: %w", d.config.WorkspaceSockPath, err)
		}
		d.workspaceSockListener = &http.Server{Handler: d.server.Handler}
		go func() {
			if serveErr := d.workspaceSockListener.Serve(wsLn); serveErr != nil && serveErr != http.ErrServerClosed {
				log.Printf("daemon: workspace sock serve: %v", serveErr)
			}
		}()
		log.Printf("daemon: workspace socket on %s", d.config.WorkspaceSockPath)
	}

	// Provision the runtime before serving. For the noop provider this is a
	// no-op; the hook exists so future providers (command, clamshell, ...)
	// can fail fast if the dev stack can't come up. Captured endpoints flow
	// into each session's sandbox via ensureSandboxHandle.
	endpoints, err := d.runtime.Up(ctx)
	if err != nil {
		cleanupExtraListeners()
		ln.Close()
		return fmt.Errorf("daemon: runtime up: %w", err)
	}
	d.runtimeEndpoints = endpoints

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

// TCPPort returns the port the TCP listener is bound to, or 0 if no TCP
// listener was configured. Valid after Start() returns without error.
func (d *Daemon) TCPPort() int { return d.tcpPort }

// Shutdown gracefully drains in-flight requests and closes everything.
//
// The phase ordering is a contract, not a convenience: external SSE consumers
// must see the draining signal (Phase b) and archive workers must finish
// flushing (Phase c) BEFORE HTTP sockets close (Phase d). Reordering breaks
// cragd's ability to distinguish a graceful drain from a daemon crash. Each
// phase is its own method so the Shutdown body reads as a strict sequence.
func (d *Daemon) Shutdown(ctx context.Context) error {
	// Phase (a): mark daemon as draining. Swap guards against double-Shutdown
	// (signal handler + parent cleanup both calling in) — second call is a no-op.
	if d.draining.Swap(true) {
		return nil
	}

	d.announceDraining(ctx) // Phase (b) — Phase 5 wires daemon_draining SSE emission.
	d.drainArchive(ctx)     // Phase (c) — Phase 3 wires archiveManager.drainAll.

	shutCtx, cancel := context.WithTimeout(ctx, d.shutdownHTTPTimeout)
	defer cancel()

	err := d.shutdownHTTP(shutCtx) // Phase (d)
	d.teardownSandboxes(shutCtx)   // Phase (e)
	d.downRuntime(shutCtx)         // Phase (f)
	d.closeAndCleanup()            // Phase (g)
	return err
}

// announceDraining is the Phase (b) seat for Phase 5's SSE daemon_draining
// control frame emission. No-op until Phase 5 lands.
func (d *Daemon) announceDraining(ctx context.Context) {
	// Phase 5: emit `event: daemon_draining\ndata: {...}\n\n` to live SSE
	// consumers here. Use its own context; do NOT share shutCtx below.
	_ = ctx
}

// drainArchive runs the Phase (c) archive drain. It uses its own
// context.WithTimeout so the 30s archive budget is independent of the 5s HTTP
// shutdown timeout set in shutCtx. Reusing shutCtx here would cap the archive
// drain at 5s, violating the phase-budget contract.
func (d *Daemon) drainArchive(ctx context.Context) {
	if d.archiver == nil {
		return
	}
	archiveCtx, cancel := context.WithTimeout(ctx, d.archiveDrainTimeout)
	defer cancel()
	d.archiver.DrainAll(archiveCtx)
}

// shutdownHTTP closes the three HTTP servers (Unix socket, optional TCP,
// optional workspace Unix socket) under the given timeout-bounded context.
func (d *Daemon) shutdownHTTP(shutCtx context.Context) error {
	err := d.server.Shutdown(shutCtx)
	if d.tcpListener != nil {
		_ = d.tcpListener.Shutdown(shutCtx)
	}
	if d.workspaceSockListener != nil {
		_ = d.workspaceSockListener.Shutdown(shutCtx)
	}
	return err
}

// teardownSandboxes stops every outstanding sandbox handle and clears the
// per-session sandbox map. Safe to call on a daemon that never spawned any.
func (d *Daemon) teardownSandboxes(shutCtx context.Context) {
	d.sandboxMu.Lock()
	sessions := d.sessionSandboxes
	d.sessionSandboxes = make(map[string]sessionSandbox)
	d.sandboxMu.Unlock()
	for sessID, ss := range sessions {
		if stopErr := ss.driver.Stop(shutCtx, ss.handle); stopErr != nil {
			log.Printf("daemon: sandbox stop %s: %v", sessID, stopErr)
		}
	}
}

// downRuntime brings the runtime provider down. Errors are logged, not
// returned, because they do not block daemon exit.
func (d *Daemon) downRuntime(shutCtx context.Context) {
	if downErr := d.runtime.Down(shutCtx); downErr != nil {
		log.Printf("daemon: runtime down: %v", downErr)
	}
}

// closeAndCleanup closes the store and removes any socket files the daemon
// created. Final phase — nothing runs after this.
func (d *Daemon) closeAndCleanup() {
	d.store.Close()
	os.Remove(d.config.SocketPath)
	if d.config.WorkspaceSockPath != "" {
		os.Remove(d.config.WorkspaceSockPath)
	}
}

// sessionSandbox caches the driver (resolved from sandbox.mode per session)
// alongside the Handle returned by Create, so Exec and Stop go to the same
// driver that created the handle.
type sessionSandbox struct {
	driver sandbox.Driver
	handle sandbox.Handle
	mode   string // sandbox.mode value from .belayer/config.yaml (e.g. "noop", "clamshell")
}

// ensureSandboxHandle returns the session's sandbox state, creating one on
// first use. The driver is resolved from <sess.WorkspaceDir>/.belayer/config.yaml's
// sandbox.mode (defaulting to noop). Safe to call from multiple goroutines;
// the state is cached per session in d.sessionSandboxes.
func (d *Daemon) ensureSandboxHandle(ctx context.Context, sess store.Session) (sessionSandbox, error) {
	d.sandboxMu.Lock()
	if ss, ok := d.sessionSandboxes[sess.ID]; ok {
		d.sandboxMu.Unlock()
		return ss, nil
	}
	d.sandboxMu.Unlock()

	settings, err := sandbox.LoadSettings(sess.WorkspaceDir)
	if err != nil {
		return sessionSandbox{}, fmt.Errorf("sandbox settings: %w", err)
	}
	mode := settings.ModeOrDefault()
	driver, err := d.sandboxDrivers.Get(mode)
	if err != nil {
		return sessionSandbox{}, err
	}

	// Create outside the lock — Create may block (container pull, VM boot).
	h, err := driver.Create(ctx, sandbox.Config{
		Name:      sess.ID,
		Workspace: sess.WorkspaceDir,
		Policy:    settings.Policy,
		Endpoints: runtimeEndpointsToSandbox(d.runtimeEndpoints),
	})
	if err != nil {
		return sessionSandbox{}, err
	}

	d.sandboxMu.Lock()
	defer d.sandboxMu.Unlock()
	// Another goroutine may have raced us; keep whichever is stored first
	// so callers never see two different handles for one session.
	if existing, ok := d.sessionSandboxes[sess.ID]; ok {
		go func() {
			_ = driver.Stop(ctx, h)
		}()
		return existing, nil
	}
	ss := sessionSandbox{driver: driver, handle: h, mode: mode}
	d.sessionSandboxes[sess.ID] = ss
	return ss, nil
}

// terminateSandbox stops and forgets the sandbox state for sessionID, if any.
// Safe to call for sessions that never had a handle.
func (d *Daemon) terminateSandbox(ctx context.Context, sessionID string) {
	d.sandboxMu.Lock()
	ss, ok := d.sessionSandboxes[sessionID]
	if !ok {
		d.sandboxMu.Unlock()
		return
	}
	delete(d.sessionSandboxes, sessionID)
	d.sandboxMu.Unlock()
	if err := ss.driver.Stop(ctx, ss.handle); err != nil {
		log.Printf("daemon: sandbox stop %s: %v", sessionID, err)
	}
}

// isTerminalSessionStatus reports whether a session status means the session
// is finished and its sandbox can be torn down.
func isTerminalSessionStatus(status string) bool {
	switch status {
	case "complete", "blocked", "failed", "cancelled", "needs_human_review", "stalled":
		return true
	}
	return false
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
		if isTerminalSessionStatus(req.Status) {
			// Archive before sandbox teardown so the archiver reads session
			// state before any teardown side-effects can perturb the session row.
			d.archiver.ArchiveTerminal(id)
			d.terminateSandbox(r.Context(), id)
		}
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

// runtimeEndpointsToSandbox converts provider endpoints into the sandbox's
// TCPEndpoint type. The shapes are identical today but the packages are
// deliberately decoupled so a future runtime protocol field doesn't force a
// matching change in sandbox policy.
func runtimeEndpointsToSandbox(eps []runtime.Endpoint) []sandbox.TCPEndpoint {
	if len(eps) == 0 {
		return nil
	}
	out := make([]sandbox.TCPEndpoint, len(eps))
	for i, e := range eps {
		out[i] = sandbox.TCPEndpoint{Name: e.Name, Host: e.Host, Port: e.Port}
	}
	return out
}
