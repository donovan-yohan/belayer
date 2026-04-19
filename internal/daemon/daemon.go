package daemon

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/trace"
	"github.com/google/uuid"
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

	// DefaultLogLevel is the default log_level for new sessions when POST
	// /sessions does not specify one. Empty = 'standard'.
	DefaultLogLevel string

	// SSEKeepaliveInterval is how often the SSE handler emits a ": keep-alive"
	// comment to prevent idle-connection timeouts. Defaults to 15s in New().
	// Tests can set a small value (e.g., 50ms) to verify keepalive behaviour
	// without sleeping for 15 seconds.
	SSEKeepaliveInterval time.Duration
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

// sseSubscriber represents a live SSE connection that should receive
// daemon_draining when Shutdown begins.
type sseSubscriber struct {
	drain chan struct{} // closed once by announceDraining; handler returns on receive
}

// Daemon is the long-running belayer supervisor process.
type Daemon struct {
	store    *store.Store
	listener net.Listener
	server   *http.Server
	config   Config
	broker   broker.Broker

	// daemonInstanceID is a UUID assigned at daemon startup that is stable for
	// the process lifetime. It is included in /health, SSE daemon_hello frames,
	// and archive manifests so consumers can correlate streams with archives.
	daemonInstanceID string

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
	// bridgeShuttingDownSessions tracks which sessionIDs are in mid-teardown.
	// stopAllBridgeAgents sets the flag for its target session; the spawn
	// registration path aborts if the session the spawn targets is marked.
	// Entries stay set — a terminated session's agents must not respawn even
	// after its teardown goroutine returns.
	bridgeShuttingDownSessions map[string]bool

	// traceWriter spills large event payloads to fragment files at trace tier.
	traceWriter trace.Writer
	// traceBase is the root directory for trace fragment files (<sessionID>/<agent>/<NNNN>.jsonl).
	// Populated by New() as filepath.Join(filepath.Dir(cfg.DBPath), "traces").
	traceBase string

	// Tool registry: per-session tool specs, protected by toolsMu.
	toolsMu sync.RWMutex
	tools   map[string][]agent.ToolSpec

	// Lifecycle / shutdown fields.
	draining               atomic.Bool   // set at start of Shutdown; readable by /health and SSE handlers
	archiver               *archiveManager
	archiveDrainTimeout    time.Duration // max time Phase 3 archive drain may run
	shutdownHTTPTimeout    time.Duration // max time HTTP servers get to drain in-flight requests
	sseKeepaliveInterval   time.Duration // interval for SSE ": keep-alive" comments (default 15s)

	// SSE subscriber registry. Populated by handleStreamEvents; drained by
	// announceDraining on Shutdown. Protected by sseMu.
	sseMu          sync.Mutex
	sseSubscribers map[*sseSubscriber]struct{}
}

// New creates a Daemon with the given config. Call Start to begin serving.
func New(cfg Config) (*Daemon, error) {
	// Fail fast on a misconfigured default log level so operators see the error
	// at boot, not the first POST /sessions call hours later.
	if cfg.DefaultLogLevel != "" {
		if _, err := ValidateLogLevel(cfg.DefaultLogLevel); err != nil {
			return nil, fmt.Errorf("daemon: DefaultLogLevel: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create db dir: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: open store: %w", err)
	}

	keepaliveInterval := cfg.SSEKeepaliveInterval
	if keepaliveInterval <= 0 {
		keepaliveInterval = 15 * time.Second
	}

	d := &Daemon{
		store:                st,
		config:               cfg,
		daemonInstanceID:     uuid.NewString(),
		tools:                make(map[string][]agent.ToolSpec),
		bridgeProcs:                make(map[string]*bridge.Process),
		bridgeShuttingDownSessions: make(map[string]bool),
		sessionSandboxes:     make(map[string]sessionSandbox),
		sseSubscribers:       make(map[*sseSubscriber]struct{}),
		startCtx:             context.Background(),
		archiveDrainTimeout:  30 * time.Second,
		shutdownHTTPTimeout:  5 * time.Second,
		sseKeepaliveInterval: keepaliveInterval,
	}
	d.archiver = newArchiveManager(st, d.daemonInstanceID)

	traceBase := filepath.Join(filepath.Dir(cfg.DBPath), "traces")
	tw, err := trace.NewWriter(traceBase)
	if err != nil {
		return nil, fmt.Errorf("daemon: init trace writer: %w", err)
	}
	d.traceWriter = tw
	d.traceBase = traceBase

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
	mux.HandleFunc("GET /sessions/{id}/trace/{agent}/{fragment}", d.handleTraceSlice)
	mux.HandleFunc("GET /sessions/{id}/outline", d.handleOutline)
	mux.HandleFunc("GET /sessions/{id}/tool-calls", d.handleToolCalls)
	mux.HandleFunc("GET /sessions/{id}/conversation", d.handleConversation)
	mux.HandleFunc("GET /sessions/{id}/phase", d.handlePhase)

	d.server = &http.Server{Handler: mux}
	return d, nil
}

// DaemonInstanceID returns the stable UUID assigned at daemon creation.
func (d *Daemon) DaemonInstanceID() string { return d.daemonInstanceID }

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
		//
		// Chmod is best-effort only for known-nonfatal errors:
		// macOS-shared bind mounts (Colima VirtIO-FS, Docker Desktop) reject
		// chmod on Unix sockets with EINVAL (and ENOTSUP on some filesystems),
		// but the socket itself still works for same-UID callers. Under the
		// one-container-per-run (clamshell) shape the bridge subprocess runs
		// as the same sandbox UID as the daemon, so 0o666 isn't required.
		// Any other failure (EPERM/EACCES/ENOENT/...) signals a real problem
		// — fail fast so connection errors surface now, not at first use.
		if err := os.Chmod(d.config.WorkspaceSockPath, 0o666); err != nil {
			if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
				log.Printf("daemon: chmod workspace sock %s: %v (best-effort; socket still functional for same-UID peers)", d.config.WorkspaceSockPath, err)
			} else {
				_ = wsLn.Close()
				cleanupExtraListeners()
				ln.Close()
				return fmt.Errorf("daemon: chmod workspace sock %s: %w", d.config.WorkspaceSockPath, err)
			}
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

// announceDraining is Phase (b) of Shutdown. It closes every live SSE
// subscriber's drain channel, giving them time to write+flush the
// daemon_draining frame before HTTP shutdown closes the connections.
func (d *Daemon) announceDraining(ctx context.Context) {
	d.sseMu.Lock()
	subs := make([]*sseSubscriber, 0, len(d.sseSubscribers))
	for sub := range d.sseSubscribers {
		subs = append(subs, sub)
	}
	d.sseMu.Unlock()

	for _, sub := range subs {
		close(sub.drain)
	}

	n := len(subs)
	log.Printf("daemon: announced draining to %d SSE subscriber(s)", n)

	if n == 0 {
		return
	}

	// Give subscribers time to write + flush the daemon_draining frame before
	// HTTP shutdown closes connections. Cap at 200ms or the context deadline.
	grace := 200 * time.Millisecond
	select {
	case <-time.After(grace):
	case <-ctx.Done():
	}
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
	if d.traceWriter != nil {
		if err := d.traceWriter.Close(); err != nil {
			log.Printf("daemon: trace writer close: %v", err)
		}
	}
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

// stopAllBridgeAgents sends a graceful stop to every bridge process in the
// session, force-killing any that do not exit within bridgeStopTimeout. Used
// when a session reaches a terminal status (PM approval, max rejections,
// operator cancel) — without it, bridge subprocesses such as the supervisor
// keep running past session completion, burn tokens, and can self-escalate
// back to human review.
//
// Safe to call for sessions with no live bridges. Does not block on missing
// processes; each bridge is stopped in its own goroutine so a single slow
// exit does not delay the others.
func (d *Daemon) stopAllBridgeAgents(sessionID, reason string) {
	d.bridgeMu.Lock()
	d.bridgeShuttingDownSessions[sessionID] = true
	var targets []*bridge.Process
	var names []string
	for key, proc := range d.bridgeProcs {
		sid, name := parseBridgeKey(key)
		if sid != sessionID {
			continue
		}
		if proc != nil {
			targets = append(targets, proc)
			names = append(names, name)
		}
		// Drop only this session's entries; leave other sessions' bridges
		// untouched so interrupts/shutdown can still reach them.
		delete(d.bridgeProcs, key)
	}
	d.bridgeMu.Unlock()

	if len(targets) == 0 {
		return
	}

	log.Printf("daemon: stopping %d bridge agent(s) in session %s (%s): %v", len(targets), sessionID, reason, names)

	var wg sync.WaitGroup
	for i, proc := range targets {
		wg.Add(1)
		go func(p *bridge.Process, name string) {
			defer wg.Done()
			if err := p.Stop(bridgeStopTimeout); err != nil {
				log.Printf("daemon: bridge stop %s/%s: %v", sessionID, name, err)
			}
		}(proc, names[i])
	}
	wg.Wait()
}

const bridgeStopTimeout = 10 * time.Second

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

// healthCapabilities is the static capabilities block returned by GET /health.
// Extracted as a package-level var so tests can read it without parsing JSON.
var healthCapabilities = struct {
	SearchPredicates []string `json:"search_predicates"`
	ArchiveHTTP      bool     `json:"archive_http"`
	SSEControlFrames []string `json:"sse_control_frames"`
	LogLevels        []string `json:"log_levels"`
}{
	SearchPredicates: []string{"q", "session", "type_prefix", "agent", "after", "before"},
	ArchiveHTTP:      true,
	SSEControlFrames: []string{"daemon_hello", "daemon_draining"},
	LogLevels:        []string{"standard", "verbose", "trace"},
}

type healthResponse struct {
	Status           string `json:"status"`
	SchemaVersion    string `json:"schema_version"`
	DaemonInstanceID string `json:"daemon_instance_id"`
	Draining         bool   `json:"draining"`
	Capabilities     any    `json:"capabilities"`
}

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	draining := d.draining.Load()
	status := "ok"
	code := http.StatusOK
	if draining {
		status = "draining"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, healthResponse{
		Status:           status,
		SchemaVersion:    "belayer-log/v1",
		DaemonInstanceID: d.daemonInstanceID,
		Draining:         draining,
		Capabilities:     healthCapabilities,
	})
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
	LogLevel     string            `json:"LogLevel"`
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
		LogLevel:     s.LogLevel,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

type createSessionRequest struct {
	Name         string            `json:"name"`
	Template     string            `json:"template,omitempty"`
	Repos        map[string]string `json:"repos,omitempty"`
	WorkspaceDir string            `json:"workspace_dir,omitempty"`
	LogLevel     string            `json:"log_level,omitempty"`
}

func (d *Daemon) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	// Reject session creation during shutdown: a session created after DrainAll
	// snapshots the session list would never be archived.
	if d.draining.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon draining"})
		return
	}
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	logLevel, err := ResolveLogLevel(req.LogLevel, d.config.DefaultLogLevel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		LogLevel:     logLevel,
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
			// Drain bridges first so any final events land in the store before
			// the archiver snapshots it; archive before sandbox teardown so the
			// archiver reads session state before any teardown side-effects can
			// perturb the session row.
			d.stopAllBridgeAgents(id, "session status changed to "+req.Status)
			d.archiver.ArchiveTerminal(id)
			d.terminateSandbox(r.Context(), id)
		} else {
			// Non-terminal transition (e.g. reopening a previously terminated
			// session back to running). Clear any shutdown tombstone so fresh
			// spawns for this session are no longer rejected by the guard.
			d.bridgeMu.Lock()
			delete(d.bridgeShuttingDownSessions, id)
			d.bridgeMu.Unlock()
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
	afterID, waitFor, beforeID, limit, err := parseEventCursor(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	events, err := d.querySessionEvents(r.Context(), id, afterID, waitFor, beforeID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	d.writeEventHeaders(w, id, len(events))
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

	// Look up session to determine tier.
	sess, err := d.store.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	data := req.Data
	var frag trace.Fragment

	if sess.LogLevel == LogLevelTrace {
		// Pre-scrub every trace event payload regardless of size, so secrets
		// never reach disk (fragment files) or SQLite inline storage.
		data = Scrub(data)
	}

	if len(data) >= 65536 {
		switch sess.LogLevel {
		case LogLevelTrace:
			agentName := extractAgentName(data)
			if !isValidAgentName(agentName) {
				// Invalid agent name: do NOT spill (prevents path traversal).
				// Truncate with a structured sentinel preserving the original size.
				log.Printf("trace spill rejected for session %s: invalid agent name %q", id, agentName)
				data = fmt.Sprintf(
					`{"_truncated":true,"tier":"trace","original_size":%d,"reason":"invalid agent id"}`,
					len(req.Data),
				)
			} else if d.traceWriter != nil {
				f, werr := d.traceWriter.Append(id, agentName, []byte(data))
				if werr != nil {
					log.Printf("trace spill failed for session %s agent %s: %v; falling back to truncation", id, agentName, werr)
					data = fmt.Sprintf(
						`{"_truncated":true,"tier":"trace","original_size":%d,"reason":"trace writer error"}`,
						len(req.Data),
					)
				} else {
					frag = f
					data = fmt.Sprintf(`{"agent":%q,"_trace":true}`, agentName)
				}
			} else {
				data = fmt.Sprintf(
					`{"_truncated":true,"tier":"trace","original_size":%d,"reason":"trace writer error"}`,
					len(req.Data),
				)
			}
		default:
			// Non-trace tiers: scrub then truncate with structured sentinel.
			scrubbed := Scrub(req.Data)
			data = fmt.Sprintf(
				`{"_truncated":true,"tier":%q,"original_size":%d,"reason":"upgrade to trace tier to capture full payload"}`,
				sess.LogLevel, len(req.Data),
			)
			_ = scrubbed // scrubbed but not stored (truncated entirely)
		}
	}

	evt := store.SessionEvent{
		SessionID: id,
		Type:      req.Type,
		Data:      data,
	}
	if err := d.store.InsertEventWithSpill(evt, frag); err != nil {
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

// extractAgentName tries to parse an "agent" field from a JSON string.
// Returns "unknown" if parsing fails or the field is empty.
func extractAgentName(data string) string {
	var v struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(data), &v); err == nil && v.Agent != "" {
		return v.Agent
	}
	return "unknown"
}

// isValidAgentName returns true iff name is safe to use as a filesystem path
// component for trace fragment directories. Rules:
//   - Non-empty
//   - Length ≤ 64 characters
//   - Contains only [A-Za-z0-9_-] characters
//   - Not "." or ".."
//   - Does not contain '/' or '\'
func isValidAgentName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
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

	// Determine afterID. Precedence: Last-Event-ID header > ?after= param > default.
	var afterID int64
	lastEventIDHeader := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	afterParam := strings.TrimSpace(r.URL.Query().Get("after"))

	if lastEventIDHeader != "" {
		parsed, err := strconv.ParseInt(lastEventIDHeader, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Last-Event-ID must be a non-negative integer"})
			return
		}
		afterID = parsed
	} else if afterParam != "" {
		parsed, err := strconv.ParseInt(afterParam, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "after must be a non-negative integer"})
			return
		}
		afterID = parsed
	}

	// Parse the optional wait parameter.
	var waitFor time.Duration
	if waitRaw := strings.TrimSpace(r.URL.Query().Get("wait")); waitRaw != "" {
		parsed, err := time.ParseDuration(waitRaw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wait must be a valid non-negative duration"})
			return
		}
		waitFor = parsed
	}

	// Compute helloLastID: the global max event ID at connect time. This is
	// what daemon_hello advertises as the high-water mark so consumers can
	// persist it as their epoch cursor. It is computed independently of
	// afterID so both can be set correctly at once.
	helloLastID, err := d.store.MaxEventID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Default: afterID = 0 so the consumer receives the full backlog from the
	// beginning (LOG_FORMAT.md §4). Consumers wanting "stream from now" MUST
	// explicitly pass ?after=<helloLastID> (or Last-Event-ID) from daemon_hello.

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	// Register subscriber inside sseMu so it is totally ordered with respect
	// to announceDraining's snapshot. If draining is already true (Swap
	// happened in Shutdown Phase a before we took the lock), reject the
	// connection immediately so no subscriber is ever registered without
	// receiving a daemon_draining frame.
	sub := &sseSubscriber{drain: make(chan struct{})}
	d.sseMu.Lock()
	if d.draining.Load() {
		d.sseMu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon draining"})
		return
	}
	d.sseSubscribers[sub] = struct{}{}
	d.sseMu.Unlock()
	defer func() {
		d.sseMu.Lock()
		delete(d.sseSubscribers, sub)
		d.sseMu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Write event headers for the first session listed (best-effort; SSE streams
	// may span multiple sessions so X-Event-Count is set to 0 here and updated
	// per-frame by the consumer if needed). Headers must be set before the first
	// Write/Flush call that commits the status code.
	if len(filtered) > 0 {
		d.writeEventHeaders(w, filtered[0], 0)
	}

	// Emit daemon_hello as the FIRST frame. No id: line (control frame invariant).
	helloData, _ := json.Marshal(map[string]any{
		"daemon_instance_id": d.daemonInstanceID,
		"schema_version":     "belayer-log/v1",
		"last_id":            helloLastID,
	})
	fmt.Fprintf(w, "event: daemon_hello\ndata: %s\n\n", helloData)
	flusher.Flush()

	enc := json.NewEncoder(w)
	lastID := afterID
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	keepalive := time.NewTicker(d.sseKeepaliveInterval)
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
				// Domain frames carry id: lines (spec §4 A8).
				// emit event: <type> per LOG_FORMAT.md §4 (A2). Sanitize the type
				// so malformed event types inserted via POST /sessions/{id}/events
				// cannot inject additional SSE frames by embedding newlines or null bytes.
				evtType := sanitizeSSEEventType(evt.Type)
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: ", evt.ID, evtType)
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
		case <-sub.drain:
			// Shutdown has begun. Emit daemon_draining (no id: line) and exit.
			totalGraceMS := int64((d.shutdownHTTPTimeout + d.archiveDrainTimeout) / time.Millisecond)
			drainingData, _ := json.Marshal(map[string]any{
				"reason":     "shutdown",
				"timeout_ms": totalGraceMS,
			})
			fmt.Fprintf(w, "event: daemon_draining\ndata: %s\n\n", drainingData)
			flusher.Flush()
			return
		case <-ticker.C:
		case <-keepalive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// parseEventCursor parses ?after=, ?wait=, ?before=, and ?limit= query params
// from a GET /sessions/{id}/events request.
// Returns afterID, waitFor, beforeID, limit (raw, not capped) and any parse error.
// Negative values for after, before, and limit are rejected with an error.
func parseEventCursor(r *http.Request) (afterID int64, waitFor time.Duration, beforeID int64, limit int, err error) {
	afterRaw := strings.TrimSpace(r.URL.Query().Get("after"))
	waitRaw := strings.TrimSpace(r.URL.Query().Get("wait"))
	beforeRaw := strings.TrimSpace(r.URL.Query().Get("before"))
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))

	if afterRaw != "" {
		parsed, parseErr := strconv.ParseInt(afterRaw, 10, 64)
		if parseErr != nil || parsed < 0 {
			return 0, 0, 0, 0, fmt.Errorf("after must be a non-negative integer")
		}
		afterID = parsed
	}

	if waitRaw != "" {
		parsed, parseErr := time.ParseDuration(waitRaw)
		if parseErr != nil || parsed < 0 {
			return 0, 0, 0, 0, fmt.Errorf("wait must be a valid non-negative duration")
		}
		waitFor = parsed
	}

	if beforeRaw != "" {
		parsed, parseErr := strconv.ParseInt(beforeRaw, 10, 64)
		if parseErr != nil || parsed < 0 {
			return 0, 0, 0, 0, fmt.Errorf("before must be a non-negative integer")
		}
		beforeID = parsed
	}

	if limitRaw != "" {
		parsed, parseErr := strconv.ParseInt(limitRaw, 10, 64)
		if parseErr != nil || parsed < 0 {
			return 0, 0, 0, 0, fmt.Errorf("limit must be a non-negative integer")
		}
		limit = int(parsed)
	}

	return afterID, waitFor, beforeID, limit, nil
}

// querySessionEvents fetches events for sessionID with optional windowing
// (afterID, beforeID, limit) and optional long-poll (waitFor). The waitFor
// behavior uses only the afterID lower bound for backwards-compat — window
// params don't change wait semantics.
func (d *Daemon) querySessionEvents(ctx context.Context, sessionID string, afterID int64, waitFor time.Duration, beforeID int64, limit int) ([]store.SessionEvent, error) {
	if waitFor <= 0 {
		return d.store.QueryEventsWindow(sessionID, afterID, beforeID, limit)
	}

	// Long-poll: wait until at least one event arrives after afterID.
	// beforeID/limit apply to each poll result.
	deadline := time.Now().Add(waitFor)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		events, err := d.store.QueryEventsWindow(sessionID, afterID, beforeID, limit)
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

// archiveDir returns the on-disk archive directory for a session, or a non-nil
// error response that has already been written to w if something is wrong.
// Callers should return immediately on a non-empty errorSent=true.
//
// Resolution rules (in order):
//  1. Session not found → 404 {"error":"session not found"}
//  2. Session has no workspace → 404 {"error":"no archive (session has no workspace)"}
//  3. Archive dir missing → 404 {"error":"no archive"}
func (d *Daemon) resolveArchiveDir(w http.ResponseWriter, r *http.Request) (dir string, ok bool) {
	id := r.PathValue("id")
	sess, err := d.store.GetSession(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return "", false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return "", false
	}
	if sess.WorkspaceDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no archive (session has no workspace)"})
		return "", false
	}
	archDir := filepath.Join(sess.WorkspaceDir, ".belayer", "archive", id)
	if _, err := os.Stat(archDir); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no archive"})
		return "", false
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return "", false
	}
	return archDir, true
}

// serveArchiveFile streams a single archive file to w with the given content type.
// Returns false and writes an error response if the file is missing or unreadable.
func serveArchiveFile(w http.ResponseWriter, path, contentType string) bool {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no archive"})
			return false
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return false
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
	return true
}

// handleArchiveNDJSON serves GET /sessions/{id}/archive.ndjson.
// Streams <workspace>/.belayer/archive/<id>/events.ndjson with no in-memory buffering.
func (d *Daemon) handleArchiveNDJSON(w http.ResponseWriter, r *http.Request) {
	archDir, ok := d.resolveArchiveDir(w, r)
	if !ok {
		return
	}
	serveArchiveFile(w, filepath.Join(archDir, "events.ndjson"), "application/x-ndjson")
}

// handleArchiveManifest serves GET /sessions/{id}/archive/manifest.json.
func (d *Daemon) handleArchiveManifest(w http.ResponseWriter, r *http.Request) {
	archDir, ok := d.resolveArchiveDir(w, r)
	if !ok {
		return
	}
	serveArchiveFile(w, filepath.Join(archDir, "manifest.json"), "application/json")
}

// handleArchiveTarGz serves GET /sessions/{id}/archive.tar.gz.
// Streams a gzip-compressed tar containing events.ndjson + manifest.json directly
// to w without buffering either file in memory.
func (d *Daemon) handleArchiveTarGz(w http.ResponseWriter, r *http.Request) {
	archDir, ok := d.resolveArchiveDir(w, r)
	if !ok {
		return
	}

	// Verify both files exist before touching response headers.
	ndjsonPath := filepath.Join(archDir, "events.ndjson")
	manifestPath := filepath.Join(archDir, "manifest.json")
	for _, p := range []string{ndjsonPath, manifestPath} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no archive"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	sessionID := r.PathValue("id")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, sessionID))
	w.WriteHeader(http.StatusOK)

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for _, entry := range []struct {
		name string
		path string
	}{
		{"events.ndjson", ndjsonPath},
		{"manifest.json", manifestPath},
	} {
		fi, err := os.Stat(entry.path)
		if err != nil {
			// Headers already sent; can't send a JSON error — just stop.
			log.Printf("archive tar.gz: stat %s: %v", entry.path, err)
			return
		}
		hdr := &tar.Header{
			Name:    entry.name,
			Mode:    0o644,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			log.Printf("archive tar.gz: write header %s: %v", entry.name, err)
			return
		}
		f, err := os.Open(entry.path)
		if err != nil {
			log.Printf("archive tar.gz: open %s: %v", entry.path, err)
			return
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			log.Printf("archive tar.gz: copy %s: %v", entry.name, err)
			return
		}
		f.Close()
	}
	_ = tw.Close()
	_ = gw.Close()
}

func (d *Daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	preds, err := parseSearchQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	events, err := d.store.SearchEventsV1(ctx, preds)
	if err != nil {
		status, msg := classifySearchError(err)
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	d.writeEventHeaders(w, preds.SessionID, len(events))
	writeJSON(w, http.StatusOK, events)
}

const searchQLenCap = 4096

// parseSearchQuery extracts and validates SearchPredicates from r.URL.Query().
// Returns an error if validation fails (caller emits HTTP 400).
func parseSearchQuery(r *http.Request) (store.SearchPredicates, error) {
	q := r.URL.Query()
	noParams := r.URL.RawQuery == ""

	var p store.SearchPredicates
	p.DescOrder = noParams // no-params: return most-recent DESC
	p.Limit = 1000

	raw := strings.TrimSpace(q.Get("q"))
	if len(raw) > searchQLenCap {
		return p, fmt.Errorf("q too long: max %d bytes", searchQLenCap)
	}
	p.Q = raw

	p.SessionID = q.Get("session")
	p.TypePrefix = q.Get("type_prefix")
	p.Agent = q.Get("agent")

	if afterStr := q.Get("after"); afterStr != "" {
		v, err := strconv.ParseInt(afterStr, 10, 64)
		if err != nil || v < 0 {
			return p, fmt.Errorf("after must be a non-negative integer")
		}
		p.AfterID = v
	}

	if beforeStr := q.Get("before"); beforeStr != "" {
		v, err := strconv.ParseInt(beforeStr, 10, 64)
		if err != nil || v < 0 {
			return p, fmt.Errorf("before must be a non-negative integer")
		}
		p.BeforeID = v
	}

	return p, nil
}

// classifySearchError maps a store error to an HTTP status + operator-clean
// message. Never leaks raw SQL or stack frames.
func classifySearchError(err error) (int, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "search timed out (limit 2s)"
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "fts5: syntax error"),
		strings.Contains(lower, "malformed match expression"),
		strings.Contains(lower, "no such column"),
		strings.Contains(lower, "unknown special query"),
		strings.Contains(lower, "unterminated string"),
		strings.Contains(lower, "unmatched parenthesis"),
		strings.Contains(lower, "fts5: parse error"):
		return http.StatusBadRequest, "invalid q: " + sanitizeFTS5Error(msg)
	}
	log.Printf("search: unexpected error: %v", err)
	return http.StatusInternalServerError, "search failed"
}

// sanitizeFTS5Error strips SQL-driver prefixes so the consumer sees a clean
// human message.
func sanitizeFTS5Error(msg string) string {
	for _, prefix := range []string{"SQL logic error: ", "SQL logic error (SQLITE_ERROR): "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
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

// sanitizeSSEEventType replaces any characters that would break SSE frame
// parsing (\n, \r, \x00) with '_'. The event type is user-controlled (anything
// posted via POST /sessions/{id}/events), so we must guard against SSE frame
// injection before emitting the event: line.
func sanitizeSSEEventType(t string) string {
	if !strings.ContainsAny(t, "\n\r\x00") {
		return t
	}
	var b strings.Builder
	b.Grow(len(t))
	for _, r := range t {
		if r == '\n' || r == '\r' || r == 0 {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// bridgeKey returns the map key for a bridge process given session and agent name.
func bridgeKey(sessionID, agentName string) string {
	return sessionID + "/" + agentName
}

// parseBridgeKey splits a bridgeKey back into (sessionID, agentName). Inverse
// of bridgeKey. Returns empty strings if key is malformed.
func parseBridgeKey(key string) (string, string) {
	idx := strings.Index(key, "/")
	if idx < 0 {
		return "", ""
	}
	return key[:idx], key[idx+1:]
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
