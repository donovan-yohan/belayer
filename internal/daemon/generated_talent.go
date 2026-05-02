package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/generatedtalent"
	"github.com/donovan-yohan/belayer/internal/store"
)

type generatedTalentScaffoldRequest struct {
	Crag          string            `json:"crag"`
	ID            string            `json:"id"`
	Domain        string            `json:"domain"`
	Role          string            `json:"role"`
	Lifecycle     string            `json:"lifecycle"`
	SourceRequest string            `json:"source_request"`
	Reason        string            `json:"reason"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Force         bool              `json:"force,omitempty"`
}

func (d *Daemon) handleScaffoldGeneratedTalent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := d.store.GetSession(sessionID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.WorkspaceDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session has no workspace_dir"})
		return
	}

	var req generatedTalentScaffoldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	record := generatedtalent.Record{
		SchemaVersion: generatedtalent.SchemaVersion,
		ID:            req.ID,
		Domain:        req.Domain,
		Role:          req.Role,
		Lifecycle:     req.Lifecycle,
		SourceRequest: req.SourceRequest,
		Reason:        req.Reason,
		Metadata:      req.Metadata,
	}
	identityDir, err := generatedtalent.ScaffoldIdentity(sess.WorkspaceDir, record, req.Force)
	if err != nil {
		status := http.StatusInternalServerError
		if generatedtalent.IsInputError(err) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	responsePath := filepath.ToSlash(identityDir)
	_ = d.store.LogEvent(store.SessionEvent{SessionID: sessionID, Type: "org:generated_talent_scaffolded", Data: mustJSON(map[string]string{
		"crag":     req.Crag,
		"identity": req.ID,
		"path":     responsePath,
	})})
	writeJSON(w, http.StatusCreated, map[string]string{
		"identity": req.ID,
		"path":     responsePath,
	})
}
