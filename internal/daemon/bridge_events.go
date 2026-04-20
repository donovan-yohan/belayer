package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"
)

const maxRejectionCycles = 3

// processBridgeEvent handles side effects for bridge:* events.
// It is called after the event has already been persisted to the event log.
func (d *Daemon) processBridgeEvent(sessionID, eventType, data string) {
	var eventData map[string]any
	if data != "" {
		_ = json.Unmarshal([]byte(data), &eventData)
	}
	agentName, _ := eventData["agent"].(string)
	if agentName == "" {
		return
	}

	switch eventType {
	case "bridge:started":
		d.handleBridgeStarted(sessionID, agentName, eventData)
	case "bridge:finished":
		d.handleBridgeFinished(sessionID, agentName, eventData)
	case "bridge:failed":
		d.handleBridgeFailed(sessionID, agentName, eventData)
	case "bridge:clarification_needed":
		d.handleBridgeClarification(sessionID, agentName, eventData)
	case "bridge:completion_requested":
		d.handleBridgeCompletionRequested(sessionID, agentName, eventData)
	case "bridge:completion_approved":
		d.handleBridgeCompletionApproved(sessionID, agentName, eventData)
	case "bridge:completion_rejected":
		d.handleBridgeCompletionRejected(sessionID, agentName, eventData)
	}
	// bridge:step_completed, bridge:heartbeat, bridge:tool_started,
	// bridge:tool_completed, bridge:status_change, bridge:turn_usage,
	// bridge:session_usage — log-only, no side effects needed.
}

func (d *Daemon) handleBridgeStarted(sessionID, agentName string, data map[string]any) {
	hermesSessionID, _ := data["hermes_session_id"].(string)
	if hermesSessionID != "" {
		_ = d.store.UpdateAgentRunHermesSessionID(sessionID, agentName, hermesSessionID)
	}
}

func (d *Daemon) handleBridgeFinished(sessionID, agentName string, data map[string]any) {
	// Don't overwrite "incomplete" — the agent already reported it couldn't finish.
	current, err := d.store.GetAgentRun(sessionID, agentName)
	if err != nil || current.Status != "incomplete" {
		if err := d.store.UpdateAgentRunStatus(sessionID, agentName, "complete"); err != nil {
			log.Printf("ERROR: handleBridgeFinished: failed to update agent %s status in session %s: %v", agentName, sessionID, err)
		}
	}

	// No auto-generated message to the supervisor here. The specialist should
	// have sent its own report via belayer_send_message before exiting.
	// If the bridge crashes without a clean exit, watchBridgeExit handles
	// the notification. Auto-generating a state_change here causes duplicate
	// messages that the supervisor dismisses as noise.

	// Check for supervisor exiting while specialists are still running.
	if agentName == "supervisor" {
		d.checkSupervisorExitedEarly(sessionID)
	}

	// Check if the session is now stalled (all agents done, no completion approval).
	d.checkSessionStalled(sessionID)
}

// checkSupervisorExitedEarly emits a warning if the supervisor exits while
// any specialist agents are still running or starting.
func (d *Daemon) checkSupervisorExitedEarly(sessionID string) {
	agents, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		log.Printf("WARNING: checkSupervisorExitedEarly: ListAgentRuns failed for session %s: %v", sessionID, err)
		return
	}
	for _, a := range agents {
		if a.Name == "supervisor" {
			continue
		}
		if a.Status == "running" || a.Status == "starting" {
			log.Printf("WARNING: supervisor exited while %s is still %s in session %s", a.Name, a.Status, sessionID)
			_ = d.store.LogEvent(store.SessionEvent{
				SessionID: sessionID,
				Type:      "warning:supervisor_exited_early",
				Data: mustJSON(map[string]string{
					"active_agent": a.Name,
					"agent_status": a.Status,
				}),
			})
			return // one warning is enough
		}
	}
}

// checkSessionStalled detects when all agents have exited but the session
// was never completed via the PM gate. Transitions session to "stalled".
func (d *Daemon) checkSessionStalled(sessionID string) {
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		log.Printf("WARNING: checkSessionStalled: GetSession failed for session %s: %v", sessionID, err)
		return
	}
	// Only check sessions that are still "running".
	if sess.Status != "running" {
		return
	}

	agents, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		log.Printf("WARNING: checkSessionStalled: ListAgentRuns failed for session %s: %v", sessionID, err)
		return
	}
	if len(agents) == 0 {
		return
	}

	// If any agent is still active, session is not stalled.
	for _, a := range agents {
		if a.Status == "running" || a.Status == "starting" || a.Status == "pending_verification" {
			return
		}
	}

	// All agents are done/blocked/complete but session is still "running".
	// Use conditional update to avoid race when multiple agents finish concurrently.
	updated, err := d.store.UpdateSessionStatusIf(sessionID, "running", "stalled")
	if err != nil {
		log.Printf("ERROR: checkSessionStalled: failed to mark session %s as stalled: %v", sessionID, err)
		return
	}
	if !updated {
		return // another goroutine already transitioned this session
	}
	if err := d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "session_stalled",
		Data: mustJSON(map[string]string{
			"reason": "all_agents_exited_without_completion",
		}),
	}); err != nil {
		log.Printf("WARNING: checkSessionStalled: session %s marked stalled but event log failed: %v", sessionID, err)
	}
	d.archiver.ArchiveTerminal(sessionID)
	log.Printf("Session %s marked stalled: all agents exited without completion approval", sessionID)
}

// processAgentStatusEvent handles side effects for agent_status:* events
// posted by agents via belayer_report_status.
func (d *Daemon) processAgentStatusEvent(sessionID, eventType, data string) {
	var eventData map[string]any
	if data != "" {
		if err := json.Unmarshal([]byte(data), &eventData); err != nil {
			log.Printf("WARNING: malformed agent_status event data in session %s (type=%s): %v", sessionID, eventType, err)
			return
		}
	}
	agentName, _ := eventData["agent"].(string)
	if agentName == "" {
		log.Printf("WARNING: agent_status event missing agent field in session %s (type=%s)", sessionID, eventType)
		return
	}

	switch eventType {
	case "agent_status:incomplete":
		detail, _ := eventData["detail"].(string)
		if err := d.store.UpdateAgentRunStatus(sessionID, agentName, "incomplete"); err != nil {
			log.Printf("ERROR: processAgentStatusEvent: failed to update agent %s to incomplete in session %s: %v", agentName, sessionID, err)
		}

		if err := d.store.LogEvent(store.SessionEvent{
			SessionID: sessionID,
			Type:      "agent_escalated",
			Data: mustJSON(map[string]string{
				"agent":  agentName,
				"reason": "incomplete",
				"detail": detail,
			}),
		}); err != nil {
			log.Printf("WARNING: processAgentStatusEvent: failed to log agent_escalated event in session %s: %v", sessionID, err)
		}

		// If the supervisor reports incomplete, escalate the session.
		if agentName == "supervisor" {
			if err := d.store.UpdateSessionStatus(sessionID, "needs_human_review"); err != nil {
				log.Printf("ERROR: processAgentStatusEvent: failed to escalate session %s to needs_human_review: %v", sessionID, err)
			} else {
				log.Printf("Session %s escalated to human review: supervisor reported incomplete", sessionID)
				d.stopAllBridgeAgents(sessionID, "supervisor reported incomplete")
				d.archiver.ArchiveTerminal(sessionID)
				d.terminateSandbox(d.startCtx, sessionID)
			}
		} else {
			// A specialist gave up. Wake the supervisor so it can decide whether
			// to respawn, hand off, or escalate — otherwise it will sleep on the
			// idle timer and escalate the whole run without attempting recovery.
			content := fmt.Sprintf("%s has finished with status=incomplete", agentName)
			if detail != "" {
				content += ". Detail: " + detail
			}
			msgID := uuid.New().String()
			msg := broker.Message{
				ID:          msgID,
				SessionID:   sessionID,
				SenderID:    agentName,
				RecipientID: "supervisor",
				Type:        broker.MessageStateChange,
				Content:     content,
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			_, _ = d.store.CreateMessage(store.Message{
				ID:          msgID,
				SessionID:   sessionID,
				SenderID:    agentName,
				RecipientID: "supervisor",
				Type:        string(broker.MessageStateChange),
				Content:     content,
				Urgent:      true,
			})
			_ = d.broker.Interrupt(sessionID, "supervisor", msg)
		}

	default:
		log.Printf("DEBUG: unhandled agent_status event %s for agent %s in session %s (log-only)", eventType, agentName, sessionID)
	}
}

func (d *Daemon) handleBridgeFailed(sessionID, agentName string, data map[string]any) {
	_ = d.store.UpdateAgentRunStatus(sessionID, agentName, "blocked")

	// Check if this was the last active agent.
	d.checkSessionStalled(sessionID)

	if agentName == "supervisor" {
		return
	}

	errorMsg, _ := data["error"].(string)
	content := agentName + " failed and was marked blocked"
	if errorMsg != "" {
		content += ". Error: " + errorMsg
	}

	msgID := uuid.New().String()
	msg := broker.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "supervisor",
		Type:        broker.MessageStateChange,
		Content:     content,
		Urgent:      true,
		Timestamp:   time.Now().UTC(),
	}

	// Persist to messages table so bridge-based supervisors can pull it.
	_, _ = d.store.CreateMessage(store.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "supervisor",
		Type:        string(broker.MessageStateChange),
		Content:     content,
		Urgent:      true,
	})

	// Urgent: supervisor should know about failures immediately.
	_ = d.broker.Interrupt(sessionID, "supervisor", msg)
}

func (d *Daemon) handleBridgeClarification(sessionID, agentName string, data map[string]any) {
	if agentName == "supervisor" {
		return // supervisor doesn't clarify to itself
	}

	question, _ := data["question"].(string)
	if question == "" {
		return
	}

	content := agentName + " needs clarification: " + question
	msgID := uuid.New().String()
	msg := broker.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "supervisor",
		Type:        broker.MessageInputNeeded,
		Content:     content,
		Timestamp:   time.Now().UTC(),
	}

	// Persist to messages table so bridge-based supervisors can pull it.
	_, _ = d.store.CreateMessage(store.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "supervisor",
		Type:        string(broker.MessageInputNeeded),
		Content:     content,
	})

	_ = d.broker.Send(sessionID, "supervisor", msg)
}

// resolveExitConditions returns the authoritative exit-condition list for the
// session plus the source it came from ("override" for a --exit-condition flag
// at run start, "config" for .belayer/config.yaml, or "none"). The PM gate
// validates these before marking the run complete; making them explicit in the
// spawn message keeps the PM from having to scan session history to find them.
func (d *Daemon) resolveExitConditions(sessionID string) ([]string, string) {
	// First: check run_initiated event for a per-run override.
	events, _ := d.store.QueryEvents(sessionID)
	for _, ev := range events {
		if ev.Type != "run_initiated" || ev.Data == "" {
			continue
		}
		var payload struct {
			ExitConditions []string `json:"exit_conditions"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &payload); err == nil && len(payload.ExitConditions) > 0 {
			return payload.ExitConditions, "override"
		}
		break // only the first run_initiated event is authoritative
	}

	// Fallback: read .belayer/config.yaml from the session's workspace.
	sess, err := d.store.GetSession(sessionID)
	if err != nil || sess.WorkspaceDir == "" {
		return nil, "none"
	}
	cfgPath := filepath.Join(sess.WorkspaceDir, ".belayer", "config.yaml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, "none"
	}
	var file struct {
		ExitConditions []string `yaml:"exit_conditions"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil || len(file.ExitConditions) == 0 {
		return nil, "none"
	}
	return file.ExitConditions, "config"
}

func (d *Daemon) handleBridgeCompletionRequested(sessionID, agentName string, data map[string]any) {
	summary, _ := data["summary"].(string)
	specArtifact, _ := data["spec_artifact"].(string)

	// Build context for the PM: spec location, artifact list, supervisor summary.
	artifacts, _ := d.store.ListArtifacts(sessionID)

	// Find the spec artifact if not explicitly provided.
	if specArtifact == "" {
		for _, a := range artifacts {
			if a.Kind == "spec" || a.Kind == "design-doc" || a.Kind == "design_doc" {
				specArtifact = a.Path
				break
			}
		}
	}

	// Build artifact summary for PM context.
	var artifactLines []string
	for _, a := range artifacts {
		line := fmt.Sprintf("- [%s] %s", a.Kind, a.Path)
		if a.Summary != "" {
			line += " — " + a.Summary
		}
		artifactLines = append(artifactLines, line)
	}
	artifactSummary := "(none registered)"
	if len(artifactLines) > 0 {
		artifactSummary = strings.Join(artifactLines, "\n")
	}

	specLine := "No spec artifact registered. Search the workspace for spec, design-doc, or README files."
	if specArtifact != "" {
		specLine = specArtifact
	}

	// Resolve exit conditions now so the PM receives them as an explicit
	// section instead of having to scan session history or reparse the config.
	exitConditions, exitSource := d.resolveExitConditions(sessionID)
	var exitBlock string
	switch exitSource {
	case "override":
		exitBlock = "Exit conditions for this run (per-run override, authoritative):\n"
	case "config":
		exitBlock = "Exit conditions for this run (from .belayer/config.yaml):\n"
	default:
		exitBlock = "Exit conditions for this run: none declared. Validate the spec only.\n"
	}
	if len(exitConditions) > 0 {
		for _, c := range exitConditions {
			exitBlock += "- " + c + "\n"
		}
	}

	// Build PM initial message with full context.
	pmMessage := fmt.Sprintf(
		`[System] The supervisor has signaled that all implementation work is complete. Your job is to verify.

Supervisor's summary:
%s

Spec artifact: %s

Registered artifacts:
%s

%s
Instructions:
1. Read the spec artifact (or find the spec in the workspace if none was registered).
2. Use git diff to see what changed during this run.
3. Walk through the spec section by section. For each requirement, find evidence in the code.
4. For each exit condition listed above, demand concrete evidence it holds.
5. Check for deferred work: TODO comments, placeholder implementations, empty test bodies.
6. Produce a structured verification report (Passed / Failed / Deferred).

If ALL spec items and exit conditions are satisfied: call belayer_approve_completion with your verification report.
If gaps exist: call belayer_reject_completion with the specific gaps so the supervisor can fix them.`,
		summary, specLine, artifactSummary, exitBlock,
	)

	// Auto-spawn the PM agent.
	go func() {
		_, err := d.spawnAgentInternal(agentSpawnRequest{
			SessionID: sessionID,
			Name:      "pm",
			Role:      "pm",
			Profile:   "default",
			Message:   pmMessage,
		})
		if err != nil {
			log.Printf("Failed to auto-spawn PM for session %s: %v", sessionID, err)
			_ = d.store.LogEvent(store.SessionEvent{
				SessionID: sessionID,
				Type:      "pm_spawn_failed",
				Data:      mustJSON(map[string]string{"error": err.Error()}),
			})
			// Notify supervisor that PM spawn failed.
			msg := broker.Message{
				ID:          uuid.New().String(),
				SessionID:   sessionID,
				SenderID:    "system",
				RecipientID: "supervisor",
				Type:        broker.MessageStateChange,
				Content:     "PM agent failed to spawn for completion verification. You may call belayer_request_completion again to retry.",
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			_, _ = d.store.CreateMessage(store.Message{
				ID:          msg.ID,
				SessionID:   sessionID,
				SenderID:    "system",
				RecipientID: "supervisor",
				Type:        string(broker.MessageStateChange),
				Content:     msg.Content,
				Urgent:      true,
			})
			_ = d.broker.Interrupt(sessionID, "supervisor", msg)
		} else {
			log.Printf("Auto-spawned PM agent for completion review in session %s", sessionID)
		}
	}()
}

func (d *Daemon) handleBridgeCompletionApproved(sessionID, agentName string, data map[string]any) {
	report, _ := data["verification_report"].(string)

	// Register the verification report as an artifact.
	_, _ = d.store.CreateArtifact(store.Artifact{
		SessionID: sessionID,
		Kind:      "verification-report",
		Path:      "(inline)",
		Producer:  agentName,
		Summary:   report[:min(len(report), 500)],
	})

	// Before flipping session state, surface any agents that were still mid-
	// work at approval time. This is a smell: the supervisor called
	// belayer_request_completion while other agents were still running, so PM
	// approval is about to kill unfinished work. Not fatal — the supervisor
	// may have intentionally raced a long-running peer — but worth logging
	// for post-mortems.
	if runs, err := d.store.ListAgentRuns(sessionID); err == nil {
		var busy []string
		for _, r := range runs {
			if r.Name == agentName || r.Name == "supervisor" {
				continue
			}
			switch r.Status {
			case "starting", "running", "pending_verification":
				busy = append(busy, fmt.Sprintf("%s=%s", r.Name, r.Status))
			}
		}
		if len(busy) > 0 {
			log.Printf("WARNING: session %s approved for completion while %d agent(s) non-idle; their work will be discarded: %v",
				sessionID, len(busy), busy)
			_ = d.store.LogEvent(store.SessionEvent{
				SessionID: sessionID,
				Type:      "completion_approved_with_busy_agents",
				Data: mustJSON(map[string]any{
					"approved_by": agentName,
					"busy_agents": busy,
				}),
			})
		}
	}

	// Mark session as complete.
	_ = d.store.UpdateSessionStatus(sessionID, "complete")
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "session_completed",
		Data: mustJSON(map[string]string{
			"approved_by": agentName,
			"report":      report[:min(len(report), 1000)],
		}),
	})

	// Shut down every bridge process in the session before tearing down the
	// sandbox. Otherwise supervisor (and any other live agents) keeps running
	// past approval, burns tokens, and can self-escalate the session back to
	// needs_human_review despite completion having already been approved.
	// Drain first so any final bridge events land in the store before the
	// archiver snapshots it.
	d.stopAllBridgeAgents(sessionID, "pm approved completion")
	d.archiver.ArchiveTerminal(sessionID)
	d.terminateSandbox(d.startCtx, sessionID)

	log.Printf("Session %s marked complete (approved by %s)", sessionID, agentName)
}

func (d *Daemon) handleBridgeCompletionRejected(sessionID, agentName string, data map[string]any) {
	report, _ := data["verification_report"].(string)
	gapList, _ := data["gap_list"].(string)

	// Register the rejection report as an artifact.
	_, _ = d.store.CreateArtifact(store.Artifact{
		SessionID: sessionID,
		Kind:      "verification-report",
		Path:      "(inline)",
		Producer:  agentName,
		Summary:   "REJECTED: " + report[:min(len(report), 450)],
	})

	// Count prior rejections to enforce the cycle limit.
	events, _ := d.store.QueryEvents(sessionID)
	rejectionCount := 0
	for _, evt := range events {
		if evt.Type == "bridge:completion_rejected" {
			rejectionCount++
		}
	}

	// rejectionCount is the count of prior bridge:completion_rejected events.
	// The current rejection is rejectionCount+1 (spec §3.7 requires positive integers).
	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "completion_rejected",
		Data: mustJSON(map[string]string{
			"rejected_by": agentName,
			"cycle":       fmt.Sprintf("%d/%d", rejectionCount+1, maxRejectionCycles),
		}),
	})

	if rejectionCount >= maxRejectionCycles {
		log.Printf("Session %s hit max rejection cycles (%d), escalating to operator", sessionID, maxRejectionCycles)
		_ = d.store.UpdateSessionStatus(sessionID, "needs_human_review")
		_ = d.store.LogEvent(store.SessionEvent{
			SessionID: sessionID,
			Type:      "completion_escalated",
			Data: mustJSON(map[string]string{
				"reason":     "max_rejection_cycles",
				"rejections": fmt.Sprintf("%d", rejectionCount),
			}),
		})
		// Drain live bridge processes before archiving so the archive snapshot
		// sees any final events they emit. Sandbox teardown happens last.
		d.stopAllBridgeAgents(sessionID, "max rejection cycles exceeded")
		d.archiver.ArchiveTerminal(sessionID)
		d.terminateSandbox(d.startCtx, sessionID)
		// Notify supervisor of escalation.
		msg := broker.Message{
			ID:          uuid.New().String(),
			SessionID:   sessionID,
			SenderID:    "system",
			RecipientID: "supervisor",
			Type:        broker.MessageStateChange,
			Content: fmt.Sprintf(
				"PM has rejected completion %d times (limit: %d). Run escalated to human review. "+
					"Stop attempting completion until a human operator intervenes.",
				rejectionCount, maxRejectionCycles,
			),
			Urgent:    true,
			Timestamp: time.Now().UTC(),
		}
		_, _ = d.store.CreateMessage(store.Message{
			ID: msg.ID, SessionID: sessionID, SenderID: "system",
			RecipientID: "supervisor", Type: string(broker.MessageStateChange),
			Content: msg.Content, Urgent: true,
		})
		_ = d.broker.Interrupt(sessionID, "supervisor", msg)
		return
	}

	// Send gap list to supervisor for remediation.
	content := fmt.Sprintf(
		"PM rejected completion (cycle %d/%d). Gaps found:\n\n%s\n\nAddress these gaps and call belayer_request_completion again when ready.",
		rejectionCount+1, maxRejectionCycles, gapList,
	)
	msgID := uuid.New().String()
	msg := broker.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "supervisor",
		Type:        broker.MessageStateChange,
		Content:     content,
		Urgent:      true,
		Timestamp:   time.Now().UTC(),
	}
	_, _ = d.store.CreateMessage(store.Message{
		ID: msgID, SessionID: sessionID, SenderID: agentName,
		RecipientID: "supervisor", Type: string(broker.MessageStateChange),
		Content: content, Urgent: true,
	})
	_ = d.broker.Interrupt(sessionID, "supervisor", msg)

	log.Printf("PM rejected completion for session %s (cycle %d/%d), gap list sent to supervisor",
		sessionID, rejectionCount+1, maxRejectionCycles)
}
