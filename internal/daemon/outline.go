package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// outlineSessionBlock is the compact session summary embedded in an outline response.
type outlineSessionBlock struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	LogLevel  string    `json:"log_level"`
	CreatedAt time.Time `json:"created_at"`
}

// outlineAgentEntry is a per-agent summary within an outline response.
type outlineAgentEntry struct {
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	LastActivity time.Time `json:"last_activity,omitempty"`
	ToolCalls    int       `json:"tool_calls"`
	Tokens       int64     `json:"tokens"`
	Cost         float64   `json:"cost"`
}

// outlinePhaseEntry records a phase transition detected from the event log.
type outlinePhaseEntry struct {
	Phase string    `json:"phase"`
	Since time.Time `json:"since"`
}

// outlineArtifactEntry is a compact artifact representation for the outline.
type outlineArtifactEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	Producer  string    `json:"producer"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// outlineResponse is the top-level response body for GET /sessions/{id}/outline.
type outlineResponse struct {
	Session     outlineSessionBlock    `json:"session"`
	Agents      []outlineAgentEntry    `json:"agents"`
	Artifacts   []outlineArtifactEntry `json:"artifacts"`
	Phases      []outlinePhaseEntry    `json:"phases"`
	FinalStatus string                 `json:"final_status"`
}

// phaseFromEventSuffix maps an agent_status or session_* event-type suffix to a
// canonical phase name. Returns ("", false) if the suffix is not recognised.
func phaseFromEventSuffix(suffix string) (string, bool) {
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
	}
	return "", false
}

// handleOutline serves GET /sessions/{id}/outline.
//
// It performs a single pass over all session events to derive per-agent
// activity metrics (last_activity, tool_calls, tokens) and the ordered
// phase timeline. Artifacts and agent runs are fetched via their own store
// methods.
func (d *Daemon) handleOutline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess, err := d.store.GetSession(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// --- agent runs: base list ---
	agentRuns, err := d.store.ListAgentRuns(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Build per-agent accumulators keyed by agent name.
	type agentAccum struct {
		toolCalls    int
		tokens       int64
		lastActivity time.Time
	}
	accums := make(map[string]*agentAccum, len(agentRuns))
	for _, ar := range agentRuns {
		accums[ar.Name] = &agentAccum{}
	}

	// --- artifacts ---
	storeArtifacts, err := d.store.ListArtifacts(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	artifacts := make([]outlineArtifactEntry, len(storeArtifacts))
	for i, a := range storeArtifacts {
		artifacts[i] = outlineArtifactEntry{
			ID:        a.ID,
			Kind:      a.Kind,
			Path:      a.Path,
			Producer:  a.Producer,
			Summary:   a.Summary,
			CreatedAt: a.CreatedAt,
		}
	}

	// --- single-pass over events for metrics + phases ---
	events, err := d.store.QueryEvents(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var phases []outlinePhaseEntry
	lastPhase := ""

	for _, evt := range events {
		// Per-agent activity: update last_activity for any event whose data has "agent".
		agentName := extractAgentName(evt.Data)
		if agentName != "unknown" {
			ac, known := accums[agentName]
			if !known {
				// Event references an agent not in the roster — ignore for accum purposes.
				ac = nil
			}
			if ac != nil {
				if evt.Timestamp.After(ac.lastActivity) {
					ac.lastActivity = evt.Timestamp
				}
				if evt.Type == "bridge:tool_started" {
					ac.toolCalls++
				}
				if evt.Type == "bridge:tool_completed" {
					ac.tokens += extractTokensFromData(evt.Data)
				}
			}
		}

		// Phase detection: agent_status:* and session_* prefixes.
		var suffix string
		switch {
		case strings.HasPrefix(evt.Type, "agent_status:"):
			suffix = strings.TrimPrefix(evt.Type, "agent_status:")
		case strings.HasPrefix(evt.Type, "session_"):
			suffix = strings.TrimPrefix(evt.Type, "session_")
		}
		if suffix != "" {
			if phase, ok := phaseFromEventSuffix(suffix); ok && phase != lastPhase {
				phases = append(phases, outlinePhaseEntry{Phase: phase, Since: evt.Timestamp})
				lastPhase = phase
			}
		}
	}

	// --- assemble agents list ---
	agentEntries := make([]outlineAgentEntry, len(agentRuns))
	for i, ar := range agentRuns {
		ac := accums[ar.Name]
		entry := outlineAgentEntry{
			Name:   ar.Name,
			Role:   ar.Role,
			Status: ar.Status,
			Cost:   0,
		}
		if ac != nil {
			entry.LastActivity = ac.lastActivity
			entry.ToolCalls = ac.toolCalls
			entry.Tokens = ac.tokens
		}
		agentEntries[i] = entry
	}

	// Ensure phases is non-nil for JSON encoding ([] not null).
	if phases == nil {
		phases = []outlinePhaseEntry{}
	}

	resp := outlineResponse{
		Session: outlineSessionBlock{
			ID:        sess.ID,
			Status:    sess.Status,
			LogLevel:  sess.LogLevel,
			CreatedAt: sess.CreatedAt,
		},
		Agents:      agentEntries,
		Artifacts:   artifacts,
		Phases:      phases,
		FinalStatus: sess.Status,
	}

	writeJSON(w, http.StatusOK, resp)
}

// extractTokensFromData parses a bridge:tool_completed event data payload and
// attempts to extract a token count. It looks for:
//   - top-level "tokens" field (integer)
//   - nested "usage.total_tokens" field (integer)
//
// Returns 0 if neither is present or if parsing fails.
func extractTokensFromData(data string) int64 {
	if data == "" {
		return 0
	}
	var payload struct {
		Tokens int64 `json:"tokens"`
		Usage  struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return 0
	}
	if payload.Tokens > 0 {
		return payload.Tokens
	}
	return payload.Usage.TotalTokens
}

// artifactsToOutline is retained for symmetry but the inline conversion above
// avoids a separate allocation pass. Kept as a named helper for readability if
// needed by future callers.
func artifactToOutlineEntry(a store.Artifact) outlineArtifactEntry {
	return outlineArtifactEntry{
		ID:        a.ID,
		Kind:      a.Kind,
		Path:      a.Path,
		Producer:  a.Producer,
		Summary:   a.Summary,
		CreatedAt: a.CreatedAt,
	}
}
