package daemon

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// transcriptEntry is the JSON shape returned by handleListTranscripts.
type transcriptEntry struct {
	Agent     string    `json:"agent"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// transcriptDir returns the directory under workspace that holds transcript
// files for a given session.
//
//	<workspace>/.belayer/runs/<session>/transcripts/
func transcriptDir(workspaceDir, sessionID string) string {
	return filepath.Join(workspaceDir, ".belayer", "runs", sessionID, "transcripts")
}

// handleListTranscripts lists transcript files for a session.
//
// GET /sessions/{id}/transcripts
//
// Returns 404 if the session is not found or the session log_level is below
// verbose. Returns a JSON array of transcriptEntry objects (empty array when no
// transcripts exist yet).
func (d *Daemon) handleListTranscripts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if logLevelRank(sess.LogLevel) < logLevelRank(LogLevelVerbose) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcripts only available at verbose or trace tier"})
		return
	}

	dir := transcriptDir(sess.WorkspaceDir, sessionID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []transcriptEntry{})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read transcripts dir: " + err.Error()})
		return
	}

	var out []transcriptEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		agent := strings.TrimSuffix(e.Name(), ".jsonl")
		out = append(out, transcriptEntry{
			Agent:     agent,
			Path:      filepath.Join(dir, e.Name()),
			Size:      info.Size(),
			UpdatedAt: info.ModTime().UTC(),
		})
	}
	if out == nil {
		out = []transcriptEntry{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTranscriptContent serves the content of a single transcript file.
//
// GET /sessions/{id}/transcripts/{agent}
//
// The route pattern captures {agent} which includes the ".jsonl" extension
// (the Go http mux does not support literal text after a wildcard segment).
// The handler strips the ".jsonl" suffix before resolving the file path.
//
// Query params:
//   - ?tail=<bytes>  serve last N bytes
//   - ?follow=1      stream appended bytes until client disconnects
func (d *Daemon) handleTranscriptContent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	agentParam := r.PathValue("agent")

	// The route registers {agent} which captures the full basename including the
	// ".jsonl" extension. Strip it so validation and file resolution use only the
	// agent name.
	agentName := strings.TrimSuffix(agentParam, ".jsonl")

	if !isSafePathComponent(sessionID) || !isSafePathComponent(agentName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path component"})
		return
	}

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if logLevelRank(sess.LogLevel) < logLevelRank(LogLevelVerbose) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcripts only available at verbose or trace tier"})
		return
	}

	filePath := filepath.Join(transcriptDir(sess.WorkspaceDir, sessionID), agentName+".jsonl")

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcript not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat transcript: " + err.Error()})
		return
	}

	q := r.URL.Query()

	// ?follow=1: stream appended bytes until client disconnects.
	if q.Get("follow") == "1" {
		var start int64
		if tailStr := q.Get("tail"); tailStr != "" {
			n, err := strconv.ParseInt(tailStr, 10, 64)
			if err != nil || n < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tail"})
				return
			}
			start = info.Size() - n
			if start < 0 {
				start = 0
			}
		}
		streamTranscript(r.Context(), w, filePath, start)
		return
	}

	// ?tail=<bytes>: serve last N bytes.
	if tailStr := q.Get("tail"); tailStr != "" {
		n, err := strconv.ParseInt(tailStr, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tail"})
			return
		}
		start := info.Size() - n
		if start < 0 {
			start = 0
		}
		f, err := os.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcript not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open transcript: " + err.Error()})
			return
		}
		defer f.Close()
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "seek: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
		return
	}

	// Default: serve the full file.
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcript not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open transcript: " + err.Error()})
		return
	}
	defer f.Close()
	http.ServeContent(w, r, agentName+".jsonl", info.ModTime(), f)
}

// streamTranscript tails filePath starting at offset start, flushing each
// appended block to w until the client disconnects or the context is done.
func streamTranscript(ctx interface {
	Done() <-chan struct{}
}, w http.ResponseWriter, filePath string, start int64) {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transcript not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open transcript: " + err.Error()})
		return
	}
	defer f.Close()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "seek: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}
		if err != nil && err != io.EOF {
			return
		}
		// EOF — wait briefly for more bytes or context cancel.
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
