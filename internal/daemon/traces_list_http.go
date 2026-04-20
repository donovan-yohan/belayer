package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// traceFragmentEntry is the JSON shape returned by handleListTraces.
// The server-side filesystem path is intentionally omitted: clients address
// fragments via (agent, fragment) through /sessions/{id}/trace/{agent}/{fragment},
// and leaking the daemon's host layout over the API serves no purpose.
type traceFragmentEntry struct {
	Agent      string    `json:"agent"`
	Fragment   string    `json:"fragment"`
	Size       int64     `json:"size"`
	Compressed bool      `json:"compressed"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// handleListTraces lists trace fragment files for a session.
//
// GET /sessions/{id}/traces
//
// Returns 404 if the session is not found or the session log_level is not
// "trace". Returns a JSON array of traceFragmentEntry objects (empty array
// when no fragments exist yet).
func (d *Daemon) handleListTraces(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.LogLevel != LogLevelTrace {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace data only available at trace tier"})
		return
	}

	sessionTraceDir := filepath.Join(d.traceBase, sessionID)

	agentDirs, err := os.ReadDir(sessionTraceDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []traceFragmentEntry{})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read trace dir: " + err.Error()})
		return
	}

	var out []traceFragmentEntry
	for _, agentDir := range agentDirs {
		if !agentDir.IsDir() {
			continue
		}
		agentName := agentDir.Name()
		agentFragDir := filepath.Join(sessionTraceDir, agentName)

		files, err := os.ReadDir(agentFragDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			// Accept .jsonl and .jsonl.zst only.
			var isZst bool
			var baseName string
			if strings.HasSuffix(name, ".jsonl.zst") {
				isZst = true
				baseName = strings.TrimSuffix(name, ".jsonl.zst")
			} else if strings.HasSuffix(name, ".jsonl") {
				isZst = false
				baseName = strings.TrimSuffix(name, ".jsonl")
			} else {
				continue
			}

			info, err := f.Info()
			if err != nil {
				continue
			}
			out = append(out, traceFragmentEntry{
				Agent:      agentName,
				Fragment:   baseName,
				Size:       info.Size(),
				Compressed: isZst,
				UpdatedAt:  info.ModTime().UTC(),
			})
		}
	}
	if out == nil {
		out = []traceFragmentEntry{}
	}
	writeJSON(w, http.StatusOK, out)
}
