package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Meta is the input to Write. All fields are caller-populated.
type Meta struct {
	SchemaVersion    string        // always "belayer-log/v1"
	DaemonInstanceID string        // UUID of the daemon; may be empty if unknown
	Session          SessionMeta   // id, name, workspace
	AgentRoster      []AgentInfo   // name, role, profile
	Artifacts        []ArtifactInfo
	FinalStatus      string // one of: complete, blocked, failed, cancelled, needs_human_review, stalled
	Partial          bool   // true if drain timed out mid-flush
	ArchivedAt       time.Time
}

// SessionMeta identifies the session being archived.
type SessionMeta struct {
	ID        string
	Name      string
	Workspace string
	LogLevel  string // "standard" or "verbose"; disambiguates presence/absence of transcripts/
}

// AgentInfo describes an agent in the roster.
type AgentInfo struct {
	Name    string
	Role    string
	Profile string
}

// ArtifactInfo describes a session artifact.
type ArtifactInfo struct {
	ID   string
	Kind string
	Path string
}

// Event is the minimal event record the writer needs. Callers adapt to this shape.
// Do NOT import store.SessionEvent — this keeps archive free of store dependencies.
type Event struct {
	ID        int64
	SessionID string
	Timestamp time.Time
	Type      string
	Data      json.RawMessage // verbatim; may be a JSON-encoded string or an object
}

// WriteOption is a functional option for Write.
type WriteOption func(*writeConfig)

// writeConfig holds the resolved options for a Write call.
type writeConfig struct {
	transcriptSrc string // source directory of per-agent JSONL files; empty = skip
}

// WithTranscriptDir returns a WriteOption that copies per-agent transcript
// files from srcDir into <destDir>/transcripts/ as part of the atomic archive.
// If srcDir does not exist Write proceeds normally — no transcripts subdir is
// created in the output.
func WithTranscriptDir(srcDir string) WriteOption {
	return func(cfg *writeConfig) {
		cfg.transcriptSrc = srcDir
	}
}

// WriteResult holds metadata about the archive that was written.
type WriteResult struct {
	EventCount   int
	FirstEventID int64
	LastEventID  int64
	EventsNDJSON string // absolute path
	ManifestJSON string // absolute path
}

// Write produces events.ndjson and manifest.json in destDir atomically via a
// staging-directory rename: both files are written to `<destDir>.staging-<pid>-<nanos>/`,
// then the staging directory is renamed to destDir. Directory rename is atomic on
// POSIX, so a consumer of destDir either sees both files or sees no directory at all.
// Events are sorted by ID ascending. Returns WriteResult with counts and paths.
//
// Optional WriteOptions may be passed (e.g. WithTranscriptDir) to include
// additional content in the archive. Existing callers passing no options are
// unaffected.
func Write(destDir string, meta Meta, events []Event, opts ...WriteOption) (WriteResult, error) {
	var wcfg writeConfig
	for _, o := range opts {
		o(&wcfg)
	}

	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return WriteResult{}, fmt.Errorf("archive: mkdir parent %s: %w", parent, err)
	}

	staging, err := os.MkdirTemp(parent, filepath.Base(destDir)+".staging-")
	if err != nil {
		return WriteResult{}, fmt.Errorf("archive: mkdir staging: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(staging) }

	sorted := make([]Event, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	eventsStaging := filepath.Join(staging, "events.ndjson")
	manifestStaging := filepath.Join(staging, "manifest.json")

	count, err := writeEventsNDJSON(eventsStaging, sorted)
	if err != nil {
		cleanup()
		return WriteResult{}, err
	}
	var firstID, lastID int64
	if len(sorted) > 0 {
		firstID = sorted[0].ID
		lastID = sorted[len(sorted)-1].ID
	}

	if err := writeManifest(manifestStaging, meta, count, firstID, lastID); err != nil {
		cleanup()
		return WriteResult{}, err
	}

	// Copy transcript files into <staging>/transcripts/ if a source dir was
	// provided and exists. Errors are propagated so the caller knows the
	// archive is incomplete.
	if wcfg.transcriptSrc != "" {
		if info, statErr := os.Stat(wcfg.transcriptSrc); statErr == nil && info.IsDir() {
			if err := copyTranscripts(wcfg.transcriptSrc, filepath.Join(staging, "transcripts")); err != nil {
				cleanup()
				return WriteResult{}, err
			}
		}
		// If transcriptSrc does not exist, proceed silently — standard-level
		// sessions never create the transcripts directory.
	}

	// If destDir already exists (re-archive), remove it first — rename over an
	// existing directory is not portable across filesystems.
	if _, err := os.Stat(destDir); err == nil {
		if err := os.RemoveAll(destDir); err != nil {
			cleanup()
			return WriteResult{}, fmt.Errorf("archive: remove existing dest %s: %w", destDir, err)
		}
	}

	if err := os.Rename(staging, destDir); err != nil {
		cleanup()
		return WriteResult{}, fmt.Errorf("archive: rename staging -> %s: %w", destDir, err)
	}

	return WriteResult{
		EventCount:   count,
		FirstEventID: firstID,
		LastEventID:  lastID,
		EventsNDJSON: filepath.Join(destDir, "events.ndjson"),
		ManifestJSON: filepath.Join(destDir, "manifest.json"),
	}, nil
}

// copyTranscripts walks srcDir and copies each regular file to
// <destDir>/<relpath>, creating parent directories as needed.
// Each destination file is fsynced before proceeding.
func copyTranscripts(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("archive: walk transcripts %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("archive: rel path for transcript %s: %w", path, err)
		}
		destPath := filepath.Join(destDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("archive: mkdir transcript dest parent: %w", err)
		}
		src, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("archive: open transcript src %s: %w", path, err)
		}
		defer src.Close()

		dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("archive: create transcript dest %s: %w", destPath, err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("archive: copy transcript %s: %w", relPath, err)
		}
		if err := dst.Sync(); err != nil {
			return fmt.Errorf("archive: fsync transcript %s: %w", relPath, err)
		}
		return nil
	})
}

// ndJsonLine is the shape of each line in events.ndjson per LOG_FORMAT.md §2.
type ndJsonLine struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

// writeEventsNDJSON writes sorted events to path as NDJSON. Returns line count.
func writeEventsNDJSON(path string, events []Event) (int, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("archive: open events tmp: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range events {
		data := normaliseData(e.Data)
		line := ndJsonLine{
			ID:        e.ID,
			SessionID: e.SessionID,
			Timestamp: e.Timestamp.UTC().Format(time.RFC3339),
			Type:      e.Type,
			Data:      data,
		}
		if err := enc.Encode(line); err != nil {
			return 0, fmt.Errorf("archive: encode event %d: %w", e.ID, err)
		}
	}
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("archive: fsync events tmp: %w", err)
	}
	return len(events), nil
}

// normaliseData converts a JSON-encoded string (HTTP shape) into a JSON object,
// honouring the LOG_FORMAT.md §2 invariant that `data` is always a JSON object.
// Inputs that are not objects are wrapped as {"raw": <original>} so consumers never
// see a bare string, array, null, or primitive in the data slot.
func normaliseData(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	// Try to unwrap the common DB-string encoding: data is stored as a JSON string
	// whose contents are themselves JSON.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if s == "" {
				return json.RawMessage("{}")
			}
			var obj json.RawMessage
			if err2 := json.Unmarshal([]byte(s), &obj); err2 == nil {
				return ensureObject(obj, raw)
			}
			// Inner content is not JSON; preserve the raw string under "raw".
			return wrapRaw(raw)
		}
		// Outer was a JSON string but not decodable; wrap defensively.
		return wrapRaw(raw)
	}
	return ensureObject(raw, raw)
}

// ensureObject returns msg when it is a JSON object, otherwise wraps original under "raw".
func ensureObject(msg, original json.RawMessage) json.RawMessage {
	if len(msg) > 0 && msg[0] == '{' {
		return msg
	}
	return wrapRaw(original)
}

// wrapRaw encodes a non-object payload as {"raw": <original>} so the NDJSON line
// still satisfies the object-only invariant without losing the original bytes.
func wrapRaw(raw json.RawMessage) json.RawMessage {
	wrapper := map[string]json.RawMessage{"raw": raw}
	out, err := json.Marshal(wrapper)
	if err != nil {
		return json.RawMessage("{}")
	}
	return out
}

// manifestJSON is the manifest.json shape per LOG_FORMAT.md §5.
type manifestJSON struct {
	SchemaVersion    string          `json:"schema_version"`
	DaemonInstanceID string          `json:"daemon_instance_id"`
	Session          sessionManifest `json:"session"`
	AgentRoster      []agentManifest `json:"agent_roster"`
	Artifacts        []artManifest   `json:"artifacts"`
	FinalStatus      string          `json:"final_status"`
	EventCount       int             `json:"event_count"`
	FirstEventID     int64           `json:"first_event_id"`
	LastEventID      int64           `json:"last_event_id"`
	ArchivedAt       string          `json:"archived_at"`
	Partial          bool            `json:"partial"`
}

type sessionManifest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	LogLevel  string `json:"log_level,omitempty"`
}

type agentManifest struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Profile string `json:"profile"`
}

type artManifest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// ExtractArtifacts scans events for artifact_created events and returns the
// ArtifactInfo list along with a count of artifact_created events that had
// unparseable data (silently dropped would hide belayer bugs — the caller surfaces
// this as a warning so cragd and operators can see the mismatch between
// artifact_created events in the NDJSON and artifacts in the manifest).
func ExtractArtifacts(events []Event) (arts []ArtifactInfo, skipped int) {
	for _, e := range events {
		if e.Type != "artifact_created" {
			continue
		}
		var payload struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}
		// e.Data may be a JSON string (HTTP shape) or an object. Try both.
		raw := e.Data
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				raw = json.RawMessage(s)
			}
		}
		if err := json.Unmarshal(raw, &payload); err != nil || payload.Kind == "" {
			skipped++
			continue
		}
		arts = append(arts, ArtifactInfo{
			ID:   fmt.Sprintf("%d", e.ID),
			Kind: payload.Kind,
			Path: payload.Path,
		})
	}
	return arts, skipped
}

// writeManifest writes manifest.json.tmp with 2-space indentation.
func writeManifest(path string, meta Meta, count int, firstID, lastID int64) error {
	roster := make([]agentManifest, len(meta.AgentRoster))
	for i, a := range meta.AgentRoster {
		roster[i] = agentManifest{Name: a.Name, Role: a.Role, Profile: a.Profile}
	}
	arts := make([]artManifest, len(meta.Artifacts))
	for i, a := range meta.Artifacts {
		arts[i] = artManifest{ID: a.ID, Kind: a.Kind, Path: a.Path}
	}
	m := manifestJSON{
		SchemaVersion:    meta.SchemaVersion,
		DaemonInstanceID: meta.DaemonInstanceID,
		Session: sessionManifest{
			ID:        meta.Session.ID,
			Name:      meta.Session.Name,
			Workspace: meta.Session.Workspace,
			LogLevel:  meta.Session.LogLevel,
		},
		AgentRoster:  roster,
		Artifacts:    arts,
		FinalStatus:  meta.FinalStatus,
		EventCount:   count,
		FirstEventID: firstID,
		LastEventID:  lastID,
		ArchivedAt:   meta.ArchivedAt.UTC().Format(time.RFC3339),
		Partial:      meta.Partial,
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("archive: marshal manifest: %w", err)
	}
	buf = append(buf, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("archive: open manifest tmp: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("archive: write manifest: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("archive: fsync manifest: %w", err)
	}
	return nil
}

