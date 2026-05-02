package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/climbpath"
	"github.com/donovan-yohan/belayer/internal/daemon/bridgelog"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/google/uuid"
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
	Kind            string `json:"kind,omitempty"`
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

type spawnLimitError struct {
	Cap        int
	LiveAgents int
	Code       string
	Message    string
}

func (e *spawnLimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("cap reached (%d live agents); retire one before spawning", e.Cap)
}

func normalizeAgentKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "", "main":
		return "main"
	case "side":
		return "side"
	default:
		return kind
	}
}

func validateAgentKind(kind string) (string, error) {
	normalized := normalizeAgentKind(kind)
	switch normalized {
	case "main", "side":
		return normalized, nil
	default:
		return "", fmt.Errorf("kind must be main or side")
	}
}

func agentRunIsSide(run store.AgentRun) bool {
	return normalizeAgentKind(run.Kind) == "side"
}

func agentRunIsMain(run store.AgentRun) bool {
	return !agentRunIsSide(run)
}

func agentRunStatusIsTerminal(status string) bool {
	switch status {
	case "complete", "blocked", "incomplete", "exited", "failed":
		return true
	default:
		return false
	}
}

func (d *Daemon) markAgentRunRunningUnlessTerminal(sessionID, name string) (store.AgentRun, error) {
	current, err := d.store.GetAgentRun(sessionID, name)
	if err != nil {
		return store.AgentRun{}, err
	}
	if agentRunStatusIsTerminal(current.Status) {
		return current, nil
	}
	if err := d.store.UpdateAgentRunStatus(sessionID, name, "running"); err != nil {
		return store.AgentRun{}, err
	}
	return d.store.GetAgentRun(sessionID, name)
}

func (d *Daemon) lockSpawnSession(sessionID string) func() {
	d.spawnSessionMu.Lock()
	mu := d.spawnSessionLocks[sessionID]
	if mu == nil {
		mu = &sync.Mutex{}
		d.spawnSessionLocks[sessionID] = mu
	}
	d.spawnSessionMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

type agentRunResponse struct {
	ID                 string `json:"id"`
	SessionID          string `json:"session_id"`
	Name               string `json:"name"`
	Role               string `json:"role"`
	Kind               string `json:"kind"`
	Profile            string `json:"profile"`
	RepoScope          string `json:"repo_scope"`
	Workdir            string `json:"workdir"`
	Branch             string `json:"branch"`
	WorktreePath       string `json:"worktree_path"`
	Transport          string `json:"transport"`
	TmuxSession        string `json:"tmux_session"`
	HermesSessionID    string `json:"hermes_session_id"`
	Status             string `json:"status"`
	Outcome            string `json:"outcome"`
	DestructiveActions int    `json:"destructive_actions,omitempty"`
	LastDestructiveCmd string `json:"last_destructive_cmd,omitempty"`
	PendingMailCount   int    `json:"pending_mail_count,omitempty"`
	UnackedMailCount   int    `json:"unacked_mail_count,omitempty"`
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
	kind := ""
	if req.Kind != "" {
		var kindErr error
		kind, kindErr = validateAgentKind(req.Kind)
		if kindErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": kindErr.Error()})
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

	identityKind := ""
	if kind == "" {
		loaded := loadAgentIdentity(req.Workdir, d.config.BelayerRoot, req.identityKey(), req.Model)
		if loaded.Kind != "" {
			var kindErr error
			identityKind, kindErr = validateAgentKind(loaded.Kind)
			if kindErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identity kind: " + kindErr.Error()})
				return
			}
		}
	}

	// Check for a prior run of this agent in this session.
	// If it exists, carry over its Hermes session ID for resume and update
	// the existing row instead of creating a new one (UNIQUE constraint).
	prior, priorErr := d.store.GetAgentRun(sessionID, req.Name)
	if priorErr == nil && kind == "" {
		kind, _ = validateAgentKind(prior.Kind)
	}
	if kind == "" && identityKind != "" {
		kind = identityKind
	}
	if kind == "" {
		kind = "main"
	}
	req.Kind = kind

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

	unlockSpawn := d.lockSpawnSession(sessionID)
	excludeName := ""
	if priorErr == nil {
		excludeName = req.Name
	}
	if err := d.enforceSpawnCapacity(sessionID, sess.WorkspaceDir, excludeName, req.Kind); err != nil {
		unlockSpawn()
		var limitErr *spawnLimitError
		if errors.As(err, &limitErr) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       limitErr.Error(),
				"code":        limitErr.Code,
				"limit":       limitErr.Cap,
				"live_agents": limitErr.LiveAgents,
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if priorErr == nil {
		// Prior run exists — update its status back to starting.
		// Carry over branch/worktree path from prior run if not re-specified.
		if req.Branch == "" && prior.Branch != "" {
			req.Branch = prior.Branch
		}
		if kind == "" {
			kind = prior.Kind
		}
		if prior.Kind != "" && kind != prior.Kind {
			unlockSpawn()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind cannot change for an existing agent run"})
			return
		}
		if err := d.store.UpdateAgentRunStatus(sessionID, req.Name, "starting"); err != nil {
			unlockSpawn()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := d.store.UpdateAgentRunOutcome(sessionID, req.Name, "active"); err != nil {
			unlockSpawn()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if prior.Kind != req.Kind {
			if err := d.store.UpdateAgentRunKind(sessionID, req.Name, req.Kind); err != nil {
				unlockSpawn()
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
	} else {
		// No prior run — create a new row.
		run := store.AgentRun{
			SessionID:       sessionID,
			Name:            req.Name,
			Role:            req.Role,
			Kind:            req.Kind,
			Profile:         req.Profile,
			RepoScope:       req.Repo,
			Workdir:         req.Workdir,
			Branch:          req.Branch,
			HermesSessionID: req.HermesSessionID,
			Transport:       "bridge",
			Status:          "starting",
		}
		if _, err := d.store.CreateAgentRun(run); err != nil {
			unlockSpawn()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	unlockSpawn()

	proc, err := d.spawnBridgeAgent(req)
	if err != nil {
		log.Printf("spawn agent %s failed: %v", req.Name, err)
		_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
		_ = d.store.UpdateAgentRunOutcome(sessionID, req.Name, "failed")
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
			_ = d.store.UpdateAgentRunOutcome(sessionID, req.Name, "failed")
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
		runDir := climbpath.AgentDir(workdirForRunDir, sessionID, req.Name)
		select {
		case <-proc.FirstEvent():
			// healthy startup — proceed
		case <-proc.Done():
			// bridge exited before posting any event — report failure with stderr tail
			_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
			_ = d.store.UpdateAgentRunOutcome(sessionID, req.Name, "failed")
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

	stored, err := d.markAgentRunRunningUnlessTerminal(sessionID, req.Name)
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
		Data: mustJSON(map[string]any{
			"agent":     req.Name,
			"role":      req.Role,
			"kind":      req.Kind,
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
	states := d.agentSurfaceStates(sessionID)
	resp := make([]agentRunResponse, 0, len(runs))
	for _, run := range runs {
		state := states[run.Name]
		pendingCount := 0
		if agentRunIsMain(run) {
			if pending, err := d.store.PendingMessages(sessionID, run.Name, ""); err == nil {
				pendingCount = len(pending)
			}
		}
		unackedCount := 0
		if agentRunIsMain(run) {
			if unacked, err := d.store.UnackedMessages(sessionID, run.Name, ""); err == nil {
				unackedCount = len(unacked)
			}
		}
		resp = append(resp, agentRunResponse{
			ID:                 run.ID,
			SessionID:          run.SessionID,
			Name:               run.Name,
			Role:               run.Role,
			Kind:               run.Kind,
			Profile:            run.Profile,
			RepoScope:          run.RepoScope,
			Workdir:            run.Workdir,
			Branch:             run.Branch,
			WorktreePath:       run.WorktreePath,
			Transport:          run.Transport,
			TmuxSession:        run.TmuxSession,
			HermesSessionID:    run.HermesSessionID,
			Status:             state.Lifecycle,
			Outcome:            state.Outcome,
			DestructiveActions: run.DestructiveActions,
			LastDestructiveCmd: run.LastDestructiveCmd,
			PendingMailCount:   pendingCount,
			UnackedMailCount:   unackedCount,
		})
	}
	writeJSON(w, http.StatusOK, resp)
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
			"agent":         name,
			"summary":       req.Summary,
			"spec_artifact": req.SpecArtifact,
		})
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "pending_verification",
			"message": "Acceptance gate agent spawned for spec verification",
		})
		return
	}

	status := "complete"
	outcome := "succeeded"
	if req.Blocked {
		status = "blocked"
		outcome = "blocked"
	}
	if err := d.store.UpdateAgentRunStatus(sessionID, name, status); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = d.store.UpdateAgentRunOutcome(sessionID, name, outcome)
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
		sender := name
		if agentRunIsSide(run) {
			sender = "system"
		}
		msg := broker.Message{SessionID: sessionID, SenderID: sender, RecipientID: "supervisor", Type: broker.MessageStateChange, Content: content, Timestamp: time.Now().UTC(), Urgent: req.Blocked}
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
	initialMessage := req.Message
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
		// Inject workspace context so the agent knows its isolation boundary without
		// needing it prescribed in the identity prompt. The daemon owns the path
		// convention; the agent just reads the fact.
		initialMessage = buildAgentInitialMessage(req.Branch, wtPath, req.Message)
	}

	runDir := climbpath.AgentDir(workdir, req.SessionID, req.Name)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, fmt.Errorf("create climb dir: %w", err)
	}

	// Compute Landlock write roots for this agent (used when ConfineAgentWrites is true).
	writeRoots := computeWriteRoots(req, repoWorkdir, worktreePath, runDir, d.config.AgentSharedWritePaths)

	// Load system prompt + agent.yaml settings from the agent's identity dir.
	// Project-local <workdir>/.belayer/agents/<identity>/ overrides shipped
	// <BelayerRoot>/agents/<identity>/. Identity defaults to Name when unset.
	identity := req.identityKey()
	loaded := loadAgentIdentity(workdir, d.config.BelayerRoot, identity, req.Model)
	systemPrompt := loaded.SystemPrompt
	belayerTools := loaded.BelayerTools
	enabledToolsets := loaded.EnabledToolsets
	agentModel := loaded.Model
	agentMaxTurns := loaded.MaxTurns
	if loaded.SystemPromptPath != "" {
		log.Printf("Loaded system prompt from %s for agent %s (identity=%s)", loaded.SystemPromptPath, req.Name, identity)
	}
	if loaded.YAMLPath != "" {
		log.Printf("Loaded agent.yaml from %s for agent %s (identity=%s): model=%q tools=%v toolsets=%v", loaded.YAMLPath, req.Name, identity, agentModel, belayerTools, enabledToolsets)
	}
	// Resolve ephemeral flag: explicit request > identity config > role-based
	// default. Supervisors stay alive by default; all other roles exit on task
	// completion unless their identity says otherwise.
	ephemeral := true
	if req.Ephemeral != nil {
		ephemeral = *req.Ephemeral
	} else if loaded.Ephemeral != nil {
		ephemeral = *loaded.Ephemeral
	} else if req.Role == "supervisor" {
		ephemeral = false
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
	// sess.WorkspaceDir/.belayer/climbs/<session>/transcripts/ finds every
	// agent's file, including branch-based specialists. If sess.WorkspaceDir
	// is empty, the session is never archived anyway (archive_manager.doArchive
	// skips), so there's no point writing transcripts.
	var transcriptPath string
	// Tier model is monotonic (standard ⊂ verbose ⊂ trace): trace sessions
	// still want the verbose transcript channel in addition to trace fragments.
	if (sess.LogLevel == LogLevelVerbose || sess.LogLevel == LogLevelTrace) && sess.WorkspaceDir != "" {
		transcriptsDir := climbpath.TranscriptsDir(sess.WorkspaceDir, req.SessionID)
		if mkErr := os.MkdirAll(transcriptsDir, 0o700); mkErr != nil {
			log.Printf("spawn %s: create transcripts dir failed (continuing without transcript): %v", req.Name, mkErr)
		} else {
			transcriptPath = filepath.Join(transcriptsDir, req.Name+".jsonl")
		}
	}

	socketPath := d.config.SocketPath
	if ss.mode != sandbox.DefaultMode && d.config.WorkspaceSockPath != "" {
		socketPath = d.config.WorkspaceSockPath
	}
	log.Printf("spawn %s: mode=%q socketPath=%q tcpPort=%d", req.Name, ss.mode, socketPath, d.tcpPort)
	cfg := bridge.Config{
		SessionID:       req.SessionID,
		AgentID:         req.Name,
		Role:            req.Role,
		Profile:         req.Profile,
		Model:           agentModel,
		MaxTurns:        agentMaxTurns,
		APIKey:          d.config.BridgeAPIKey,
		BaseURL:         d.config.BridgeBaseURL,
		Provider:        d.config.BridgeProvider,
		Message:         initialMessage,
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
		BelayerRoot:         d.config.RuntimeDir,
		BelayerTools:        belayerTools,
		EnabledToolsets:     enabledToolsets,
		TranscriptPath:      transcriptPath,
		LogLevel:            sess.LogLevel,
		SkipOpenRouterProbe: d.config.SkipOpenRouterProbe,
		WriteRoots:          writeRoots,
		ConfineWrites:       d.config.ConfineAgentWrites,
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
	env = append(env, "BELAYER_AGENT_KIND="+req.Kind)
	env = append(env, "BELAYER_AGENT_ARTIFACT_DIR="+climbpath.AgentArtifactsRel(workdir, req.SessionID, req.Name))

	// Use an io.Pipe for stdout so the scanner pump can tee bytes to the log
	// writer while concurrently scanning for error markers. If we assigned
	// io.MultiWriter directly to Stdout, NewProcess would have no scanner and
	// proc.StdoutErrors() would be nil — meaning stdout markers would never
	// synthesize bridge:failed events in watchBridgeExit.
	stdoutPipeR, stdoutPipeW := io.Pipe()

	osProc, err := ss.driver.Exec(d.startCtx, ss.handle, argv, sandbox.ExecOpts{
		Env:    env,
		Dir:    workdir,
		Stdin:  stdinR,
		Stdout: stdoutPipeW,
		Stderr: io.MultiWriter(os.Stderr, stderrLog),
	})
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutLog.Close()
		_ = stderrLog.Close()
		_ = stdoutPipeR.Close()
		_ = stdoutPipeW.Close()
		return nil, fmt.Errorf("sandbox exec: %w", err)
	}

	// Close read end — it's been inherited by the child process.
	_ = stdinR.Close()

	proc := bridge.NewProcess(osProc, stdinW)

	// Attach the stdout scanner so proc.StdoutErrors() is non-nil and
	// watchBridgeExit can synthesize bridge:failed events from LLM/API
	// connectivity markers detected in stdout.
	pumpDone := proc.StartStdoutScanner(stdoutPipeR, io.MultiWriter(os.Stdout, stdoutLog))

	// Close log files and stdin writer when process exits. stdinW is our
	// handle for sending interrupts; once the process is gone it's a pipe to
	// nowhere, so closing it avoids leaking file descriptors. We must close
	// stdoutPipeW to EOF the scanner pump, then wait for pumpDone before
	// closing stdoutLog so the log file contains complete output.
	go func() {
		<-proc.Done()
		_ = stdoutPipeW.Close() // EOF the scanner pump
		<-pumpDone
		_ = stdoutPipeR.Close()
		_ = stdinW.Close()
		_ = stdoutLog.Close()
		_ = stderrLog.Close()
	}()

	return proc, nil
}

func (d *Daemon) watchBridgeExit(run store.AgentRun, proc *bridge.Process) {
	// Drain stdout errors in a separate goroutine. When a matching line is
	// detected AND the agent is still running, synthesize a bridge:failed event
	// so the session can transition to failed rather than silently stalling.
	if proc.StdoutErrors() != nil {
		go func() {
			for se := range proc.StdoutErrors() {
				current, err := d.store.GetAgentRun(run.SessionID, run.Name)
				if err != nil || current.Status != "running" {
					continue
				}
				eventData := mustJSON(map[string]string{
					"agent":         run.Name,
					"error":         se.Line,
					"stdout_marker": se.Pattern,
					"source":        "stdout_scanner",
				})
				_ = d.store.LogEvent(store.SessionEvent{
					SessionID: run.SessionID,
					Type:      "bridge:failed",
					Data:      eventData,
				})
				log.Printf("stdout_scanner: session %s agent %s matched pattern %q: %s",
					run.SessionID, run.Name, se.Pattern, se.Line)
				d.handleBridgeFailed(run.SessionID, run.Name, map[string]any{
					"agent":         run.Name,
					"error":         se.Line,
					"stdout_marker": se.Pattern,
					"source":        "stdout_scanner",
				})
			}
		}()
	}

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
		_ = d.store.UpdateAgentRunOutcome(run.SessionID, run.Name, "failed")
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
			// Reconstruct the climb directory for this agent. If the agent was
			// spawned on a branch it has a worktree; otherwise it uses Workdir.
			runBase := run.Workdir
			if run.WorktreePath != "" {
				runBase = run.WorktreePath
			}
			runDir := climbpath.ExistingAgentDir(runBase, run.SessionID, run.Name)
			stderrPath := filepath.Join(runDir, "bridge-stderr.log")
			stderrTail := bridgelog.TailLines(stderrPath, 50)
			stdoutPath := filepath.Join(runDir, "bridge-stdout.log")
			stdoutTail := bridgelog.TailLines(stdoutPath, 50)
			content := fmt.Sprintf(
				"%s bridge exited unexpectedly (exit_err: %v). Marked blocked.\n\nLast 50 lines of bridge-stderr.log:\n%s\n\nLast 50 lines of bridge-stdout.log:\n%s",
				run.Name, exitErr, stderrTail, stdoutTail)
			msgID := uuid.New().String()
			msg := broker.Message{
				ID:          msgID,
				SessionID:   run.SessionID,
				SenderID:    run.Name,
				RecipientID: "supervisor",
				Type:        broker.MessageStateChange,
				Content:     content,
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			// Persist so bridge-based supervisors can pull via GET /messages.
			_, _ = d.store.CreateMessage(store.Message{
				ID:          msgID,
				SessionID:   run.SessionID,
				SenderID:    run.Name,
				RecipientID: "supervisor",
				Type:        string(broker.MessageStateChange),
				Content:     content,
				Urgent:      true,
			})
			_ = d.broker.Interrupt(run.SessionID, "supervisor", msg)
		}

		// Check if this was the last active agent (session may be stalled).
		d.checkSessionStalled(run.SessionID)
	}()
}

// interruptBridgeAgent sends an interrupt command to a bridge agent's stdin.
func (d *Daemon) interruptBridgeAgent(sessionID, agentName, messageID, from, content string) error {
	d.bridgeMu.RLock()
	proc := d.bridgeProcs[bridgeKey(sessionID, agentName)]
	d.bridgeMu.RUnlock()

	if proc == nil {
		return fmt.Errorf("no bridge process for %s/%s", sessionID, agentName)
	}
	return proc.InterruptMessage(messageID, from, content)
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

	kind := ""
	if req.Kind != "" {
		var kindErr error
		kind, kindErr = validateAgentKind(req.Kind)
		if kindErr != nil {
			return store.AgentRun{}, kindErr
		}
	}
	identityKind := ""
	if kind == "" {
		loaded := loadAgentIdentity(req.Workdir, d.config.BelayerRoot, req.identityKey(), req.Model)
		if loaded.Kind != "" {
			var kindErr error
			identityKind, kindErr = validateAgentKind(loaded.Kind)
			if kindErr != nil {
				return store.AgentRun{}, fmt.Errorf("identity kind: %w", kindErr)
			}
		}
	}

	// Check for prior run (resume support).
	prior, priorErr := d.store.GetAgentRun(req.SessionID, req.Name)
	if priorErr == nil && kind == "" {
		kind, _ = validateAgentKind(prior.Kind)
	}
	if kind == "" && identityKind != "" {
		kind = identityKind
	}
	if kind == "" {
		kind = "main"
	}
	req.Kind = kind
	if priorErr == nil && prior.HermesSessionID != "" && req.HermesSessionID == "" {
		req.HermesSessionID = prior.HermesSessionID
		log.Printf("Resuming agent %s with hermes session %s", req.Name, req.HermesSessionID)
	}

	if priorErr == nil {
		unlockSpawn := d.lockSpawnSession(req.SessionID)
		if err := d.enforceSpawnCapacity(req.SessionID, sess.WorkspaceDir, req.Name, req.Kind); err != nil {
			unlockSpawn()
			return store.AgentRun{}, err
		}
		if req.Branch == "" && prior.Branch != "" {
			req.Branch = prior.Branch
		}
		if prior.Kind != "" && req.Kind != prior.Kind {
			unlockSpawn()
			return store.AgentRun{}, fmt.Errorf("kind cannot change for an existing agent run")
		}
		if err := d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "starting"); err != nil {
			unlockSpawn()
			return store.AgentRun{}, fmt.Errorf("update agent status: %w", err)
		}
		if err := d.store.UpdateAgentRunOutcome(req.SessionID, req.Name, "active"); err != nil {
			unlockSpawn()
			return store.AgentRun{}, fmt.Errorf("update agent outcome: %w", err)
		}
		if prior.Kind != req.Kind {
			if err := d.store.UpdateAgentRunKind(req.SessionID, req.Name, req.Kind); err != nil {
				unlockSpawn()
				return store.AgentRun{}, err
			}
		}
		unlockSpawn()
	} else {
		unlockSpawn := d.lockSpawnSession(req.SessionID)
		if err := d.enforceSpawnCapacity(req.SessionID, sess.WorkspaceDir, "", req.Kind); err != nil {
			unlockSpawn()
			return store.AgentRun{}, err
		}
		run := store.AgentRun{
			SessionID:       req.SessionID,
			Name:            req.Name,
			Role:            req.Role,
			Kind:            req.Kind,
			Profile:         req.Profile,
			RepoScope:       req.Repo,
			Workdir:         req.Workdir,
			Branch:          req.Branch,
			HermesSessionID: req.HermesSessionID,
			Transport:       "bridge",
			Status:          "starting",
		}
		if _, err := d.store.CreateAgentRun(run); err != nil {
			unlockSpawn()
			return store.AgentRun{}, fmt.Errorf("create agent run: %w", err)
		}
		unlockSpawn()
	}

	proc, err := d.spawnBridgeAgent(req)
	if err != nil {
		_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
		_ = d.store.UpdateAgentRunOutcome(req.SessionID, req.Name, "failed")
		return store.AgentRun{}, fmt.Errorf("spawn bridge: %w", err)
	}

	if proc != nil {
		d.bridgeMu.Lock()
		if d.bridgeShuttingDownSessions[req.SessionID] {
			d.bridgeMu.Unlock()
			_ = proc.Stop(2 * time.Second)
			// Same reason as the HTTP cancellation path above.
			_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
			_ = d.store.UpdateAgentRunOutcome(req.SessionID, req.Name, "failed")
			return store.AgentRun{}, fmt.Errorf("daemon shutting down; bridge spawn cancelled")
		}
		d.bridgeProcs[bridgeKey(req.SessionID, req.Name)] = proc
		d.bridgeMu.Unlock()

		// Wait up to 500ms for the first bridge event (proves healthy startup)
		// or process exit (crash during startup). This converts a silent race
		// where the bridge exits in <500ms into an immediate error returned to
		// the supervisor tool call (Gap 14).
		runDir := climbpath.AgentDir(req.Workdir, req.SessionID, req.Name)
		select {
		case <-proc.FirstEvent():
			// healthy startup — proceed
		case <-proc.Done():
			// bridge exited before posting any event — report failure with stderr tail
			_ = d.store.UpdateAgentRunStatus(req.SessionID, req.Name, "failed")
			_ = d.store.UpdateAgentRunOutcome(req.SessionID, req.Name, "failed")
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

	stored, err := d.markAgentRunRunningUnlessTerminal(req.SessionID, req.Name)
	if err != nil {
		return store.AgentRun{}, fmt.Errorf("update running status: %w", err)
	}

	if proc != nil {
		d.watchBridgeExit(stored, proc)
	}

	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: req.SessionID,
		Type:      "agent_spawned",
		Data: mustJSON(map[string]any{
			"agent":     req.Name,
			"role":      req.Role,
			"kind":      req.Kind,
			"profile":   req.Profile,
			"transport": "bridge",
		}),
	})

	return stored, nil
}

func (d *Daemon) enforceSpawnCapacity(sessionID, workdir, excludeName, kind string) error {
	caps, splitMode, err := d.spawnCapsForWorkdir(workdir)
	if err != nil {
		return err
	}
	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		return err
	}
	liveMain := 0
	liveSide := 0
	for _, run := range runs {
		if excludeName != "" && run.Name == excludeName {
			continue
		}
		lifecycle := lifecycleFromRunStatus(run.Status, d.isBridgeLive(sessionID, run.Name))
		switch lifecycle {
		case "starting", "running", "idle":
			if agentRunIsSide(run) {
				liveSide++
			} else {
				liveMain++
			}
		}
	}

	if !splitMode {
		liveAgents := liveMain + liveSide
		if liveAgents >= caps.legacy {
			return &spawnLimitError{Cap: caps.legacy, LiveAgents: liveAgents, Code: "max_concurrent_agents"}
		}
		return nil
	}

	if liveSide >= caps.sides {
		if normalizeAgentKind(kind) == "side" {
			return &spawnLimitError{
				Cap:        caps.sides,
				LiveAgents: liveSide,
				Code:       "max_concurrent_sides",
				Message:    fmt.Sprintf("side summon cap reached (%d live sides); retire one before summoning", caps.sides),
			}
		}
	}
	if liveMain >= caps.mains {
		if normalizeAgentKind(kind) != "side" {
			return &spawnLimitError{
				Cap:        caps.mains,
				LiveAgents: liveMain,
				Code:       "max_concurrent_mains",
				Message:    fmt.Sprintf("main agent cap reached (%d live mains); retire one before spawning", caps.mains),
			}
		}
	}
	if normalizeAgentKind(kind) == "side" {
		sideSummons, err := d.countSideSummons(sessionID)
		if err != nil {
			return err
		}
		if sideSummons >= caps.sideSummons {
			return &spawnLimitError{
				Cap:        caps.sideSummons,
				LiveAgents: sideSummons,
				Code:       "max_side_summons_per_session",
				Message:    fmt.Sprintf("side summon budget reached (%d summons this session); do not summon more sides", caps.sideSummons),
			}
		}
	}
	return nil
}

func (d *Daemon) countSideSummons(sessionID string) (int, error) {
	events, err := d.store.QueryEvents(sessionID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, evt := range events {
		if evt.Type != "agent_spawned" {
			continue
		}
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
			continue
		}
		if normalizeAgentKind(payload.Kind) == "side" {
			count++
		}
	}
	return count, nil
}

func (d *Daemon) spawnCapsForWorkdir(workdir string) (spawnCapsConfig, bool, error) {
	caps := spawnCapsConfig{
		legacy:      d.config.MaxConcurrentAgents,
		mains:       d.config.MaxConcurrentMains,
		sides:       d.config.MaxConcurrentSides,
		sideSummons: d.config.MaxSideSummonsPerSession,
	}
	loaded, err := LoadRuntimeCaps(workdir)
	if err != nil {
		return caps, false, err
	}
	if loaded.MaxConcurrentAgents > 0 {
		caps.legacy = loaded.MaxConcurrentAgents
	}
	if loaded.MaxConcurrentMains > 0 {
		caps.mains = loaded.MaxConcurrentMains
	}
	if loaded.MaxConcurrentSides > 0 {
		caps.sides = loaded.MaxConcurrentSides
	}
	if loaded.MaxSideSummonsPerSession > 0 {
		caps.sideSummons = loaded.MaxSideSummonsPerSession
	}
	if caps.mains == 0 && caps.sides == 0 && caps.sideSummons == 0 {
		if caps.legacy <= 0 {
			caps.legacy = 15
		}
		return caps, false, nil
	}
	if caps.mains <= 0 {
		caps.mains = caps.legacy
		if caps.mains <= 0 {
			caps.mains = 15
		}
	}
	if caps.sides <= 0 {
		caps.sides = caps.legacy
		if caps.sides <= 0 {
			caps.sides = 15
		}
	}
	if caps.sideSummons <= 0 {
		caps.sideSummons = 30
	}
	return caps, true, nil
}

type spawnCapsConfig struct {
	legacy      int
	mains       int
	sides       int
	sideSummons int
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

// agentIdentity captures everything the daemon reads from an agent's
// identity directory (.belayer/agents/<identity>/). Populated by
// loadAgentIdentity and consumed when building bridge.Config.
//
// An empty SystemPromptPath / YAMLPath means the respective file was not
// found — useful for tests and for deciding whether to log the "loaded X"
// breadcrumbs.
type agentIdentity struct {
	SystemPrompt     string
	SystemPromptPath string
	BelayerTools     []string
	EnabledToolsets  []string
	Model            string
	MaxTurns         int
	Kind             string
	Ephemeral        *bool
	YAMLPath         string
}

// loadAgentIdentity resolves an agent's identity files (system-prompt.md and
// agent.yaml) under workdir (project-local override) first and then
// belayerRoot (shipped default), and returns the merged settings.
//
// modelOverride, when non-empty, takes precedence over any model: line in
// agent.yaml (an explicit spawn request always wins).
//
// This function is the single source of truth for "how the daemon reads an
// agent identity" — used both by bridgeLaunchAgent for real spawns and by
// tests that want to assert what reaches the bridge subprocess.
func loadAgentIdentity(workdir, belayerRoot, identity, modelOverride string) agentIdentity {
	out := agentIdentity{Model: modelOverride}

	for _, candidate := range agentIdentityPaths(workdir, belayerRoot, identity, "system-prompt.md") {
		if data, err := os.ReadFile(candidate); err == nil {
			out.SystemPrompt = string(data)
			out.SystemPromptPath = candidate
			break
		}
	}

	for _, yamlPath := range agentIdentityPaths(workdir, belayerRoot, identity, "agent.yaml") {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		out.YAMLPath = yamlPath
		inTools := false
		inToolsets := false
		for _, line := range splitLines(string(data)) {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "model:") && out.Model == "" {
				out.Model = strings.TrimSpace(strings.TrimPrefix(trimmed, "model:"))
				inTools = false
				inToolsets = false
				continue
			}
			if strings.HasPrefix(trimmed, "kind:") && out.Kind == "" {
				out.Kind = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:")), `"'`)
				inTools = false
				inToolsets = false
				continue
			}
			if strings.HasPrefix(trimmed, "ephemeral:") && out.Ephemeral == nil {
				raw := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "ephemeral:")), `"'`)
				if parsed, convErr := strconv.ParseBool(raw); convErr == nil {
					out.Ephemeral = &parsed
				}
				inTools = false
				inToolsets = false
				continue
			}
			if strings.HasPrefix(trimmed, "max_turns:") && out.MaxTurns == 0 {
				raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "max_turns:"))
				if n, convErr := strconv.Atoi(raw); convErr == nil && n > 0 {
					out.MaxTurns = n
				}
				inTools = false
				inToolsets = false
				continue
			}
			if strings.HasPrefix(trimmed, "belayer_tools:") {
				inTools = false
				inToolsets = false
				raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "belayer_tools:"))
				switch raw {
				case "":
					out.BelayerTools = []string{} // mark explicitly configured
					inTools = true
				case "[]":
					out.BelayerTools = []string{} // explicit empty list
				default:
					if items := parseInlineYAMLList(raw); items != nil {
						out.BelayerTools = items
					}
				}
				continue
			}
			if strings.HasPrefix(trimmed, "enabled_toolsets:") {
				inToolsets = false
				inTools = false
				raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "enabled_toolsets:"))
				switch raw {
				case "":
					out.EnabledToolsets = []string{} // mark explicitly configured
					inToolsets = true
				case "[]":
					out.EnabledToolsets = []string{} // explicit empty list
				default:
					if items := parseInlineYAMLList(raw); items != nil {
						out.EnabledToolsets = items
					}
				}
				continue
			}
			if inTools {
				if strings.HasPrefix(trimmed, "- ") {
					out.BelayerTools = append(out.BelayerTools, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				} else {
					inTools = false
				}
			}
			if inToolsets {
				if strings.HasPrefix(trimmed, "- ") {
					out.EnabledToolsets = append(out.EnabledToolsets, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				} else {
					inToolsets = false
				}
			}
		}
		break
	}

	return out
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
// (a session workspace under .belayer/climbs/<id>/workspace/, or a worktree under
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

// parseInlineYAMLList extracts comma-separated items from an inline YAML list
// like `[file, code_execution]` or `["a", 'b']`. Returns nil if the raw string
// does not start with '[' and end with ']'.
func parseInlineYAMLList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return []string{}
	}
	var out []string
	for _, item := range strings.Split(inner, ",") {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// buildAgentInitialMessage prepends a workspace context header to the agent's
// initial message when the agent is isolated to a git worktree on a specific
// branch. When branch is empty the message is returned unchanged.
//
// The header tells the agent exactly where its working tree lives so it does
// not need to rely on identity-prompt conventions (which may contradict the
// real cwd injected by the daemon).
func buildAgentInitialMessage(branch, worktreePath, message string) string {
	if branch == "" {
		return message
	}
	prefix := fmt.Sprintf("[workspace: %s (git worktree on branch %s)]\n\n", worktreePath, branch)
	return prefix + message
}

// validateAgentName rejects names that could escape a filesystem path.
// Agent names are used as directory segments under .belayer/climbs/<session>/
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
