package daemon

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// isSafePathComponent returns true iff s is a non-empty string that contains
// no path separator and is not a dot-traversal component. It rejects:
//   - empty string
//   - "." or ".."
//   - any string containing os.PathSeparator ('/')
//   - any string starting with '/' (absolute-path injection)
func isSafePathComponent(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsRune(s, os.PathSeparator) {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return false
	}
	return true
}

// handleTraceSlice serves a byte-range slice of a trace fragment file.
//
// GET /sessions/{id}/trace/{agent}/{fragment}?offset=N&length=M
//
// The {fragment} path value can be a zero-padded index (e.g. "0001"), a plain
// integer ("1"), or include the ".zst" suffix ("0001.zst"). The handler locates
// the fragment on disk (plain .jsonl or sealed .jsonl.zst), reads the requested
// slice, and writes it as application/octet-stream.
func (d *Daemon) handleTraceSlice(w http.ResponseWriter, r *http.Request) {
	sessID := r.PathValue("id")
	agentName := r.PathValue("agent")
	fragmentParam := r.PathValue("fragment")

	// Reject path components that could enable directory traversal. PathValue
	// decodes percent-encoding, so %2F → '/' and %2E%2E → '..' arrive here
	// as their decoded forms.
	if !isSafePathComponent(sessID) || !isSafePathComponent(agentName) || !isSafePathComponent(fragmentParam) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path component"})
		return
	}

	// Parse query params.
	offsetStr := r.URL.Query().Get("offset")
	lengthStr := r.URL.Query().Get("length")
	if offsetStr == "" || lengthStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset and length are required"})
		return
	}
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be a non-negative integer"})
		return
	}
	length, err := strconv.ParseInt(lengthStr, 10, 64)
	if err != nil || length < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "length must be a non-negative integer"})
		return
	}

	// Look up session to check log level.
	sess, err := d.store.GetSession(sessID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.LogLevel != LogLevelTrace {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace data only available at trace tier"})
		return
	}

	// Compute the base directory for this (session, agent) pair.
	fragDir := filepath.Join(d.traceBase, sessID, agentName)

	// Resolve fragmentParam to an actual file path. We try multiple candidate
	// names to accept zero-padded index, plain int, and optional .zst suffix.
	plainPath, zstPath := resolveFragmentPaths(fragDir, fragmentParam)

	// Enforce that the resolved paths stay under traceBase to guard against any
	// remaining traversal vector (e.g. symlinks or future router changes).
	safeBase := filepath.Clean(d.traceBase) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(plainPath), safeBase) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path component"})
		return
	}

	// Check which file exists; prefer the plain (open) file over the sealed .zst.
	var resolvedPath string
	var isZst bool

	if _, err := os.Stat(plainPath); err == nil {
		resolvedPath = plainPath
		isZst = false
	} else if _, err := os.Stat(zstPath); err == nil {
		resolvedPath = zstPath
		isZst = true
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "fragment not found"})
		return
	}

	// filepath.Clean + HasPrefix above catch "../" traversal but do not follow
	// symlinks. A symlink created under traceBase pointing outside would pass
	// the prefix check and then expose arbitrary files through os.Open. Verify
	// the resolved real path still lives under traceBase's real path.
	baseReal, err := filepath.EvalSymlinks(d.traceBase)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("resolve trace base: %v", err)})
		return
	}
	resolvedReal, err := filepath.EvalSymlinks(resolvedPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("resolve fragment: %v", err)})
		return
	}
	baseRealPrefix := filepath.Clean(baseReal) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(resolvedReal), baseRealPrefix) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path component"})
		return
	}
	resolvedPath = resolvedReal

	w.Header().Set("Content-Type", "application/octet-stream")

	if isZst {
		// Decompress the full fragment into memory, then slice.
		f, err := os.Open(resolvedPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("open fragment: %v", err)})
			return
		}
		defer f.Close()

		dec, err := zstd.NewReader(f)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("zstd reader: %v", err)})
			return
		}
		defer dec.Close()

		var buf bytes.Buffer
		if _, err := io.Copy(&buf, dec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("decompress: %v", err)})
			return
		}
		data := buf.Bytes()
		size := int64(len(data))

		// Overflow-safe range validation: check offset and length against size
		// without computing offset+length (which could wrap on large values).
		if offset > size {
			writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range out of bounds"})
			return
		}
		if length > size-offset {
			writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range out of bounds"})
			return
		}
		end := offset + length
		w.WriteHeader(http.StatusOK)
		w.Write(data[offset:end]) //nolint:errcheck
		return
	}

	// Plain (active or uncompressed) fragment: seek + copy.
	f, err := os.Open(resolvedPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("open fragment: %v", err)})
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("stat fragment: %v", err)})
		return
	}
	size := fi.Size()
	// Overflow-safe range validation: check offset and length against size
	// without computing offset+length (which could wrap on large values).
	if offset > size {
		writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range out of bounds"})
		return
	}
	if length > size-offset {
		writeJSON(w, http.StatusRequestedRangeNotSatisfiable, map[string]string{"error": "range out of bounds"})
		return
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("seek: %v", err)})
		return
	}

	w.WriteHeader(http.StatusOK)
	io.CopyN(w, f, length) //nolint:errcheck
}

// resolveFragmentPaths returns the candidate plain (.jsonl) and sealed
// (.jsonl.zst) file paths for a given fragment identifier and directory.
//
// The fragmentParam may be:
//   - a zero-padded 4-digit index like "0001"
//   - a plain integer like "1"
//   - a name with ".zst" suffix like "0001.zst" (the .zst is stripped for lookup)
func resolveFragmentPaths(fragDir, fragmentParam string) (plainPath, zstPath string) {
	// Strip optional ".zst" suffix from the input so we normalise the name.
	name := strings.TrimSuffix(fragmentParam, ".zst")
	// Strip optional ".jsonl" suffix too.
	name = strings.TrimSuffix(name, ".jsonl")

	// Try to parse name as an integer so we can zero-pad it consistently.
	n, err := strconv.Atoi(name)
	if err == nil {
		padded := fmt.Sprintf("%04d", n)
		plainPath = filepath.Join(fragDir, padded+".jsonl")
		zstPath = filepath.Join(fragDir, padded+".jsonl.zst")
		return
	}

	// Treat as a literal name (e.g. already zero-padded but somehow non-numeric).
	plainPath = filepath.Join(fragDir, name+".jsonl")
	zstPath = filepath.Join(fragDir, name+".jsonl.zst")
	return
}
