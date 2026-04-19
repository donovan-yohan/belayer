package daemon

import (
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/store"
)

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

	// Resolve content path.
	var contentPath string
	if filepath.IsAbs(artifact.Path) {
		contentPath = artifact.Path
	} else {
		sess, err := d.store.GetSession(sessionID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		contentPath = filepath.Join(sess.WorkspaceDir, artifact.Path)
	}

	fi, err := os.Stat(contentPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat artifact: " + err.Error()})
		return
	}

	// Infer content type.
	ext := strings.ToLower(filepath.Ext(contentPath))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}

	// Determine disposition.
	var disposition string
	if strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "image/") || ct == "application/json" {
		disposition = "inline"
	} else {
		disposition = `attachment; filename="` + filepath.Base(contentPath) + `"`
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disposition)

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
