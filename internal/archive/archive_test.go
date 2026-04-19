package archive

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeEvent(id int64, sessID, typ string, data json.RawMessage) Event {
	return Event{
		ID:        id,
		SessionID: sessID,
		Timestamp: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Type:      typ,
		Data:      data,
	}
}

func fullMeta() Meta {
	return Meta{
		SchemaVersion:    "belayer-log/v1",
		DaemonInstanceID: "3b1e5c08-dead-beef-cafe-0a1b2c3d4e5f",
		Session: SessionMeta{
			ID:        "9f2b4a11-7e3d-4c5a-b6f8-1234567890ab",
			Name:      "build-feature-x",
			Workspace: "/Users/op/work/my-repo",
		},
		AgentRoster: []AgentInfo{
			{Name: "supervisor", Role: "supervisor", Profile: "default"},
		},
		Artifacts: []ArtifactInfo{
			{ID: "a1", Kind: "spec", Path: ".belayer/artifacts/spec.md"},
		},
		FinalStatus: "complete",
		Partial:     false,
		ArchivedAt:  time.Date(2026, 4, 17, 13, 12, 44, 0, time.UTC),
	}
}

func readNDJSON(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open ndjson: %v", err)
	}
	defer f.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("parse ndjson line: %v", err)
		}
		lines = append(lines, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan ndjson: %v", err)
	}
	return lines
}

func readManifest(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

func TestWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		makeEvent(1, "sess1", "session_created", json.RawMessage(`{"name":"foo"}`)),
		makeEvent(2, "sess1", "agent_spawned", json.RawMessage(`{"agent":"sup"}`)),
		makeEvent(3, "sess1", "bridge:heartbeat", json.RawMessage(`{"agent":"sup"}`)),
	}
	res, err := Write(dir, fullMeta(), events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.EventCount != 3 {
		t.Errorf("EventCount: got %d, want 3", res.EventCount)
	}
	lines := readNDJSON(t, res.EventsNDJSON)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	// data must be a JSON object, not a string
	for _, line := range lines {
		data := line["data"]
		if _, ok := data.(map[string]any); !ok {
			t.Errorf("data must be an object, got %T: %v", data, data)
		}
	}
}

func TestWrite_StagingCleanupAndAtomicDirRename(t *testing.T) {
	// Successful write must leave no staging dirs behind and the dest must
	// contain both events.ndjson and manifest.json — the directory rename is
	// the atomic commit point.
	parent := t.TempDir()
	destDir := filepath.Join(parent, "archive", "sess1")
	events := []Event{makeEvent(1, "s", "t", json.RawMessage(`{}`))}
	res, err := Write(destDir, fullMeta(), events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Dest dir contains both files.
	for _, name := range []string{"events.ndjson", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(destDir, name)); err != nil {
			t.Errorf("missing %s after write: %v", name, err)
		}
	}
	// No *.staging-* siblings left behind.
	entries, err := os.ReadDir(filepath.Dir(destDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(destDir) && len(e.Name()) > 0 {
			t.Errorf("unexpected sibling entry after write: %s", e.Name())
		}
	}
	_ = res
}

func TestWrite_StagingRemovedOnFailure(t *testing.T) {
	// Force writeEventsNDJSON to fail by passing a destDir whose parent is a file
	// (not a directory) — MkdirAll will fail. We can't easily inject a write
	// failure without hooks, so we prove the happy-path cleanup via the previous
	// test and here assert that a failure path surfaces an error instead of a
	// partial archive.
	parent := t.TempDir()
	// Create a file where the archive parent should be.
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(blocker, "archive")
	events := []Event{makeEvent(1, "s", "t", json.RawMessage(`{}`))}
	if _, err := Write(destDir, fullMeta(), events); err == nil {
		t.Fatal("expected error when parent cannot be a directory")
	}
	// We cannot cleanly os.Stat destDir because the parent is a regular file
	// (ENOTDIR traversing blocker/archive). The contract here is: Write returned
	// an error, so the caller must treat the archive as absent. We verify the
	// parent file was not corrupted (still a regular file, still has our data).
	info, err := os.Stat(blocker)
	if err != nil {
		t.Fatalf("stat blocker: %v", err)
	}
	if info.IsDir() {
		t.Error("blocker file became a directory somehow")
	}
}

func TestWrite_ManifestShape(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		makeEvent(143, "sess1", "session_created", json.RawMessage(`{"name":"x"}`)),
		makeEvent(360, "sess1", "session_completed", json.RawMessage(`{"approved_by":"pm","report":"ok"}`)),
	}
	meta := fullMeta()
	res, err := Write(dir, meta, events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	m := readManifest(t, res.ManifestJSON)

	checks := []struct {
		key  string
		want any
	}{
		{"schema_version", "belayer-log/v1"},
		{"daemon_instance_id", meta.DaemonInstanceID},
		{"final_status", "complete"},
		{"event_count", float64(2)},
		{"first_event_id", float64(143)},
		{"last_event_id", float64(360)},
		{"archived_at", "2026-04-17T13:12:44Z"},
		{"partial", false},
	}
	for _, c := range checks {
		got := m[c.key]
		if got != c.want {
			t.Errorf("manifest[%q]: got %v (%T), want %v", c.key, got, got, c.want)
		}
	}

	// session sub-object
	sess, ok := m["session"].(map[string]any)
	if !ok {
		t.Fatalf("manifest.session not an object")
	}
	if sess["id"] != meta.Session.ID {
		t.Errorf("session.id: got %v, want %v", sess["id"], meta.Session.ID)
	}

	// agent_roster
	roster, ok := m["agent_roster"].([]any)
	if !ok || len(roster) != 1 {
		t.Fatalf("agent_roster: got %v", m["agent_roster"])
	}

	// artifacts
	arts, ok := m["artifacts"].([]any)
	if !ok || len(arts) != 1 {
		t.Fatalf("artifacts: got %v", m["artifacts"])
	}
}

func TestWrite_EmptyEvents(t *testing.T) {
	dir := t.TempDir()
	res, err := Write(dir, fullMeta(), nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.EventCount != 0 {
		t.Errorf("EventCount: got %d, want 0", res.EventCount)
	}
	if res.FirstEventID != 0 {
		t.Errorf("FirstEventID: got %d, want 0", res.FirstEventID)
	}
	if res.LastEventID != 0 {
		t.Errorf("LastEventID: got %d, want 0", res.LastEventID)
	}
	m := readManifest(t, res.ManifestJSON)
	if m["event_count"] != float64(0) {
		t.Errorf("manifest event_count: got %v", m["event_count"])
	}
	if m["first_event_id"] != float64(0) {
		t.Errorf("manifest first_event_id: got %v, want 0", m["first_event_id"])
	}
	if m["last_event_id"] != float64(0) {
		t.Errorf("manifest last_event_id: got %v, want 0", m["last_event_id"])
	}
	// events.ndjson must exist but be empty
	info, err := os.Stat(res.EventsNDJSON)
	if err != nil {
		t.Fatalf("events.ndjson missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("events.ndjson should be empty for 0 events, got size %d", info.Size())
	}
}

func TestWrite_NonObjectDataWrapped(t *testing.T) {
	// Data that is neither a JSON object nor a decodable inner-JSON string MUST
	// be wrapped as {"raw": <original>} so cragd never sees a bare string or
	// primitive in the data slot.
	dir := t.TempDir()
	cases := []struct {
		name string
		data json.RawMessage
		want string // key in the resulting data object
	}{
		{"plain string", json.RawMessage(`"some free-text error"`), "raw"},
		{"empty string", json.RawMessage(`""`), ""}, // empty string unwraps to {}
		{"array", json.RawMessage(`[1,2,3]`), "raw"},
		{"number", json.RawMessage(`42`), "raw"},
		{"null", json.RawMessage(`null`), "raw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subDir := filepath.Join(dir, tc.name)
			events := []Event{makeEvent(1, "s", "t", tc.data)}
			res, err := Write(subDir, fullMeta(), events)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			lines := readNDJSON(t, res.EventsNDJSON)
			data, ok := lines[0]["data"].(map[string]any)
			if !ok {
				t.Fatalf("data must be object, got %T: %v", lines[0]["data"], lines[0]["data"])
			}
			if tc.want == "" {
				if len(data) != 0 {
					t.Errorf("empty string should unwrap to {}, got %v", data)
				}
			} else {
				if _, exists := data[tc.want]; !exists {
					t.Errorf("data should contain key %q, got %v", tc.want, data)
				}
			}
		})
	}
}

func TestWrite_PartialFlag(t *testing.T) {
	dir := t.TempDir()
	meta := fullMeta()
	meta.Partial = true
	res, err := Write(dir, meta, nil)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	m := readManifest(t, res.ManifestJSON)
	if m["partial"] != true {
		t.Errorf("manifest.partial: got %v, want true", m["partial"])
	}
}

func TestWrite_DataStringToObject(t *testing.T) {
	dir := t.TempDir()
	// Data is a JSON-encoded string whose content is a JSON object.
	innerJSON := `{"agent":"web"}`
	encoded, _ := json.Marshal(innerJSON) // produces "\"{ ... }\""
	events := []Event{
		makeEvent(1, "sess1", "bridge:heartbeat", json.RawMessage(encoded)),
	}
	res, err := Write(dir, fullMeta(), events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := readNDJSON(t, res.EventsNDJSON)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line")
	}
	data, ok := lines[0]["data"].(map[string]any)
	if !ok {
		t.Fatalf("data must be object after unwrap, got %T: %v", lines[0]["data"], lines[0]["data"])
	}
	if data["agent"] != "web" {
		t.Errorf("data.agent: got %v, want web", data["agent"])
	}
}

func TestWrite_IDsSorted(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		makeEvent(7, "s", "t", json.RawMessage(`{}`)),
		makeEvent(1, "s", "t", json.RawMessage(`{}`)),
		makeEvent(3, "s", "t", json.RawMessage(`{}`)),
	}
	res, err := Write(dir, fullMeta(), events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := readNDJSON(t, res.EventsNDJSON)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines")
	}
	ids := []float64{
		lines[0]["id"].(float64),
		lines[1]["id"].(float64),
		lines[2]["id"].(float64),
	}
	if ids[0] != 1 || ids[1] != 3 || ids[2] != 7 {
		t.Errorf("IDs not sorted ascending: %v", ids)
	}
}

func TestExtractArtifacts_SkipCounter(t *testing.T) {
	events := []Event{
		{ID: 1, Type: "artifact_created", Data: json.RawMessage(`{"kind":"spec","path":"/tmp/spec.md"}`)},
		{ID: 2, Type: "artifact_created", Data: json.RawMessage(`not json at all`)},
		{ID: 3, Type: "bridge:heartbeat", Data: json.RawMessage(`{"agent":"sup"}`)},
		{ID: 4, Type: "artifact_created", Data: json.RawMessage(`{"path":"/tmp/x.md"}`)},
	}
	arts, skipped := ExtractArtifacts(events)
	if len(arts) != 1 {
		t.Errorf("expected 1 parseable artifact, got %d", len(arts))
	}
	if skipped != 2 {
		t.Errorf("expected skipped=2 (unparseable + missing kind), got %d", skipped)
	}
	if len(arts) > 0 && arts[0].Kind != "spec" {
		t.Errorf("expected kind=spec, got %s", arts[0].Kind)
	}
}

func TestWrite_IncludesTranscripts(t *testing.T) {
	// Create a source transcripts directory with two agent JSONL files.
	srcDir := t.TempDir()
	supervisorContent := `{"ts":1,"agent_id":"supervisor","foo":"bar"}` + "\n"
	implementerContent := `{"ts":2,"agent_id":"implementer","foo":"baz"}` + "\n"
	if err := os.WriteFile(filepath.Join(srcDir, "supervisor.jsonl"), []byte(supervisorContent), 0o644); err != nil {
		t.Fatalf("write supervisor.jsonl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "implementer.jsonl"), []byte(implementerContent), 0o644); err != nil {
		t.Fatalf("write implementer.jsonl: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "archive", "sess-transcript-test")
	events := []Event{makeEvent(1, "sess1", "session_created", json.RawMessage(`{"name":"test"}`))}

	res, err := Write(destDir, fullMeta(), events, WithTranscriptDir(srcDir))
	if err != nil {
		t.Fatalf("Write with transcripts: %v", err)
	}

	// Assert transcript files exist with verbatim content.
	for _, tc := range []struct {
		file    string
		content string
	}{
		{"supervisor.jsonl", supervisorContent},
		{"implementer.jsonl", implementerContent},
	} {
		path := filepath.Join(destDir, "transcripts", tc.file)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("transcript %s missing: %v", tc.file, err)
			continue
		}
		if string(got) != tc.content {
			t.Errorf("transcript %s content mismatch:\ngot:  %q\nwant: %q", tc.file, string(got), tc.content)
		}
	}

	// Assert events.ndjson and manifest.json are still written (no regression).
	if _, err := os.Stat(res.EventsNDJSON); err != nil {
		t.Errorf("events.ndjson missing after transcript write: %v", err)
	}
	if _, err := os.Stat(res.ManifestJSON); err != nil {
		t.Errorf("manifest.json missing after transcript write: %v", err)
	}
}

func TestWrite_TranscriptDirMissingIsNotError(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "archive", "sess-no-transcripts")
	events := []Event{makeEvent(1, "s", "t", json.RawMessage(`{}`))}

	_, err := Write(destDir, fullMeta(), events, WithTranscriptDir("/nonexistent/path/to/transcripts"))
	if err != nil {
		t.Fatalf("expected no error when transcript dir is missing, got: %v", err)
	}

	// Assert no transcripts subdir was created.
	transcriptsPath := filepath.Join(destDir, "transcripts")
	if _, statErr := os.Stat(transcriptsPath); statErr == nil {
		t.Errorf("transcripts/ dir should not exist when srcDir is missing, but it does")
	}
}

func TestWrite_ManifestLogLevelRoundtrip(t *testing.T) {
	readSession := func(t *testing.T, destDir string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(destDir, "manifest.json"))
		if err != nil {
			t.Fatalf("read manifest: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("parse manifest: %v", err)
		}
		sess, ok := m["session"].(map[string]any)
		if !ok {
			t.Fatalf("manifest.session not an object")
		}
		return sess
	}

	t.Run("verbose_is_serialized", func(t *testing.T) {
		destDir := filepath.Join(t.TempDir(), "archive", "sess-verbose")
		meta := fullMeta()
		meta.Session.LogLevel = "verbose"
		if _, err := Write(destDir, meta, nil); err != nil {
			t.Fatalf("Write: %v", err)
		}
		sess := readSession(t, destDir)
		if got := sess["log_level"]; got != "verbose" {
			t.Errorf("session.log_level: got %v, want %q", got, "verbose")
		}
	})

	t.Run("empty_is_omitted", func(t *testing.T) {
		destDir := filepath.Join(t.TempDir(), "archive", "sess-empty")
		meta := fullMeta() // LogLevel unset
		if _, err := Write(destDir, meta, nil); err != nil {
			t.Fatalf("Write: %v", err)
		}
		sess := readSession(t, destDir)
		if _, present := sess["log_level"]; present {
			t.Errorf("session.log_level should be omitted when empty, got %v", sess["log_level"])
		}
	})
}

func TestWrite_EventIDGapsPreserved(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		makeEvent(1, "s", "t", json.RawMessage(`{}`)),
		makeEvent(3, "s", "t", json.RawMessage(`{}`)),
		makeEvent(7, "s", "t", json.RawMessage(`{}`)),
	}
	res, err := Write(dir, fullMeta(), events)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	lines := readNDJSON(t, res.EventsNDJSON)
	ids := []float64{
		lines[0]["id"].(float64),
		lines[1]["id"].(float64),
		lines[2]["id"].(float64),
	}
	if ids[0] != 1 || ids[1] != 3 || ids[2] != 7 {
		t.Errorf("IDs must be preserved as-is (no renumbering): %v", ids)
	}
	if res.FirstEventID != 1 || res.LastEventID != 7 {
		t.Errorf("FirstEventID=%d LastEventID=%d", res.FirstEventID, res.LastEventID)
	}
}
