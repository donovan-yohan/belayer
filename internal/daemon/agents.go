package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
)

type agentSpawnRequest struct {
	SessionID       string `json:"-"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	Profile         string `json:"profile"`
	Model           string `json:"model,omitempty"`
	Message         string `json:"message,omitempty"`
	Repo            string `json:"repo,omitempty"`
	Workdir         string `json:"workdir,omitempty"`
	Branch          string `json:"branch,omitempty"`          // if set, agent works in a git worktree on this branch
	HermesSessionID string `json:"hermes_session_id,omitempty"`
	Ephemeral       *bool  `json:"ephemeral,omitempty"` // nil = default (true for specialists, false for planner)
}

type finishAgentRequest struct {
	Summary string `json:"summary"`
	Blocked bool   `json:"blocked"`
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
		_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Track the process (may be nil for test stubs).
	if proc != nil {
		d.bridgeMu.Lock()
		d.bridgeProcs[bridgeKey(sessionID, req.Name)] = proc
		d.bridgeMu.Unlock()
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
	var req finishAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// When the planner calls finish (and isn't blocked), trigger PM verification
	// instead of marking the session complete immediately.
	if name == "planner" && !req.Blocked {
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
			"agent":   name,
			"summary": req.Summary,
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
	if name != "planner" {
		content := fmt.Sprintf("%s marked work as %s. Summary: %s", name, status, req.Summary)
		msg := broker.Message{SessionID: sessionID, SenderID: name, RecipientID: "planner", Type: broker.MessageStateChange, Content: content, Timestamp: time.Now().UTC(), Urgent: req.Blocked}
		if req.Blocked {
			_ = d.broker.Interrupt(sessionID, "planner", msg)
		} else {
			_ = d.broker.Send(sessionID, "planner", msg)
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

	// Resolve ephemeral flag: explicit request > role-based default.
	// Planners stay alive by default; all other roles exit on task completion.
	ephemeral := true
	if req.Ephemeral != nil {
		ephemeral = *req.Ephemeral
	} else if req.Role == "planner" {
		ephemeral = false
	}

	// Load system prompt from templates/<name>/system-prompt.md if it exists.
	// The template is keyed by agent name (e.g. "pm", "pilot", "reviewer").
	// Check the workspace first, then fall back to belayer root.
	var systemPrompt string
	for _, base := range []string{workdir, d.config.BelayerRoot} {
		if base == "" {
			continue
		}
		promptPath := filepath.Join(base, "templates", req.Name, "system-prompt.md")
		if data, err := os.ReadFile(promptPath); err == nil {
			systemPrompt = string(data)
			log.Printf("Loaded system prompt from %s for agent %s", promptPath, req.Name)
			break
		}
	}

	// Load belayer_tools from templates/<name>/agent.yaml if it exists.
	var belayerTools []string
	for _, base := range []string{workdir, d.config.BelayerRoot} {
		if base == "" {
			continue
		}
		yamlPath := filepath.Join(base, "templates", req.Name, "agent.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		// Simple line-based parse — avoid YAML library dependency.
		// Looks for "belayer_tools:" then collects "  - tool_name" lines.
		inTools := false
		for _, line := range splitLines(string(data)) {
			trimmed := strings.TrimSpace(line)
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
		log.Printf("Loaded belayer_tools from %s for agent %s: %v", yamlPath, req.Name, belayerTools)
		break
	}

	cfg := bridge.Config{
		SessionID:       req.SessionID,
		AgentID:         req.Name,
		Role:            req.Role,
		Profile:         req.Profile,
		Model:           req.Model,
		Message:         req.Message,
		SystemPrompt:    systemPrompt,
		HermesSessionID: req.HermesSessionID,
		Ephemeral:       ephemeral,
		Workdir:         workdir,
		SocketPath:      d.config.SocketPath,
		RunDir:          runDir,
		BelayerRoot:     d.config.BelayerRoot,
		BelayerTools:    belayerTools,
	}
	_ = worktreePath // stored in DB; cleanup handled separately

	proc, err := bridge.Spawn(cfg)
	if err != nil {
		return nil, fmt.Errorf("bridge spawn: %w", err)
	}
	return proc, nil
}

func (d *Daemon) watchBridgeExit(run store.AgentRun, proc *bridge.Process) {
	go func() {
		<-proc.Done()

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

		// Notify planner.
		if run.Name != "planner" {
			msg := broker.Message{
				SessionID:   run.SessionID,
				SenderID:    run.Name,
				RecipientID: "planner",
				Type:        broker.MessageStateChange,
				Content:     run.Name + " bridge process exited unexpectedly and was marked blocked",
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			_ = d.broker.Interrupt(run.SessionID, "planner", msg)
		}

		// Clean up process reference.
		d.bridgeMu.Lock()
		delete(d.bridgeProcs, bridgeKey(run.SessionID, run.Name))
		d.bridgeMu.Unlock()
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
		d.bridgeProcs[bridgeKey(req.SessionID, req.Name)] = proc
		d.bridgeMu.Unlock()
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
// The worktree is placed at <repoRoot>/.belayer/worktrees/<agentName>.
// Returns the absolute path to the worktree directory.
func ensureWorktree(repoRoot, sessionID, agentName, branch string) (string, error) {
	wtDir := filepath.Join(repoRoot, ".belayer", "worktrees", agentName)

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

