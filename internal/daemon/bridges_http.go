package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type bridgeLogDescriptor struct {
	Agent        string    `json:"agent"`
	LogPath      string    `json:"log_path"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modified_at"`
	RotatedFiles int       `json:"rotated_files"`
}

func bridgeLogPathFor(workdir, sessionID, agentName string) string {
	return filepath.Join(workdir, ".belayer", "runs", sessionID, agentName, "bridge-stdout.log")
}

func (d *Daemon) handleListBridges(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := []bridgeLogDescriptor{}
	for _, run := range runs {
		logPath := bridgeLogPathFor(run.Workdir, sessionID, run.Name)
		info, statErr := os.Stat(logPath)
		if statErr != nil {
			continue
		}
		// Count rotated backups (.log.1 .. .log.3).
		rotated := 0
		for i := 1; i <= 3; i++ {
			if _, err := os.Stat(fmt.Sprintf("%s.%d", logPath, i)); err == nil {
				rotated++
			}
		}
		out = append(out, bridgeLogDescriptor{
			Agent:        run.Name,
			LogPath:      logPath,
			Size:         info.Size(),
			ModifiedAt:   info.ModTime(),
			RotatedFiles: rotated,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *Daemon) handleBridgeStdout(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	agentName := r.PathValue("agent")
	run, err := d.store.GetAgentRun(sessionID, agentName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found in session"})
		return
	}
	logPath := bridgeLogPathFor(run.Workdir, sessionID, agentName)
	info, err := os.Stat(logPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "bridge log not found"})
		return
	}

	q := r.URL.Query()
	if q.Get("follow") == "1" {
		// Start at after_byte if given, else current end of file.
		start := info.Size()
		if s := q.Get("after_byte"); s != "" {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
				start = n
			}
		}
		streamBridgeStdout(r.Context(), w, logPath, start)
		return
	}

	if tailStr := q.Get("tail"); tailStr != "" {
		n, err := strconv.ParseInt(tailStr, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tail"})
			return
		}
		if n > info.Size() {
			n = info.Size()
		}
		f, err := os.Open(logPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer f.Close()
		if _, err := f.Seek(info.Size()-n, io.SeekStart); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
		return
	}

	// Default: serve the full file.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, logPath)
}

// streamBridgeStdout tails logPath starting at offset `start`, flushing each
// appended block to w until the client disconnects or the context is done.
func streamBridgeStdout(ctx context.Context, w http.ResponseWriter, logPath string, start int64) {
	f, err := os.Open(logPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
		case <-time.After(250 * time.Millisecond):
		}
	}
}
