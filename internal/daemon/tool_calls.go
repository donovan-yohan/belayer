package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// toolCallEntry is one item in the GET /sessions/{id}/tool-calls response.
type toolCallEntry struct {
	Agent      string `json:"agent"`
	Tool       string `json:"tool"`
	Path       string `json:"path"`
	DurationMS int64  `json:"duration_ms"`
	Status     string `json:"status"`
	At         string `json:"at"`
	ID         string `json:"id"`
}

// toolStartedData holds the fields we care about from a bridge:tool_started event.
type toolStartedData struct {
	Agent      string `json:"agent"`
	Tool       string `json:"tool"`
	Name       string `json:"name"` // alternative field name for tool
	Path       string `json:"path"`
	ToolCallID string `json:"tool_call_id"`
}

// toolCompletedData holds the fields we care about from a bridge:tool_completed event.
type toolCompletedData struct {
	Agent      string `json:"agent"`
	ToolCallID string `json:"tool_call_id"`
	Status     string `json:"status"`
}

// handleToolCalls serves GET /sessions/{id}/tool-calls.
//
// It scans all events for the session and pairs each bridge:tool_started with
// its corresponding bridge:tool_completed. Pairing is first attempted by
// agent + tool_call_id; if tool_call_id is absent, pairing falls back to
// agent + insertion order. Unmatched started events are emitted with
// status:"pending" and duration_ms:0.
//
// The response is a JSON array ordered ascending by started-event timestamp.
func (d *Daemon) handleToolCalls(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, err := d.store.GetSession(id)
	if err != nil {
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

	type pendingStart struct {
		eventID   int64
		timestamp time.Time
		data      toolStartedData
		// orderIndex is the per-agent sequence number among started events,
		// used for fallback (no tool_call_id) pairing.
		orderIndex int
	}

	// keyed by "agent/tool_call_id" when tool_call_id is present.
	byCallID := map[string]*pendingStart{}
	// keyed by "agent", slice of unmatched starts in order.
	byAgent := map[string][]*pendingStart{}

	// completions keyed the same way.
	type completionInfo struct {
		timestamp time.Time
		status    string
	}
	completionsByCallID := map[string]completionInfo{}
	// completions without a call_id, keyed by agent, in order.
	completionsByAgent := map[string][]completionInfo{}

	// First pass: collect all started and completed events.
	for _, evt := range events {
		switch evt.Type {
		case "bridge:tool_started":
			var d toolStartedData
			_ = json.Unmarshal([]byte(evt.Data), &d)
			tool := d.Tool
			if tool == "" {
				tool = d.Name
			}
			d.Tool = tool

			ps := &pendingStart{
				eventID:   evt.ID,
				timestamp: evt.Timestamp,
				data:      d,
			}

			if d.ToolCallID != "" {
				key := d.Agent + "/" + d.ToolCallID
				byCallID[key] = ps
			} else {
				byAgent[d.Agent] = append(byAgent[d.Agent], ps)
				ps.orderIndex = len(byAgent[d.Agent]) - 1
			}

		case "bridge:tool_completed":
			var c toolCompletedData
			_ = json.Unmarshal([]byte(evt.Data), &c)

			ci := completionInfo{
				timestamp: evt.Timestamp,
				status:    c.Status,
			}
			if ci.status == "" {
				ci.status = "ok"
			}

			if c.ToolCallID != "" {
				key := c.Agent + "/" + c.ToolCallID
				completionsByCallID[key] = ci
			} else {
				completionsByAgent[c.Agent] = append(completionsByAgent[c.Agent], ci)
			}
		}
	}

	// Second pass: emit entries for every started event in order.
	// We iterate events again to preserve timestamp order.
	type startRecord struct {
		eventID   int64
		timestamp time.Time
		agent     string
		tool      string
		path      string
		callID    string
	}

	var starts []startRecord
	// Track per-agent index counters for fallback matching.
	agentFallbackIdx := map[string]int{}

	for _, evt := range events {
		if evt.Type != "bridge:tool_started" {
			continue
		}
		var sd toolStartedData
		_ = json.Unmarshal([]byte(evt.Data), &sd)
		tool := sd.Tool
		if tool == "" {
			tool = sd.Name
		}
		starts = append(starts, startRecord{
			eventID:   evt.ID,
			timestamp: evt.Timestamp,
			agent:     sd.Agent,
			tool:      tool,
			path:      sd.Path,
			callID:    sd.ToolCallID,
		})
		_ = agentFallbackIdx // suppress unused warning; used below
	}

	result := make([]toolCallEntry, 0, len(starts))
	// Per-agent counters for fallback (no tool_call_id) matching.
	agentMatchIdx := map[string]int{}

	for _, sr := range starts {
		entry := toolCallEntry{
			Agent:      sr.agent,
			Tool:       sr.tool,
			Path:       sr.path,
			DurationMS: 0,
			Status:     "pending",
			At:         sr.timestamp.UTC().Format(time.RFC3339Nano),
			ID:         fmt.Sprintf("%d", sr.eventID),
		}

		if sr.callID != "" {
			// Pair by agent + tool_call_id.
			key := sr.agent + "/" + sr.callID
			if ci, ok := completionsByCallID[key]; ok {
				entry.DurationMS = ci.timestamp.Sub(sr.timestamp).Milliseconds()
				entry.Status = ci.status
			}
		} else {
			// Pair by agent + order.
			idx := agentMatchIdx[sr.agent]
			cis := completionsByAgent[sr.agent]
			if idx < len(cis) {
				ci := cis[idx]
				entry.DurationMS = ci.timestamp.Sub(sr.timestamp).Milliseconds()
				entry.Status = ci.status
			}
			agentMatchIdx[sr.agent] = idx + 1
		}

		result = append(result, entry)
	}

	// X-Event-Count=0 for aggregate endpoints — tool-calls is a derived view, not a raw event list.
	d.writeEventHeaders(w, id, 0)
	writeJSON(w, http.StatusOK, result)
}
