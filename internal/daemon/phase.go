package daemon

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// phaseResponse is the JSON body returned by GET /sessions/{id}/phase.
type phaseResponse struct {
	Phase string     `json:"phase"`
	Since *time.Time `json:"since,omitempty"`
}

// handlePhase derives the current session phase from the most recent
// agent_status:* or session_* event and returns it as JSON.
//
// GET /sessions/{id}/phase
//
// Response: {"phase":"implement","since":"2026-04-19T12:34:56Z"}
// If no phase event exists: {"phase":"unknown"} (since omitted).
// 404 if session not found.
func (d *Daemon) handlePhase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Verify the session exists.
	if _, err := d.store.GetSession(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	events, err := d.store.QueryEvents(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Walk backward from most recent to find the latest phase event.
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		phase, ok := derivePhase(ev.Type)
		if !ok {
			continue
		}
		ts := ev.Timestamp
		writeJSON(w, http.StatusOK, phaseResponse{
			Phase: phase,
			Since: &ts,
		})
		return
	}

	// No phase event found.
	writeJSON(w, http.StatusOK, phaseResponse{Phase: "unknown"})
}

// derivePhase maps an event type to a canonical phase name.
// It returns (phase, true) when the event type is a phase event
// (agent_status:* or session_*), or ("", false) otherwise.
func derivePhase(eventType string) (string, bool) {
	var suffix string
	switch {
	case strings.HasPrefix(eventType, "agent_status:"):
		suffix = strings.TrimPrefix(eventType, "agent_status:")
	case strings.HasPrefix(eventType, "session_"):
		suffix = strings.TrimPrefix(eventType, "session_")
	default:
		return "", false
	}

	suffix = strings.ToLower(suffix)

	// Map known suffixes to canonical phases.
	// Unknown suffixes are returned as-is only for agent_status: events;
	// generic session_* lifecycle events (e.g. session_created,
	// session_status_changed) that don't carry phase semantics are excluded
	// by the default case so they don't pollute the phase derivation.
	switch suffix {
	case "discovering":
		return "discover", true
	case "planning":
		return "plan", true
	case "implementing":
		return "implement", true
	case "reviewing":
		return "review", true
	case "finished":
		return "finish", true
	default:
		// Only propagate unrecognised suffixes for agent_status: events
		// (vendor-specific phases). For session_* events, skip — they are
		// lifecycle signals, not phase transitions.
		if strings.HasPrefix(eventType, "agent_status:") {
			return suffix, true
		}
		return "", false
	}
}
