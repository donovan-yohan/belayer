package daemon

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/donovan-yohan/belayer/internal/store"
)

// handleConversation serves GET /sessions/{id}/conversation.
//
// Query params:
//   - between=a,b — returns messages in either direction between the two agents.
//   - agent=a     — returns messages where a is either sender or recipient.
//   - (none)      — returns all messages in the session.
//   - both params present → 400 error.
func (d *Daemon) handleConversation(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	// 404 if session not found.
	if _, err := d.store.GetSession(sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	betweenParam := r.URL.Query().Get("between")
	agentParam := r.URL.Query().Get("agent")

	// Both params present → 400.
	if betweenParam != "" && agentParam != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use between or agent, not both"})
		return
	}

	var msgs []store.Message
	var err error

	switch {
	case betweenParam != "":
		parts := strings.Split(betweenParam, ",")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "between requires exactly two non-empty agent names"})
			return
		}
		agentA := strings.TrimSpace(parts[0])
		agentB := strings.TrimSpace(parts[1])
		msgs, err = d.store.ListMessagesBetween(sessionID, agentA, agentB)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

	case agentParam != "":
		all, listErr := d.store.ListMessagesInSession(sessionID)
		if listErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": listErr.Error()})
			return
		}
		msgs = []store.Message{}
		for _, m := range all {
			if m.SenderID == agentParam || m.RecipientID == agentParam {
				msgs = append(msgs, m)
			}
		}

	default:
		msgs, err = d.store.ListMessagesInSession(sessionID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// X-Event-Count=0 for aggregate endpoints — conversation is a message list, not a raw event list.
	d.writeEventHeaders(w, sessionID, 0)
	writeJSON(w, http.StatusOK, msgs)
}
