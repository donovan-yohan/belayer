package daemon

import (
	"encoding/json"
	"net/http"

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
