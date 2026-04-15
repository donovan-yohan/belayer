package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/broker"
	hermesharness "github.com/donovan-yohan/belayer/internal/harness/hermes"
	"github.com/donovan-yohan/belayer/internal/store"
)

type agentSpawnRequest struct {
	SessionID string `json:"-"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Profile   string `json:"profile"`
	Repo      string `json:"repo,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
}

type finishAgentRequest struct {
	Summary string `json:"summary"`
	Blocked bool   `json:"blocked"`
}

func (d *Daemon) handleSpawnAgent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := d.store.GetSession(sessionID); err != nil {
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
	run := store.AgentRun{
		SessionID: sessionID,
		Name:      req.Name,
		Role:      req.Role,
		Profile:   req.Profile,
		RepoScope: req.Repo,
		Workdir:   req.Workdir,
		Transport: "tmux",
		Status:    "starting",
	}
	if _, err := d.store.CreateAgentRun(run); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tmuxSession, err := d.launchAgent(req)
	if err != nil {
		_ = d.store.UpdateAgentRunStatus(sessionID, req.Name, "failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := d.store.UpdateAgentRunTmuxSession(sessionID, req.Name, tmuxSession); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
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
	_ = d.broker.Subscribe(sessionID, req.Name, func(msg broker.Message) {
		_ = d.deliverMessage(stored, msg)
	})
	d.watchAgentExit(stored)
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "agent_spawned",
		Data: mustJSON(map[string]string{
			"agent":       req.Name,
			"role":        req.Role,
			"profile":     req.Profile,
			"tmux_session": tmuxSession,
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

func (d *Daemon) defaultLaunchAgent(req agentSpawnRequest) (string, error) {
	workdir := req.Workdir
	if workdir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine workdir: %w", err)
		}
		workdir = cwd
	}
	runDir := filepath.Join(workdir, ".belayer", "runs", req.SessionID, req.Name)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", fmt.Errorf("create run dir: %w", err)
	}
	tmuxSession := req.SessionID + "-" + req.Name
	cmd, err := hermesharness.BuildLaunchCmd(hermesharness.LaunchConfig{
		Profile:    req.Profile,
		Workdir:    workdir,
		SocketPath: d.config.SocketPath,
		SessionID:  req.SessionID,
		AgentID:    req.Name,
		RunDir:     runDir,
		Skills:     []string{"belayer-support:belayer-communication"},
	})
	if err != nil {
		return "", err
	}
	if err := d.runner.CreateSession(tmuxSession, cmd); err != nil {
		return "", err
	}
	return tmuxSession, nil
}

func (d *Daemon) defaultDeliverMessage(run store.AgentRun, msg broker.Message) error {
	payload := msg.Content
	if err := d.runner.SendKeys(run.TmuxSession, payload, true); err != nil {
		return err
	}
	return d.runner.SendEnter(run.TmuxSession)
}

func (d *Daemon) watchAgentExit(run store.AgentRun) {
	if d.runner == nil || run.TmuxSession == "" {
		return
	}
	go func() {
		if err := d.runner.WaitForSession(run.TmuxSession, 24*time.Hour); err != nil {
			return
		}
		current, err := d.store.GetAgentRun(run.SessionID, run.Name)
		if err != nil {
			return
		}
		if current.Status != "running" {
			return
		}
		marker := filepath.Join(current.Workdir, ".belayer", "runs", current.SessionID, current.Name, ".belayer-finished")
		if _, err := os.Stat(marker); err == nil {
			return
		}
		_ = d.store.UpdateAgentRunStatus(run.SessionID, run.Name, "blocked")
		_ = d.store.LogEvent(store.SessionEvent{
			SessionID: run.SessionID,
			Type:      "agent_exited_without_finish",
			Data:      mustJSON(map[string]string{"agent": run.Name, "status": "blocked"}),
		})
		if run.Name != "planner" {
			msg := broker.Message{SessionID: run.SessionID, SenderID: run.Name, RecipientID: "planner", Type: broker.MessageStateChange, Content: run.Name + " exited without belayer finish and was marked blocked", Urgent: true, Timestamp: time.Now().UTC()}
			_ = d.broker.Interrupt(run.SessionID, "planner", msg)
		}
	}()
}
