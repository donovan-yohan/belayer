package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"github.com/donovan-yohan/belayer/internal/store"
)

// sandboxDir returns the path to the sandbox directory for a session.
// Convention: ~/.belayer/sandboxes/<sessionID>/
func sandboxDir(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("determine sandbox directory: user home directory unavailable: %w", err)
	}
	return filepath.Join(home, ".belayer", "sandboxes", sessionID), nil
}

// toolResult is the JSON response for a tool execution.
type toolResult struct {
	Output     string `json:"output"`
	Stderr     string `json:"stderr,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Target     string `json:"target"`
}

// handleRegisterTool registers a ToolSpec for a session.
// POST /sessions/{id}/tools
// Body: agent.ToolSpec JSON
func (d *Daemon) handleRegisterTool(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	// Verify the session exists.
	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	var spec agent.ToolSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if spec.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if spec.Exec.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exec.target is required"})
		return
	}
	if !agent.ValidTargets[spec.Exec.Target] {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("invalid exec.target %q (must be: agent, workbench, infra, host)", spec.Exec.Target),
		})
		return
	}
	if spec.Exec.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exec.command is required"})
		return
	}

	d.toolsMu.Lock()
	// Replace existing tool with same name if present.
	existing := d.tools[sessionID]
	updated := make([]agent.ToolSpec, 0, len(existing)+1)
	replaced := false
	for _, t := range existing {
		if t.Name == spec.Name {
			updated = append(updated, spec)
			replaced = true
		} else {
			updated = append(updated, t)
		}
	}
	if !replaced {
		updated = append(updated, spec)
	}
	d.tools[sessionID] = updated
	d.toolsMu.Unlock()

	d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "tool_registered",
		Data:      mustJSON(map[string]string{"tool": spec.Name, "target": spec.Exec.Target}),
	})

	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered", "tool": spec.Name})
}

// handleListTools returns the registered tools for a session.
// GET /sessions/{id}/tools
func (d *Daemon) handleListTools(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	d.toolsMu.RLock()
	tools := d.tools[sessionID]
	d.toolsMu.RUnlock()

	if tools == nil {
		tools = []agent.ToolSpec{}
	}
	writeJSON(w, http.StatusOK, tools)
}

// handleExecuteTool executes a named tool for a session and logs the result.
// POST /sessions/{id}/tools/{name}
// Body: {"key": "value", ...} — input map passed to the tool
// Query params: ?agent=<name> — optional calling agent identifier for audit
func (d *Daemon) handleExecuteTool(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	toolName := r.PathValue("name")
	callingAgent := r.URL.Query().Get("agent")

	// Verify the session exists.
	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	// Look up the tool spec.
	d.toolsMu.RLock()
	var spec agent.ToolSpec
	found := false
	for _, t := range d.tools[sessionID] {
		if t.Name == toolName {
			spec = t
			found = true
			break
		}
	}
	d.toolsMu.RUnlock()

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("tool %q not registered for session %s", toolName, sessionID),
		})
		return
	}

	// Parse the input map.
	var input map[string]string
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: expected JSON object"})
		return
	}
	if input == nil {
		input = map[string]string{}
	}

	// Resolve sandbox directory.
	sandboxPath, err := sandboxDir(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Execute the tool.
	executor := &agent.Executor{SandboxDir: sandboxPath}
	result, err := executor.Execute(r.Context(), spec, input)
	if err != nil {
		var renderErr *agent.RenderError
		if errors.As(err, &renderErr) {
			// Bad input: template references a key not present in the input map.
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "tool execution timed out"})
			return
		}
		// Execution infrastructure error (e.g. compose file missing, process can't start).
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Log the tool_executed event for audit trail.
	logData := map[string]any{
		"tool":          toolName,
		"target":        spec.Exec.Target,
		"input":         input,
		"exit_code":     result.ExitCode,
		"duration_ms":   result.DurationMS,
		"calling_agent": callingAgent,
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	// Include truncated output in the event (cap at 4 KB for the event log).
	output := result.Stdout
	if len(output) > 4096 {
		output = output[:4096] + "... [truncated]"
	}
	logData["output"] = output

	d.store.LogEvent(store.SessionEvent{
		SessionID: sessionID,
		Type:      "tool_executed",
		Data:      mustJSON(logData),
	})

	writeJSON(w, http.StatusOK, toolResult{
		Output:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		Target:     spec.Exec.Target,
	})
}
