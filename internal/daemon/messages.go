package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/broker"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/google/uuid"
)

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

	// Reject messages to agents that have exited the session.
	if target, err := d.store.GetAgentRun(id, req.To); err == nil {
		if target.Status == "complete" || target.Status == "blocked" || target.Status == "incomplete" || target.Status == "exited" {
			writeJSON(w, http.StatusGone, map[string]string{
				"error": "agent '" + req.To + "' has exited (status: " + target.Status + "). Use belayer_spawn_agent to re-spawn with conversation history.",
			})
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

	// Persist to messages table for pull-based delivery (bridge agents poll this).
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

	if req.Interrupt {
		// Check if target is a bridge agent; if so, write to stdin directly.
		targetRun, runErr := d.store.GetAgentRun(id, req.To)
		if runErr == nil && targetRun.Transport == "bridge" {
			if bridgeErr := d.interruptBridgeAgent(id, req.To, msgID, from, req.Content); bridgeErr != nil {
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
	msg := broker.Message{
		ID:        uuid.New().String(),
		SessionID: id,
		SenderID:  from,
		Type:      broker.MessageType(req.Type),
		Content:   req.Content,
		Timestamp: time.Now().UTC(),
	}
	if err := d.broker.Broadcast(id, msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// TODO(v1): persist broadcast to messages table with one row per subscribed
	// agent so bridge agents can pull via PendingMessages. For now, broadcasts
	// are delivered only through the broker (push-based).

	data := mustJSON(map[string]any{
		"from":    from,
		"content": req.Content,
		"type":    req.Type,
		"sent_at": msg.Timestamp.Format(time.RFC3339Nano),
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
		runs, err := d.store.ListAgentRuns(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var messages []store.Message
		seen := map[string]struct{}{}
		for _, run := range runs {
			unacked, err := d.store.UnackedMessages(id, run.Name, "")
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
	if err := d.store.MarkAcknowledgedForSession(sessionID, messageID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged", "id": messageID})
}
