package daemon

import (
	"encoding/json"
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

	var err error
	if req.Interrupt {
		err = d.broker.Interrupt(id, req.To, msg)
	} else {
		err = d.broker.Send(id, req.To, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
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
