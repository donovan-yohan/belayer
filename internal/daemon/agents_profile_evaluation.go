package daemon

// Phase 3.D: talent-evaluation artifact extraction.
//
// Before a climb-scoped fork profile is torn down, the daemon reads the
// agent's MEMORY.md (and USER.md when present) from the profile's memories/
// directory and writes a talent-evaluation/v1 artifact to the climb's
// artifacts dir. This preserves the agent's accumulated context for retros
// and promotion review even after the profile is gone.
//
// Schema: docs/artifact-schemas/talent-evaluation.schema.json
//
// Phase 3.D scope — fields populated here:
//   schema_version, talent, org, session.id, tasks (empty), assignment
//   (placeholder), gate_outcomes (empty), recommendations (empty),
//   evaluated_at, observations (memory content from MEMORY.md / USER.md).
//
// Memory content is captured as a single strength-type observation so the
// artifact remains conformant with the schema's additionalProperties: false
// constraint. MEMORY.md and USER.md (when present) are joined in one
// observation summary separated by "--- USER ---".
//
// Out of scope for Phase 3.D (Phase 4+):
//   produced_artifacts  — requires listing artifacts registered by this agent run.
//   gate_outcomes       — requires querying gate-result artifacts for this run.
//   metrics             — requires token-usage accounting from the bridge.
//   recommendations     — requires gate outcome data to be meaningful.
//   tasks               — requires task-graph integration from the org-plan.
//   assignment.source/lifecycle/state/task_ids — requires talent-request artifact.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/climbpath"
	"github.com/donovan-yohan/belayer/internal/store"
)

// talentEvaluationArtifact is the in-memory representation written to disk
// as a talent-evaluation/v1 JSON artifact. Only the fields populated by
// Phase 3.D are present; see package-level comment for omissions.
type talentEvaluationArtifact struct {
	SchemaVersion   string               `json:"schema_version"`
	Talent          string               `json:"talent"`
	Org             string               `json:"org"`
	Session         talentEvalSession    `json:"session"`
	Tasks           []any                `json:"tasks"`
	Assignment      talentEvalAssignment `json:"assignment"`
	GateOutcomes    []any                `json:"gate_outcomes"`
	Observations    []talentObservation  `json:"observations,omitempty"`
	Recommendations []any                `json:"recommendations"`
	EvaluatedAt     string               `json:"evaluated_at"`
}

// talentObservation is the schema's observation object (type + summary + evidence).
type talentObservation struct {
	Type     string   `json:"type"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence"`
}

type talentEvalSession struct {
	ID string `json:"id"`
}

// talentEvalAssignment is a minimal placeholder that satisfies the schema's
// required fields. Populated with "not_available" because the talent-request
// artifact (which carries source/lifecycle/task_ids) is not yet queried here.
// Phase 4 should replace this with real data from the org-plan artifact.
type talentEvalAssignment struct {
	// Source is "project" as a conservative default; the talent was materialized
	// from a project-local agent definition. Phase 4 should resolve from the
	// talent-request artifact.
	Source    string   `json:"source"`
	// Lifecycle defaults to "ephemeral" for climb-scoped profiles.
	Lifecycle string   `json:"lifecycle"`
	// State is "not_available" because Phase 3.D does not query the task graph.
	State     string   `json:"state"`
	// TaskIDs is a placeholder; Phase 4 should populate from the org-plan.
	TaskIDs   []string `json:"task_ids"`
}

// writeTalentEvaluationArtifact reads the agent's accumulated memory from the
// profile directory, writes a talent-evaluation/v1 JSON file to the climb's
// artifacts directory, and registers it in the daemon store.
//
// Failure is best-effort: errors are logged and the function returns so that
// teardown proceeds regardless. The artifact is overwritten if it already
// exists (idempotent re-spawn path).
//
// Parameters:
//   - d             — the daemon (store access).
//   - sessionID     — the climb's session UUID.
//   - agentRunID    — the agent_runs.id for this run (used in the filename).
//   - workspaceDir  — the session's workspace root (used to locate the climb dir).
//   - profileName   — the materialized Hermes profile name (e.g. "belayer-local-supervisor").
//   - meta          — the full talent metadata read from .belayer-talent.yaml.
func (d *Daemon) writeTalentEvaluationArtifact(
	sessionID, agentRunID, workspaceDir, profileName string,
	meta talentMetadata,
) {
	// Resolve the climb artifacts directory. climbpath.SessionDir resolves the
	// legacy "runs" fallback automatically. We derive the artifacts subdir from
	// the session dir (not per-agent dir) so all evaluations land in one place.
	sessionDir := climbpath.SessionDir(workspaceDir, sessionID)
	artifactsDir := filepath.Join(sessionDir, "artifacts")

	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		log.Printf("ERROR: writeTalentEvaluationArtifact: mkdir %s: %v — skipping artifact, proceeding with teardown", artifactsDir, err)
		return
	}

	// Read memories from the profile directory.
	profilesRoot, err := ProfilesRoot()
	if err != nil {
		log.Printf("ERROR: writeTalentEvaluationArtifact: resolve profiles root: %v — skipping artifact, proceeding with teardown", err)
		return
	}
	memoriesDir := filepath.Join(profilesRoot, profileName, "memories")
	memoryExcerpt := readProfileMemories(memoriesDir)

	// Build talent name: prefer .belayer-talent.yaml TalentName; fall back to
	// stripping the "belayer-<crag>-" prefix from the profile name.
	talentName := meta.TalentName
	if talentName == "" {
		talentName, _ = splitProfileName(profileName)
	}
	orgName := meta.CragSlug
	if orgName == "" {
		orgName = "unknown"
	}

	artifact := talentEvaluationArtifact{
		SchemaVersion: "belayer-talent-evaluation/v1",
		Talent:        talentName,
		Org:           orgName,
		Session:       talentEvalSession{ID: sessionID},
		Tasks:         []any{},
		Assignment: talentEvalAssignment{
			Source:    "project",
			Lifecycle: "ephemeral",
			State:     "not_available",
			// Phase 4: populate from org-plan artifact task_ids.
			TaskIDs: []string{"unknown"},
		},
		GateOutcomes:    []any{},
		Recommendations: []any{},
		EvaluatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	// Capture memory content as a strength observation so the artifact stays
	// conformant with the schema's additionalProperties: false constraint.
	// MEMORY.md and USER.md (when present) are joined in one observation
	// summary separated by "--- USER ---".
	//
	// TODO(Phase 4): store-dedup — re-running writeTalentEvaluationArtifact
	// (e.g. on agent re-spawn) calls CreateArtifact again, producing duplicate
	// rows. The file is overwritten cleanly, but the store accumulates stale
	// entries. Phase 4 should upsert by (session_id, kind, producer) instead.
	if memContent, ok := memoryExcerpt["MEMORY.md"]; ok {
		summary := memContent
		if userContent, hasUser := memoryExcerpt["USER.md"]; hasUser {
			summary += "\n\n--- USER ---\n\n" + userContent
		}
		artifact.Observations = append(artifact.Observations, talentObservation{
			Type:     "strength",
			Summary:  summary,
			Evidence: []string{},
		})
	}

	// Build filename: talent-evaluation-<talent>-<agent_run_id>.json
	// agentRunID may be empty if the caller could not fetch the run row; fall
	// back to a truncated sessionID to keep the file unique.
	runSuffix := agentRunID
	if runSuffix == "" {
		runSuffix = sessionID
	}
	filename := "talent-evaluation-" + talentName + "-" + runSuffix + ".json"
	artifactPath := filepath.Join(artifactsDir, filename)

	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		log.Printf("ERROR: writeTalentEvaluationArtifact: marshal JSON: %v — skipping artifact, proceeding with teardown", err)
		return
	}
	if err := os.WriteFile(artifactPath, raw, 0o644); err != nil {
		log.Printf("ERROR: writeTalentEvaluationArtifact: write %s: %v — skipping artifact, proceeding with teardown", artifactPath, err)
		return
	}

	// Register with the store so the artifact appears in /sessions/{id}/artifacts.
	// Path is relative to workspace so it remains portable.
	relPath := artifactPath
	if workspaceDir != "" {
		if rel, relErr := filepath.Rel(workspaceDir, artifactPath); relErr == nil {
			relPath = filepath.ToSlash(rel)
		}
	}
	if _, storeErr := d.store.CreateArtifact(store.Artifact{
		SessionID: sessionID,
		Kind:      "talent-evaluation",
		Path:      relPath,
		Producer:  talentName,
		Summary:   "Per-run talent evaluation captured at profile teardown (Phase 3.D)",
	}); storeErr != nil {
		log.Printf("WARNING: writeTalentEvaluationArtifact: register artifact in store: %v — file written but not indexed", storeErr)
	}

	log.Printf("INFO: writeTalentEvaluationArtifact: wrote %s (talent=%s session=%s)", artifactPath, talentName, sessionID)
}

// readProfileMemories reads MEMORY.md and USER.md from the given memories
// directory. Missing files are silently skipped. Returns a map with keys
// "MEMORY.md" and/or "USER.md" for any non-empty files found.
func readProfileMemories(memoriesDir string) map[string]string {
	out := make(map[string]string)
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		p := filepath.Join(memoriesDir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			// Missing or unreadable — skip silently; not all profiles have USER.md.
			continue
		}
		content := string(data)
		if content != "" {
			out[name] = content
		}
	}
	return out
}
