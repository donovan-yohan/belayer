package daemon

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/store"
)

// resolveArtifactPath joins artifactPath onto workspaceDir (or uses
// artifactPath as-is when absolute) and returns the cleaned absolute path
// iff it lives inside workspaceDir. Escapes via "..", symlinks outside, or
// absolute paths outside workspaceDir are rejected.
func resolveArtifactPath(workspaceDir, artifactPath string) (string, error) {
	if workspaceDir == "" {
		return "", fmt.Errorf("session has no workspace_dir")
	}
	absRoot, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	absRoot = filepath.Clean(absRoot)

	var joined string
	if filepath.IsAbs(artifactPath) {
		joined = artifactPath
	} else {
		joined = filepath.Join(absRoot, artifactPath)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	abs = filepath.Clean(abs)

	rootWithSep := absRoot + string(os.PathSeparator)
	if abs != absRoot && !strings.HasPrefix(abs, rootWithSep) {
		return "", fmt.Errorf("artifact path escapes workspace")
	}
	return abs, nil
}

// artifactInlineDisposition reports whether ct is safe to render inline in a
// browser. The whitelist is intentionally narrow — text/html, text/xml, and
// anything a user agent might sniff-upgrade to HTML are force-attachment.
func artifactInlineDisposition(ct string) bool {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = ct
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "image/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/pdf", "text/plain":
		return true
	}
	return false
}

type artifactCreateRequest struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Producer string `json:"producer"`
	Summary  string `json:"summary"`
}

func (d *Daemon) handleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	var req artifactCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Kind == "" || req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind and path are required"})
		return
	}
	artifact := store.Artifact{SessionID: sessionID, Kind: req.Kind, Path: req.Path, Producer: req.Producer, Summary: req.Summary}
	id, err := d.store.CreateArtifact(artifact)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	created, err := d.store.GetArtifact(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload artifact"})
		return
	}
	_ = d.store.LogEvent(store.SessionEvent{SessionID: sessionID, Type: "artifact_created", Data: mustJSON(map[string]string{"kind": req.Kind, "path": req.Path, "producer": req.Producer})})
	writeJSON(w, http.StatusCreated, created)
}

// handleGetArtifactBytes serves the raw bytes of an artifact file.
//
// GET /sessions/{id}/artifacts/{artifact_id}
func (d *Daemon) handleGetArtifactBytes(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	artifactID := r.PathValue("artifact_id")

	if !isSafePathComponent(sessionID) || !isSafePathComponent(artifactID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path component"})
		return
	}

	artifact, err := d.store.GetArtifact(artifactID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	if artifact.SessionID != sessionID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}

	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	contentPath, err := resolveArtifactPath(sess.WorkspaceDir, artifact.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact path: " + err.Error()})
		return
	}

	fi, err := os.Stat(contentPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact file missing"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat artifact: " + err.Error()})
		return
	}
	if fi.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact path is a directory"})
		return
	}

	// Infer content type.
	ext := strings.ToLower(filepath.Ext(contentPath))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}

	// Determine disposition using a narrow inline whitelist. text/html and
	// similar are force-attachment so an authenticated TCP consumer cannot
	// serve XSS surfaces off the artifact endpoint.
	var disposition string
	if artifactInlineDisposition(ct) {
		disposition = "inline"
	} else {
		disposition = `attachment; filename="` + filepath.Base(contentPath) + `"`
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	f, err := os.Open(contentPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open artifact: " + err.Error()})
		return
	}
	defer f.Close()

	http.ServeContent(w, r, filepath.Base(contentPath), fi.ModTime(), f)
}

func (d *Daemon) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := d.store.GetSession(sessionID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	artifacts, err := d.store.ListArtifacts(sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, artifacts)
}
