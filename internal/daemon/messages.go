package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/google/uuid"
)

func (d *Daemon) agentRunByName(sessionID, name string) (store.AgentRun, bool) {
	run, err := d.store.GetAgentRun(sessionID, name)
	if err != nil {
		return store.AgentRun{}, false
	}
	return run, true
}

func (d *Daemon) mainAgentNames(sessionID, exclude string) ([]string, error) {
	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(runs))
	for _, run := range runs {
		if !agentRunIsMain(run) {
			continue
		}
		if exclude != "" && run.Name == exclude {
			continue
		}
		names = append(names, run.Name)
	}
	return names, nil
}

type sendMessageRequest struct {
	To        string `json:"to"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	Interrupt bool   `json:"interrupt"`
	From      string `json:"from,omitempty"`
}

type broadcastMessageRequest struct {
	Content string `json:"content"`
	Type    string `json:"type"`
	From    string `json:"from,omitempty"`
}

func (d *Daemon) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to is required"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	if fromRun, ok := d.agentRunByName(id, req.From); ok && agentRunIsSide(fromRun) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "side agents have no outbox"})
		return
	}

	// Reject messages to agents that have exited the session, except completed
	// side agents with a Hermes session: those are dormant resumable talent and
	// direct mail is the wake signal.
	if target, err := d.store.GetAgentRun(id, req.To); err == nil {
		if agentRunStatusIsTerminal(target.Status) {
			if agentRunIsWakeableDormantSide(target) {
				msgID, from, wakeErr := d.wakeDormantSideFromMessage(id, target, req)
				if wakeErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": wakeErr.Error()})
					return
				}
				data := mustJSON(map[string]any{
					"id":        msgID,
					"to":        req.To,
					"from":      from,
					"content":   req.Content,
					"type":      req.Type,
					"interrupt": req.Interrupt,
					"delivery":  "wake_spawn",
					"sent_at":   time.Now().UTC().Format(time.RFC3339Nano),
				})
				if err := d.store.LogEvent(store.SessionEvent{SessionID: id, Type: "message_sent", Data: data}); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusCreated, map[string]string{"id": msgID})
				return
			}
			writeJSON(w, http.StatusGone, map[string]string{
				"error": "agent '" + req.To + "' has exited (status: " + target.Status + "). Use belayer_spawn_agent to re-spawn with conversation history.",
			})
			return
		}
		if agentRunIsSide(target) && !req.Interrupt {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sides have no inbox; use interrupt only"})
			return
		}
	}

	msgID := uuid.New().String()
	from := req.From
	if from == "" {
		from = "operator"
	}
	msg := broker.Message{
		ID:          msgID,
		SessionID:   id,
		SenderID:    from,
		RecipientID: req.To,
		Type:        broker.MessageType(req.Type),
		Content:     req.Content,
		Urgent:      req.Interrupt,
		Timestamp:   time.Now().UTC(),
	}

	persistToMailbox := true
	if target, ok := d.agentRunByName(id, req.To); ok && agentRunIsSide(target) && req.Interrupt {
		// Side agents only have the stdin interrupt surface. Do not create a
		// queued/unacked mailbox row that the side can never poll or ack.
		persistToMailbox = false
		msg.ID = ""
	}

	// Persist to messages table for pull-based delivery when the recipient has a mailbox.
	if persistToMailbox {
		if _, err := d.store.CreateMessage(store.Message{
			ID:          msgID,
			SessionID:   id,
			SenderID:    from,
			RecipientID: req.To,
			Type:        req.Type,
			Content:     req.Content,
			Urgent:      req.Interrupt,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("persist message: %v", err)})
			return
		}
	}

	if req.Interrupt {
		// Check if target is a bridge agent; if so, write to stdin directly.
		targetRun, runErr := d.store.GetAgentRun(id, req.To)
		if runErr == nil && targetRun.Transport == "bridge" {
			if bridgeErr := d.interruptBridgeAgent(id, req.To, msg.ID, from, req.Content); bridgeErr != nil {
				// Bridge proc not tracked (e.g. already exited) — fall back to broker
				// so the message is still delivered if anything is still subscribed.
				_ = d.broker.Interrupt(id, req.To, msg)
			}
		} else {
			// Fallback to broker for agents without a tracked bridge proc or unknown transport.
			if err := d.broker.Interrupt(id, req.To, msg); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	} else {
		// Non-urgent: bridge agents pull via HTTP; broker is a no-op fallback.
		_ = d.broker.Send(id, req.To, msg)
	}

	data := mustJSON(map[string]any{
		"id":        msgID,
		"to":        req.To,
		"from":      from,
		"content":   req.Content,
		"type":      req.Type,
		"interrupt": req.Interrupt,
		"sent_at":   msg.Timestamp.Format(time.RFC3339Nano),
	})
	if err := d.store.LogEvent(store.SessionEvent{SessionID: id, Type: "message_sent", Data: data}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID})
}

func agentRunIsWakeableDormantSide(run store.AgentRun) bool {
	return agentRunIsSide(run) && run.Status == "complete" && run.HermesSessionID != ""
}

func (d *Daemon) wakeDormantSideFromMessage(sessionID string, target store.AgentRun, req sendMessageRequest) (string, string, error) {
	msgID := uuid.New().String()
	from := req.From
	if from == "" {
		from = "operator"
	}

	wakeMessage := fmt.Sprintf("Resume from your prior Hermes conversation. New message from %s:\n\n%s", from, req.Content)
	_, err := d.spawnAgentInternal(agentSpawnRequest{
		SessionID:       sessionID,
		Name:            target.Name,
		Role:            target.Role,
		Kind:            target.Kind,
		Profile:         target.Profile,
		Repo:            target.RepoScope,
		Workdir:         target.Workdir,
		Branch:          target.Branch,
		HermesSessionID: target.HermesSessionID,
		Message:         wakeMessage,
	})
	if err != nil {
		return "", "", fmt.Errorf("wake dormant side %q: %w", target.Name, err)
	}
	return msgID, from, nil
}

func (d *Daemon) handleBroadcastMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req broadcastMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	from := req.From
	if from == "" {
		from = "operator"
	}
	if fromRun, ok := d.agentRunByName(id, from); ok && agentRunIsSide(fromRun) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "side agents have no outbox"})
		return
	}
	msg := broker.Message{
		ID:        uuid.New().String(),
		SessionID: id,
		SenderID:  from,
		Type:      broker.MessageType(req.Type),
		Content:   req.Content,
		Timestamp: time.Now().UTC(),
	}
	recipients, err := d.mainAgentNames(id, from)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, recipient := range recipients {
		msgID := uuid.New().String()
		msgCopy := msg
		msgCopy.ID = msgID
		msgCopy.RecipientID = recipient
		if _, err := d.store.CreateMessage(store.Message{
			ID:          msgID,
			SessionID:   id,
			SenderID:    from,
			RecipientID: recipient,
			Type:        req.Type,
			Content:     req.Content,
			Urgent:      false,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("persist broadcast message: %v", err)})
			return
		}
		if err := d.broker.Send(id, recipient, msgCopy); err != nil {
			log.Printf("WARNING: broadcast delivery to %s in session %s failed: %v", recipient, id, err)
		}
	}

	data := mustJSON(map[string]any{
		"from":       from,
		"content":    req.Content,
		"type":       req.Type,
		"recipients": recipients,
		"sent_at":    msg.Timestamp.Format(time.RFC3339Nano),
	})
	if err := d.store.LogEvent(store.SessionEvent{SessionID: id, Type: "message_broadcast", Data: data}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "broadcast"})
}

func (d *Daemon) handleListMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	forAgent := r.URL.Query().Get("for")
	state := r.URL.Query().Get("state")
	if forAgent != "" {
		// Pull-based delivery for bridge agents.
		afterID := r.URL.Query().Get("after")
		pending := r.URL.Query().Get("pending") == "true"
		if run, ok := d.agentRunByName(id, forAgent); ok && agentRunIsSide(run) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sides have no inbox"})
			return
		}

		var (
			messages []store.Message
			err      error
		)
		if state == "unacked" {
			messages, err = d.store.UnackedMessages(id, forAgent, afterID)
		} else {
			messages, err = d.store.PendingMessages(id, forAgent, afterID)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Mark as delivered so they are not returned on the next poll.
		if pending && len(messages) > 0 {
			ids := make([]string, 0, len(messages))
			for _, msg := range messages {
				ids = append(ids, msg.ID)
			}
			if err := d.store.MarkDelivered(ids...); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, messages)
		return
	}

	if state == "unacked" {
		recipients, err := d.mainAgentNames(id, "")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var messages []store.Message
		seen := map[string]struct{}{}
		for _, recipient := range recipients {
			unacked, err := d.store.UnackedMessages(id, recipient, "")
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, msg := range unacked {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
				messages = append(messages, msg)
			}
		}
		writeJSON(w, http.StatusOK, messages)
		return
	}

	// Original behavior: list all message_ events from the event log.
	events, err := d.store.QueryEvents(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var messages []store.SessionEvent
	for _, e := range events {
		if strings.HasPrefix(e.Type, "message_") {
			messages = append(messages, e)
		}
	}
	if messages == nil {
		messages = []store.SessionEvent{}
	}

	writeJSON(w, http.StatusOK, messages)
}

func (d *Daemon) handleAckMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	messageID := strings.TrimSpace(r.PathValue("mid"))
	if messageID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message id is required"})
		return
	}
	msgs, err := d.store.ListMessagesInSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var recipient string
	for _, msg := range msgs {
		if msg.ID == messageID {
			recipient = msg.RecipientID
			break
		}
	}
	if recipient == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	if run, ok := d.agentRunByName(sessionID, recipient); ok && agentRunIsSide(run) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sides have no ack state"})
		return
	}
	if err := d.store.MarkAcknowledgedForRecipient(sessionID, recipient, messageID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged", "id": messageID})
}
