package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/google/uuid"
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
	_ = d.store.UpdateAgentRunStatus(sessionID, agentName, "complete")

	// No auto-generated message to the planner here. The specialist should
	// have sent its own report via belayer_send_message before exiting.
	// If the bridge crashes without a clean exit, watchBridgeExit handles
	// the notification. Auto-generating a state_change here causes duplicate
	// messages that the planner dismisses as noise.
}

func (d *Daemon) handleBridgeFailed(sessionID, agentName string, data map[string]any) {
	_ = d.store.UpdateAgentRunStatus(sessionID, agentName, "blocked")

	if agentName == "planner" {
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
		RecipientID: "planner",
		Type:        broker.MessageStateChange,
		Content:     content,
		Urgent:      true,
		Timestamp:   time.Now().UTC(),
	}

	// Persist to messages table so bridge-based planners can pull it.
	_, _ = d.store.CreateMessage(store.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "planner",
		Type:        string(broker.MessageStateChange),
		Content:     content,
		Urgent:      true,
	})

	// Urgent: planner should know about failures immediately.
	_ = d.broker.Interrupt(sessionID, "planner", msg)
}

func (d *Daemon) handleBridgeClarification(sessionID, agentName string, data map[string]any) {
	if agentName == "planner" {
		return // planner doesn't clarify to itself
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
		RecipientID: "planner",
		Type:        broker.MessageInputNeeded,
		Content:     content,
		Timestamp:   time.Now().UTC(),
	}

	// Persist to messages table so bridge-based planners can pull it.
	_, _ = d.store.CreateMessage(store.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "planner",
		Type:        string(broker.MessageInputNeeded),
		Content:     content,
	})

	_ = d.broker.Send(sessionID, "planner", msg)
}

func (d *Daemon) handleBridgeCompletionRequested(sessionID, agentName string, data map[string]any) {
	summary, _ := data["summary"].(string)
	specArtifact, _ := data["spec_artifact"].(string)

	// Build context for the PM: spec location, artifact list, planner summary.
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

	// Build PM initial message with full context.
	pmMessage := fmt.Sprintf(
		`[System] The planner has signaled that all implementation work is complete. Your job is to verify.

Planner's summary:
%s

Spec artifact: %s

Registered artifacts:
%s

Instructions:
1. Read the spec artifact (or find the spec in the workspace if none was registered).
2. Use git diff to see what changed during this run.
3. Walk through the spec section by section. For each requirement, find evidence in the code.
4. Check for deferred work: TODO comments, placeholder implementations, empty test bodies.
5. Produce a structured verification report (Passed / Failed / Deferred).

If ALL spec items are satisfied: call belayer_approve_completion with your verification report.
If gaps exist: call belayer_reject_completion with the specific gaps so the planner can fix them.`,
		summary, specLine, artifactSummary,
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
			// Notify planner that PM spawn failed.
			msg := broker.Message{
				ID:          uuid.New().String(),
				SessionID:   sessionID,
				SenderID:    "system",
				RecipientID: "planner",
				Type:        broker.MessageStateChange,
				Content:     "PM agent failed to spawn for completion verification. You may call belayer_request_completion again to retry.",
				Urgent:      true,
				Timestamp:   time.Now().UTC(),
			}
			_, _ = d.store.CreateMessage(store.Message{
				ID:          msg.ID,
				SessionID:   sessionID,
				SenderID:    "system",
				RecipientID: "planner",
				Type:        string(broker.MessageStateChange),
				Content:     msg.Content,
				Urgent:      true,
			})
			_ = d.broker.Interrupt(sessionID, "planner", msg)
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

	_ = d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "completion_rejected",
		Data: mustJSON(map[string]string{
			"rejected_by": agentName,
			"cycle":       fmt.Sprintf("%d/%d", rejectionCount, maxRejectionCycles),
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
		// Notify planner of escalation.
		msg := broker.Message{
			ID:          uuid.New().String(),
			SessionID:   sessionID,
			SenderID:    "system",
			RecipientID: "planner",
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
			RecipientID: "planner", Type: string(broker.MessageStateChange),
			Content: msg.Content, Urgent: true,
		})
		_ = d.broker.Interrupt(sessionID, "planner", msg)
		return
	}

	// Send gap list to planner for remediation.
	content := fmt.Sprintf(
		"PM rejected completion (cycle %d/%d). Gaps found:\n\n%s\n\nAddress these gaps and call belayer_request_completion again when ready.",
		rejectionCount, maxRejectionCycles, gapList,
	)
	msgID := uuid.New().String()
	msg := broker.Message{
		ID:          msgID,
		SessionID:   sessionID,
		SenderID:    agentName,
		RecipientID: "planner",
		Type:        broker.MessageStateChange,
		Content:     content,
		Urgent:      true,
		Timestamp:   time.Now().UTC(),
	}
	_, _ = d.store.CreateMessage(store.Message{
		ID: msgID, SessionID: sessionID, SenderID: agentName,
		RecipientID: "planner", Type: string(broker.MessageStateChange),
		Content: content, Urgent: true,
	})
	_ = d.broker.Interrupt(sessionID, "planner", msg)

	log.Printf("PM rejected completion for session %s (cycle %d/%d), gap list sent to planner",
		sessionID, rejectionCount, maxRejectionCycles)
}
