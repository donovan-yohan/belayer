package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/daemon/bridgelog"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
)

type agentSpawnRequest struct {
	SessionID string `json:"-"`
	Name      string `json:"name"`
	// Identity selects the directory under .belayer/agents/<identity>/ used to
	// load the agent's system prompt and belayer_tools allowlist. When empty it
	// defaults to Name, preserving the single-instance-per-identity convention
	// (e.g. "supervisor", "pm"). Set explicitly when spawning multiple agents
	// off the same identity (e.g. Name="reviewer-1", Identity="reviewer").
	Identity        string `json:"identity,omitempty"`
	Role            string `json:"role"`
	Profile         string `json:"profile"` // Hermes runtime profile (BELAYER_PROFILE / HERMES_HOME), independent of identity
	Model           string `json:"model,omitempty"`
	Message         string `json:"message,omitempty"`
	Repo            string `json:"repo,omitempty"`
	Workdir         string `json:"workdir,omitempty"`
	Branch          string `json:"branch,omitempty"` // if set, agent works in a git worktree on this branch
	HermesSessionID string `json:"hermes_session_id,omitempty"`
	Ephemeral       *bool  `json:"ephemeral,omitempty"` // nil = default (true for specialists, false for supervisor)
}

// identityKey returns the identity directory name to use for this spawn.
// Falls back to Name when Identity is unset, preserving the original
// single-instance-per-identity behavior.
func (r agentSpawnRequest) identityKey() string {
	if r.Identity != "" {
		return r.Identity
	}
	return r.Name
}

type finishAgentRequest struct {
	Summary      string `json:"summary"`
	Blocked      bool   `json:"blocked"`
	SpecArtifact string `json:"spec_artifact,omitempty"`
}

func (d *Daemon) handleSpawnAgent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	var req agentSpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" || req.Profile == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and profile are required"})
		return
	}
	if err := validateAgentName(req.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Identity is used to pick the identity directory under .belayer/agents/<identity>/
	// and agents/<identity>/; an unvalidated value with "../" would escape the
	// identity tree when resolved by agentIdentityPaths at spawn time.
	if req.Identity != "" {
		if err := validateAgentName(req.Identity); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identity: " + err.Error()})
			return
		}
	}
	req.SessionID = sessionID

	// Resolve workdir from session repos when repo scope is set but workdir is not.
	if req.Workdir == "" && req.Repo != "" {
		var repos map[string]string
		if err := json.Unmarshal([]byte(sess.Repos), &repos); err == nil {
			if path, ok := repos[req.Repo]; ok {
				req.Workdir = path
			} else {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("repo %q not found in session repos (available: %v)", req.Repo, repoNames(repos)),
				})
				return
			}
		}
	}

	// Fall back to session workspace dir if workdir still empty.
	if req.Workdir == "" && sess.WorkspaceDir != "" {
		req.Workdir = sess.WorkspaceDir
	}

	// Check for a prior run of this agent in this session.
	// If it exists, carry over its Hermes session ID for resume and update
	// the existing row instead of creating a new one (UNIQUE constraint).
	prior, priorErr := d.store.GetAgentRun(sessionID, req.Name)
	resuming := priorErr == nil && prior.HermesSessionID != ""

	if resuming && req.HermesSessionID == "" {
		req.HermesSessionID = prior.HermesSessionID
		log.Printf("Resuming agent %s with hermes session %s", req.Name, req.HermesSessionID)
	}

	// If a branch is requested, check that worktrees aren't disabled for this project.
	if req.Branch != "" {
		if worktreesDisabled(req.Workdir) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "worktrees are disabled for this project (see .belayer/config.yaml). Remove the branch parameter or enable worktrees.",
			})
			return
		}
	}

	if priorErr == nil {
		// Prior run exists — update its status back to starting.
		// Carry over branch/worktree path from prior run if not re-specified.
		if req.Branch == "" && prior.Branch != "" {
			req.Branch = prior.Branch
		}
		if err := d.store.UpdateAgentRunStatus(sessionID, req.Name, "starting"); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		// No prior run — create a new row.
		run := store.AgentRun{
			SessionID:       sessionID,
			Name:            req.Name,
			Role:            req.Role,
			Profile:         req.Profile,
			RepoScope:       req.Repo,
			Workdir:         req.Workdir,
			Branch:          req.Branch,
			HermesSessionID: req.HermesSessionID,
			Transport:       "bridge",
			Status:          "starting",
		}
		if _, err := d.store.CreateAgentRun(run); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	proc, err := d.spawnBridgeAgent(req)
	if err != nil {
		log.Printf("spawn agent %s failed: %v", req.Name, err)
		_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Track the process (may be nil for test stubs).
	if proc != nil {
		d.bridgeMu.Lock()
		if d.bridgeShuttingDownSessions[sessionID] {
			d.bridgeMu.Unlock()
			_ = proc.Stop(2 * time.Second)
			// Move the run out of "starting" so operators see the cancel, not a
			// stuck starting row that will never transition.
			_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "daemon shutting down; bridge spawn cancelled"})
			return
		}
		d.bridgeProcs[bridgeKey(sessionID, req.Name)] = proc
		d.bridgeMu.Unlock()

		// Wait up to 500ms for the first bridge event (proves healthy startup)
		// or process exit (crash during startup). This converts a silent race
		// where the bridge exits in <500ms into an immediate error returned to
		// the supervisor tool call (Gap 14).
		workdirForRunDir := req.Workdir
		if workdirForRunDir == "" {
			if cwd, cwdErr := os.Getwd(); cwdErr == nil {
				workdirForRunDir = cwd
			}
		}
		runDir := filepath.Join(workdirForRunDir, ".belayer", "runs", sessionID, req.Name)
		select {
		case <-proc.FirstEvent():
			// healthy startup — proceed
		case <-proc.Done():
			// bridge exited before posting any event — report failure with stderr tail
			_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
			d.bridgeMu.Lock()
			delete(d.bridgeProcs, bridgeKey(sessionID, req.Name))
			d.bridgeMu.Unlock()
			stderrPath := filepath.Join(runDir, "bridge-stderr.log")
			tail := bridgelog.TailLines(stderrPath, 20)
			errMsg := fmt.Sprintf("bridge exited during spawn (%v): %s", proc.ExitErr(), tail)
			log.Printf("spawn agent %s failed during startup: %v", req.Name, errMsg)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": errMsg})
			return
		case <-time.After(500 * time.Millisecond):
			// Slow start — assume the bridge is still initialising.
			// watchBridgeExit will catch any later crash.
		}
	}

	if err := d.store.UpdateAgentRunStatus(sessionID, req.Name, "running"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stored, err := d.store.GetAgentRun(sessionID, req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// No broker subscription for bridge agents — they pull messages via HTTP.
	// Daemon writes stdin for interrupts directly.

	// Watch for unexpected exit (skipped when proc is nil, e.g. in tests).
	if proc != nil {
		d.watchBridgeExit(stored, proc)
	}

	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "agent_spawned",
		Data: mustJSON(map[string]string{
			"agent":     req.Name,
			"role":      req.Role,
			"profile":   req.Profile,
			"transport": "bridge",
		}),
	})
	writeJSON(w, http.StatusCreated, stored)
}

func (d *Daemon) handleListAgents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (d *Daemon) handleFinishAgent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	name := r.PathValue("name")
	if _, err := d.store.GetAgentRun(sessionID, name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	var req finishAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// When the supervisor calls finish (and isn't blocked), trigger PM verification
	// instead of marking the session complete immediately.
	if name == "supervisor" && !req.Blocked {
		if err := d.store.UpdateAgentRunStatus(sessionID, name, "pending_verification"); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_ = d.store.LogEvent(store.SessionEvent{
			SessionID: sessionID,
			Type:      "agent_finished",
			Data: mustJSON(map[string]string{
				"agent":   name,
				"status":  "pending_verification",
				"summary": req.Summary,
			}),
		})
		d.handleBridgeCompletionRequested(sessionID, name, map[string]any{
			"agent":        name,
			"summary":      req.Summary,
			"spec_artifact": req.SpecArtifact,
		})
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "pending_verification",
			"message": "PM agent spawned for spec verification",
		})
		return
	}

	status := "complete"
	if req.Blocked {
		status = "blocked"
	}
	if err := d.store.UpdateAgentRunStatus(sessionID, name, status); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	run, err := d.store.GetAgentRun(sessionID, name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "agent_finished",
		Data: mustJSON(map[string]string{
			"agent":   name,
			"status":  status,
			"summary": req.Summary,
		}),
	})
	if name != "supervisor" {
		content := fmt.Sprintf("%s marked work as %s. Summary: %s", name, status, req.Summary)
		msg := broker.Message{SessionID: sessionID, SenderID: name, RecipientID: "supervisor", Type: broker.MessageStateChange, Content: content, Timestamp: time.Now().UTC(), Urgent: req.Blocked}
		if req.Blocked {
			_ = d.broker.Interrupt(sessionID, "supervisor", msg)
		} else {
			_ = d.broker.Send(sessionID, "supervisor", msg)
		}
	}
	writeJSON(w, http.StatusOK, run)
}

func (d *Daemon) bridgeLaunchAgent(req agentSpawnRequest) (*bridge.Process, error) {
	workdir := req.Workdir
	if workdir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determine workdir: %w", err)
		}
		workdir = cwd
	}

	// Remember the pre-worktree workdir so the BelayerRoot fallback below can
	// find an extracted hermes_bridge in the main checkout — worktrees omit
	// .belayer/ (it's gitignored), so probing only the worktree path would
	// miss it for branch-based specialists.
	repoWorkdir := workdir

	// If a branch is specified, create (or reuse) a git worktree for isolation.
	worktreePath := ""
	if req.Branch != "" {
		wtPath, err := ensureWorktree(workdir, req.SessionID, req.Name, req.Branch)
		if err != nil {
			return nil, fmt.Errorf("create worktree: %w", err)
		}
		worktreePath = wtPath
		// Update the stored record with the resolved worktree path.
		_ = d.store.UpdateAgentRunWorktree(req.SessionID, req.Name, req.Branch, wtPath)
		// Agent works in the worktree, not the main repo.
		workdir = wtPath
	}

	runDir := filepath.Join(workdir, ".belayer", "runs", req.SessionID, req.Name)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	// Compute Landlock write roots for this agent (used when ConfineAgentWrites is true).
	writeRoots := computeWriteRoots(req, repoWorkdir, worktreePath, runDir, d.config.AgentSharedWritePaths)

	// Resolve ephemeral flag: explicit request > role-based default.
	// Supervisors stay alive by default; all other roles exit on task completion.
	ephemeral := true
	if req.Ephemeral != nil {
		ephemeral = *req.Ephemeral
	} else if req.Role == "supervisor" {
		ephemeral = false
	}

	// Load system prompt from agent identity dir if it exists. Project-local
	// overrides under <workdir>/.belayer/agents/<identity>/ win over the shipped
	// defaults at <BelayerRoot>/agents/<identity>/. Identity defaults to Name
	// when not explicitly set — see agentSpawnRequest.identityKey for details.
	identity := req.identityKey()
	var systemPrompt string
	for _, candidate := range agentIdentityPaths(workdir, d.config.BelayerRoot, identity, "system-prompt.md") {
		if data, err := os.ReadFile(candidate); err == nil {
			systemPrompt = string(data)
			log.Printf("Loaded system prompt from %s for agent %s (identity=%s)", candidate, req.Name, identity)
			break
		}
	}

	// Load belayer_tools and model from <agent-dir>/agent.yaml if it exists. Same
	// project-local-over-shipped resolution as the system prompt.
	var belayerTools []string
	agentModel := req.Model // explicit spawn request takes precedence
	for _, yamlPath := range agentIdentityPaths(workdir, d.config.BelayerRoot, identity, "agent.yaml") {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		// Simple line-based parse — avoid YAML library dependency.
		// Looks for "belayer_tools:" then collects "  - tool_name" lines.
		// Also reads "model: <value>" for the default model.
		inTools := false
		for _, line := range splitLines(string(data)) {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "model:") && agentModel == "" {
				agentModel = strings.TrimSpace(strings.TrimPrefix(trimmed, "model:"))
				inTools = false
				continue
			}
			if trimmed == "belayer_tools:" || trimmed == "belayer_tools: []" {
				inTools = true
				if trimmed == "belayer_tools: []" {
					break // explicit empty list
				}
				continue
			}
			if inTools {
				if strings.HasPrefix(trimmed, "- ") {
					tool := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
					belayerTools = append(belayerTools, tool)
				} else {
					break // end of list
				}
			}
		}
		log.Printf("Loaded agent.yaml from %s for agent %s (identity=%s): model=%q tools=%v", yamlPath, req.Name, identity, agentModel, belayerTools)
		break
	}

	// Set up stdin pipe for daemon→agent communication.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	// Drain any still-live bridge process for this (session, agent) before
	// rotating its log files. bridgelog.Rotate is unsafe while a writer holds
	// the file open — the kernel will keep writing the renamed inode, so
	// post-rotate bytes would land in .log.1 instead of the new .log.
	if existing := d.takeExistingBridge(req.SessionID, req.Name); existing != nil {
		if err := existing.Stop(5 * time.Second); err != nil {
			log.Printf("spawn %s: stop prior bridge: %v (continuing)", req.Name, err)
		}
		// Bound the drain: if Stop failed to reach the process (e.g. Kill
		// returned an error and Done() never closes), we still need to
		// rotate its logs. The risk of a few bytes leaking into .log.1 is
		// strictly smaller than hanging the respawn forever.
		select {
		case <-existing.Done():
		case <-time.After(5 * time.Second):
			log.Printf("spawn %s: prior bridge did not drain within 5s, proceeding with rotation", req.Name)
		}
	}

	// Rotate and open per-spawn stdout/stderr logs (keeps last 3 spawns).
	stdoutLog, err := bridgelog.RotateAndOpen(filepath.Join(runDir, "bridge-stdout.log"), 3)
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		return nil, fmt.Errorf("open stdout log: %w", err)
	}
	stderrLog, err := bridgelog.RotateAndOpen(filepath.Join(runDir, "bridge-stderr.log"), 3)
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutLog.Close()
		return nil, fmt.Errorf("open stderr log: %w", err)
	}

	// Get or create sandbox handle for the session. Must happen before building
	// bridge.Config so we can select the correct socket path (Unix vs TCP).
	sess, err := d.store.GetSession(req.SessionID)
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutLog.Close()
		stderrLog.Close()
		return nil, fmt.Errorf("load session for sandbox handle: %w", err)
	}
	ss, err := d.ensureSandboxHandle(d.startCtx, sess)
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutLog.Close()
		stderrLog.Close()
		return nil, fmt.Errorf("ensure sandbox handle: %w", err)
	}

	// Allocate per-agent transcript path for verbose sessions. Anchored to
	// sess.WorkspaceDir (not the agent's workdir, which may be a per-branch
	// worktree) so the archive manager's single read path under
	// sess.WorkspaceDir/.belayer/runs/<session>/transcripts/ finds every
	// agent's file, including branch-based specialists. If sess.WorkspaceDir
	// is empty, the session is never archived anyway (archive_manager.doArchive
	// skips), so there's no point writing transcripts.
	var transcriptPath string
	// Tier model is monotonic (standard ⊂ verbose ⊂ trace): trace sessions
	// still want the verbose transcript channel in addition to trace fragments.
	if (sess.LogLevel == LogLevelVerbose || sess.LogLevel == LogLevelTrace) && sess.WorkspaceDir != "" {
		transcriptsDir := filepath.Join(sess.WorkspaceDir, ".belayer", "runs", req.SessionID, "transcripts")
		if mkErr := os.MkdirAll(transcriptsDir, 0o700); mkErr != nil {
			log.Printf("spawn %s: create transcripts dir failed (continuing without transcript): %v", req.Name, mkErr)
		} else {
			transcriptPath = filepath.Join(transcriptsDir, req.Name+".jsonl")
		}
	}

	socketPath := bridgeSocketPath(ss.mode, d.config.SocketPath, d.config.DockerHostGateway, d.tcpPort, d.config.WorkspaceSockPath)
	log.Printf("spawn %s: mode=%q socketPath=%q tcpPort=%d gateway=%q", req.Name, ss.mode, socketPath, d.tcpPort, d.config.DockerHostGateway)
	cfg := bridge.Config{
		SessionID:       req.SessionID,
		AgentID:         req.Name,
		Role:            req.Role,
		Profile:         req.Profile,
		Model:           agentModel,
		APIKey:          d.config.BridgeAPIKey,
		BaseURL:         d.config.BridgeBaseURL,
		Provider:        d.config.BridgeProvider,
		Message:         req.Message,
		SystemPrompt:    systemPrompt,
		HermesSessionID: req.HermesSessionID,
		Ephemeral:       ephemeral,
		Workdir:         workdir,
		SocketPath:      socketPath,
		RunDir:          runDir,
		// BelayerRoot on bridge.Config is the parent directory placed on PYTHONPATH
		// so that `python -m hermes_bridge` resolves correctly. We prefer the
		// daemon's resolved RuntimeDir (extracted at daemon startup outside the
		// workspace) so that workspace agents running `rm -rf .belayer/` cannot
		// destroy the module required for spawning peer bridges.
		BelayerRoot:     d.config.RuntimeDir,
		BelayerTools:    belayerTools,
		TranscriptPath:  transcriptPath,
		LogLevel:        sess.LogLevel,
		WriteRoots:      writeRoots,
		ConfineWrites:   d.config.ConfineAgentWrites,
	}
	// In clamshell mode the bridge runs inside the Docker container where the
	// host hermes venv path doesn't exist; use the container's system python3.
	// Also inject proxy vars so LLM API calls route through the egress broker.
	// BelayerRoot is overridden to the container's view of the runtime dir
	// bind-mounted into the container so `python3 -m hermes_bridge` resolves.
	if ss.mode == "clamshell" {
		cfg.Cmd = []string{"python3", "-m", "hermes_bridge"}
		cfg.HTTPProxy = "http://proxy.internal:3128"
		cfg.BelayerRoot = "/opt/belayer/runtime"
		// The bridge runs inside the container where the host workspace is
		// mounted at /workspace. Translate transcriptPath (built from the host
		// path) so the bridge can actually open it; otherwise transcript capture
		// silently fails for verbose clamshell runs.
		if cfg.TranscriptPath != "" && sess.WorkspaceDir != "" {
			cfg.TranscriptPath = translateHostPathToContainer(cfg.TranscriptPath, sess.WorkspaceDir)
		}
	}
	// Universal fallback: when RuntimeDir was not set (e.g. tests that skip the
	// CLI wiring path) AND BELAYER_RUNTIME_DIR is unset, fall back to legacy
	// workspace location first, then workdir-based probe. The legacy path also
	// emits a deprecation warning (handled by extractBridgeToRuntimeDir in normal
	// operation; here we probe without warning since this is the test / no-daemon
	// path).
	if cfg.BelayerRoot == "" {
		for _, base := range []string{workdir, repoWorkdir} {
			if base == "" {
				continue
			}
			candidate := filepath.Join(base, ".belayer")
			if _, err := os.Stat(filepath.Join(candidate, "hermes_bridge", "__main__.py")); err == nil {
				cfg.BelayerRoot = candidate
				break
			}
		}
	}
	_ = worktreePath // stored in DB; cleanup handled separately

	// Build command and environment using bridge pure functions.
	argv := bridge.BuildCmd(cfg)
	env := bridge.BuildEnv(cfg)

	osProc, err := ss.driver.Exec(d.startCtx, ss.handle, argv, sandbox.ExecOpts{
		Env:    env,
		Dir:    workdir,
		Stdin:  stdinR,
		Stdout: io.MultiWriter(os.Stdout, stdoutLog),
		Stderr: io.MultiWriter(os.Stderr, stderrLog),
	})
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutLog.Close()
		stderrLog.Close()
		return nil, fmt.Errorf("sandbox exec: %w", err)
	}

	// Close read end — it's been inherited by the child process.
	stdinR.Close()

	proc := bridge.NewProcess(osProc, stdinW)

	// Close log files and stdin writer when process exits. stdinW is our
	// handle for sending interrupts; once the process is gone it's a pipe to
	// nowhere, so closing it avoids leaking file descriptors.
	go func() {
		<-proc.Done()
		stdinW.Close()
		stdoutLog.Close()
		stderrLog.Close()
	}()

	return proc, nil
}

func (d *Daemon) watchBridgeExit(run store.AgentRun, proc *bridge.Process) {
	go func() {
		<-proc.Done()

		// takeExistingBridge can replace this process with a fresh spawn
		// before its watcher fires; that old watcher must not mark the new
		// run blocked or delete the new process entry. Verify we're still
		// the current entry under lock, and take the cleanup-slot while
		// we hold it so the later delete race cannot touch a replacement.
		key := bridgeKey(run.SessionID, run.Name)
		d.bridgeMu.Lock()
		if d.bridgeProcs[key] != proc {
			d.bridgeMu.Unlock()
			return
		}
		delete(d.bridgeProcs, key)
		d.bridgeMu.Unlock()

		current, err := d.store.GetAgentRun(run.SessionID, run.Name)
		if err != nil {
			return
		}
		// If already marked complete/blocked by the bridge itself (via bridge:finished event), skip.
		if current.Status != "running" {
			return
		}

		// Bridge exited without sending bridge:finished — mark as blocked.
		exitErr := proc.ExitErr()
		_ = d.store.UpdateAgentRunStatus(run.SessionID, run.Name, "blocked")
		_ = d.store.LogEvent(store.SessionEvent{
			SessionID: run.SessionID,
			Type:      "agent_exited_without_finish",
			Data: mustJSON(map[string]string{
				"agent":    run.Name,
				"status":   "blocked",
				"exit_err": fmt.Sprintf("%v", exitErr),
			}),
		})

		// Notify supervisor.
		if run.Name != "supervisor" {
			// Reconstruct the run directory for this agent. If the agent was
			// spawned on a branch it has a worktree; otherwise it uses Workdir.
			runBase := run.Workdir
			if run.WorktreePath != "" {
				runBase = run.WorktreePath
			}
			stderrPath := filepath.Join(runBase, ".belayer", "runs", run.SessionID, run.Name, "bridge-stderr.log")
			stderrTail := bridgelog.TailLines(stderrPath, 50)
			content := fmt.Sprintf("%s bridge exited unexpectedly (exit_err: %v). Marked blocked.\n\nLast 50 lines of bridge-stderr.log:\n%s",
				run.Name, exitErr, stderrTail)
			msg := broker.Message{
				SessionID:   run.SessionID,
				SenderID:    run.Name,
				RecipientID: "supervisor",
				Type:        broker.MessageStateChange,
				Content:     content,
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			_ = d.broker.Interrupt(run.SessionID, "supervisor", msg)
		}

		// Check if this was the last active agent (session may be stalled).
		d.checkSessionStalled(run.SessionID)
	}()
}

// interruptBridgeAgent sends an interrupt command to a bridge agent's stdin.
func (d *Daemon) interruptBridgeAgent(sessionID, agentName, from, content string) error {
	d.bridgeMu.RLock()
	proc := d.bridgeProcs[bridgeKey(sessionID, agentName)]
	d.bridgeMu.RUnlock()

	if proc == nil {
		return fmt.Errorf("no bridge process for %s/%s", sessionID, agentName)
	}
	return proc.Interrupt(from, content)
}

// takeExistingBridge atomically removes and returns any bridge process
// tracked for (sessionID, agentName). Returns nil if no process is tracked.
// Caller is responsible for stopping/waiting on the returned process; the
// map entry is gone either way, so the replacement spawn can proceed.
func (d *Daemon) takeExistingBridge(sessionID, agentName string) *bridge.Process {
	key := bridgeKey(sessionID, agentName)
	d.bridgeMu.Lock()
	proc := d.bridgeProcs[key]
	if proc != nil {
		delete(d.bridgeProcs, key)
	}
	d.bridgeMu.Unlock()
	return proc
}

// spawnAgentInternal handles the core agent spawn logic without HTTP.
// Used by event handlers (e.g. PM auto-spawn) that need to spawn agents
// programmatically rather than via the REST API.
func (d *Daemon) spawnAgentInternal(req agentSpawnRequest) (store.AgentRun, error) {
	sess, err := d.store.GetSession(req.SessionID)
	if err != nil {
		return store.AgentRun{}, fmt.Errorf("session not found: %w", err)
	}

	// Resolve workdir from session.
	if req.Workdir == "" && req.Repo != "" {
		var repos map[string]string
		if err := json.Unmarshal([]byte(sess.Repos), &repos); err == nil {
			if path, ok := repos[req.Repo]; ok {
				req.Workdir = path
			}
		}
	}
	if req.Workdir == "" && sess.WorkspaceDir != "" {
		req.Workdir = sess.WorkspaceDir
	}

	// Check for prior run (resume support).
	prior, priorErr := d.store.GetAgentRun(req.SessionID, req.Name)
	if priorErr == nil && prior.HermesSessionID != "" && req.HermesSessionID == "" {
		req.HermesSessionID = prior.HermesSessionID
		log.Printf("Resuming agent %s with hermes session %s", req.Name, req.HermesSessionID)
	}

	if priorErr == nil {
		if req.Branch == "" && prior.Branch != "" {
			req.Branch = prior.Branch
		}
		if err := d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "starting"); err != nil {
			return store.AgentRun{}, fmt.Errorf("update agent status: %w", err)
		}
	} else {
		run := store.AgentRun{
			SessionID:       req.SessionID,
			Name:            req.Name,
			Role:            req.Role,
			Profile:         req.Profile,
			RepoScope:       req.Repo,
			Workdir:         req.Workdir,
			Branch:          req.Branch,
			HermesSessionID: req.HermesSessionID,
			Transport:       "bridge",
			Status:          "starting",
		}
		if _, err := d.store.CreateAgentRun(run); err != nil {
			return store.AgentRun{}, fmt.Errorf("create agent run: %w", err)
		}
	}

	proc, err := d.spawnBridgeAgent(req)
	if err != nil {
		_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
		return store.AgentRun{}, fmt.Errorf("spawn bridge: %w", err)
	}

	if proc != nil {
		d.bridgeMu.Lock()
		if d.bridgeShuttingDownSessions[req.SessionID] {
			d.bridgeMu.Unlock()
			_ = proc.Stop(2 * time.Second)
			// Same reason as the HTTP cancellation path above.
			_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
			return store.AgentRun{}, fmt.Errorf("daemon shutting down; bridge spawn cancelled")
		}
		d.bridgeProcs[bridgeKey(req.SessionID, req.Name)] = proc
		d.bridgeMu.Unlock()

		// Wait up to 500ms for the first bridge event (proves healthy startup)
		// or process exit (crash during startup). This converts a silent race
		// where the bridge exits in <500ms into an immediate error returned to
		// the supervisor tool call (Gap 14).
		runDir := filepath.Join(req.Workdir, ".belayer", "runs", req.SessionID, req.Name)
		select {
		case <-proc.FirstEvent():
			// healthy startup — proceed
		case <-proc.Done():
			// bridge exited before posting any event — report failure with stderr tail
			_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
			d.bridgeMu.Lock()
			delete(d.bridgeProcs, bridgeKey(req.SessionID, req.Name))
			d.bridgeMu.Unlock()
			stderrPath := filepath.Join(runDir, "bridge-stderr.log")
			tail := bridgelog.TailLines(stderrPath, 20)
			return store.AgentRun{}, fmt.Errorf("bridge exited during spawn (%v): %s", proc.ExitErr(), tail)
		case <-time.After(500 * time.Millisecond):
			// Slow start — assume the bridge is still initialising.
			// watchBridgeExit will catch any later crash.
		}
	}

	if err := d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "running"); err != nil {
		return store.AgentRun{}, fmt.Errorf("update running status: %w", err)
	}

	stored, err := d.store.GetAgentRun(req.SessionID, req.Name)
	if err != nil {
		return store.AgentRun{}, fmt.Errorf("get agent run: %w", err)
	}

	if proc != nil {
		d.watchBridgeExit(stored, proc)
	}

	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: req.SessionID,
		Type:      "agent_spawned",
		Data: mustJSON(map[string]string{
			"agent":     req.Name,
			"role":      req.Role,
			"profile":   req.Profile,
			"transport": "bridge",
		}),
	})

	return stored, nil
}

func repoNames(repos map[string]string) []string {
	names := make([]string, 0, len(repos))
	for name := range repos {
		names = append(names, name)
	}
	return names
}

// ensureWorktree creates (or reuses) a git worktree for an agent.
// The worktree is placed at <repoRoot>/.belayer/worktrees/<sessionID>/<agentName>
// to avoid collisions across concurrent sessions.
// Returns the absolute path to the worktree directory.
func ensureWorktree(repoRoot, sessionID, agentName, branch string) (string, error) {
	wtDir := filepath.Join(repoRoot, ".belayer", "worktrees", sessionID, agentName)

	// If worktree already exists (re-spawn), reuse it.
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		// Verify it's still a valid worktree by checking for .git file.
		if _, err := os.Stat(filepath.Join(wtDir, ".git")); err == nil {
			log.Printf("Reusing existing worktree for %s at %s", agentName, wtDir)
			return wtDir, nil
		}
		// Directory exists but isn't a worktree — remove and recreate.
		_ = os.RemoveAll(wtDir)
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Try creating the worktree with a new branch first.
	// If the branch already exists, check it out instead.
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtDir)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch might already exist — try without -b.
		cmd2 := exec.Command("git", "worktree", "add", wtDir, branch)
		cmd2.Dir = repoRoot
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return "", fmt.Errorf("git worktree add failed:\n  attempt 1 (-b): %s\n  attempt 2: %s", string(out), string(out2))
		}
	}

	log.Printf("Created worktree for %s on branch %s at %s", agentName, branch, wtDir)
	return wtDir, nil
}

// worktreesDisabled checks whether the project has disabled worktree isolation
// via .belayer/config.yaml in the workspace root.
func worktreesDisabled(workdir string) bool {
	if workdir == "" {
		return false
	}
	cfgPath := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false // no config = worktrees allowed
	}
	// Simple check — avoid pulling in a YAML library for one boolean.
	// Matches "worktrees: false" or "worktrees:false".
	for _, line := range splitLines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "worktrees: false" || trimmed == "worktrees:false" {
			return true
		}
	}
	return false
}

// agentIdentityPaths returns the ordered list of candidate paths to look up
// for an agent identity file, in priority order:
//
//  1. <workdir>/.belayer/agents/<identity>/<file>, then each parent directory's
//     .belayer/agents/<identity>/<file> walking up to filesystem root —
//     project-local overrides
//  2. <belayerRoot>/agents/<identity>/<file>                  — shipped default
//
// The walk-up handles the common case where workdir points at a nested location
// (a session workspace under .belayer/runs/<id>/workspace/, or a worktree under
// .belayer/worktrees/<id>/<name>/) rather than the project root itself —
// without it, the project-local override would be silently invisible.
//
// Empty bases are skipped and duplicate paths are removed.
func agentIdentityPaths(workdir, belayerRoot, identity, file string) []string {
	var paths []string
	seen := make(map[string]struct{})
	addPath := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	if workdir != "" {
		for dir := workdir; dir != ""; {
			addPath(filepath.Join(dir, ".belayer", "agents", identity, file))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if belayerRoot != "" {
		addPath(filepath.Join(belayerRoot, "agents", identity, file))
	}
	return paths
}

// bridgeSocketPath returns the socket path/URL to use as BELAYER_SOCKET for a
// bridge subprocess. For clamshell sandboxes the bridge runs inside a Docker
// container and accesses the daemon via a Unix socket in the bind-mounted
// workspace directory (/workspace/.belayer/daemon.sock). Falls back to the
// TCP gateway URL if the workspace socket path was not configured.
//
// The container-side /workspace path is a clamshell convention enforced by the
// sandbox driver (see sandbox/clamshell.go:sandboxWorkspace). If that constant
// ever changes, this function must be updated in lockstep.
func bridgeSocketPath(mode, unixPath, dockerGateway string, tcpPort int, workspaceSockPath string) string {
	if mode != "clamshell" {
		return unixPath
	}
	// Prefer workspace Unix socket: the workspace is bind-mounted into the
	// container at /workspace, so the container-side path is always this.
	if workspaceSockPath != "" {
		return "/workspace/.belayer/daemon.sock"
	}
	// Fallback: TCP listener via Docker host gateway.
	if tcpPort > 0 {
		return fmt.Sprintf("http://%s:%d", dockerGateway, tcpPort)
	}
	return unixPath
}

func splitLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i])
		s = s[i+1:]
	}
	return lines
}

// translateHostPathToContainer rewrites a host path under hostWorkspace to
// the clamshell container's /workspace mount. Paths not under hostWorkspace
// (or non-absolute paths) are returned unchanged. Mirrors
// sandbox.translateDir, which is unexported.
func translateHostPathToContainer(hostPath, hostWorkspace string) string {
	if hostPath == "" || hostWorkspace == "" {
		return hostPath
	}
	if !filepath.IsAbs(hostPath) {
		return hostPath
	}
	rel, err := filepath.Rel(hostWorkspace, hostPath)
	if err != nil {
		return hostPath
	}
	if rel == "." {
		return "/workspace"
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return hostPath
	}
	return "/workspace/" + filepath.ToSlash(rel)
}

// validateAgentName rejects names that could escape a filesystem path.
// Agent names are used as directory segments under .belayer/runs/<session>/
// and as filenames for per-agent transcripts, so they must not contain path
// separators, "..", NUL bytes, or leading dots.
func validateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("agent name %q is reserved", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("agent name %q must not start with '.'", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("agent name %q must not contain path separators", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("agent name contains NUL byte")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("agent name %q must not contain '..'", name)
	}
	return nil
}

