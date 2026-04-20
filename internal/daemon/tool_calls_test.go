package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// seedToolCallEvents inserts bridge:tool_started and bridge:tool_completed
// events directly into the store for the given session, returning the IDs
// of the started events in insertion order.
func seedToolCallEvents(t *testing.T, d *Daemon, sessionID string) (startID1, startID2, startID3 int64) {
	t.Helper()

	now := time.Now().UTC()

	// Pair 1: agent=backend-dev, tool=Write, tool_call_id=call-1
	// started at now, completed 42ms later with status="ok"
	start1 := store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:tool_started",
		Data:      `{"agent":"backend-dev","tool":"Write","path":"foo.go","tool_call_id":"call-1"}`,
	}
	if err := d.store.LogEvent(start1); err != nil {
		t.Fatalf("log start1: %v", err)
	}

	// Sleep a tiny bit so timestamps differ (SQLite stores millisecond resolution).
	time.Sleep(50 * time.Millisecond)

	complete1 := store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:tool_completed",
		Data:      `{"agent":"backend-dev","tool_call_id":"call-1","status":"ok"}`,
	}
	if err := d.store.LogEvent(complete1); err != nil {
		t.Fatalf("log complete1: %v", err)
	}

	// Pair 2: agent=supervisor, tool=Bash, no tool_call_id (fallback pairing)
	time.Sleep(10 * time.Millisecond)
	start2 := store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:tool_started",
		Data:      `{"agent":"supervisor","tool":"Bash","path":""}`,
	}
	if err := d.store.LogEvent(start2); err != nil {
		t.Fatalf("log start2: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	complete2 := store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:tool_completed",
		Data:      `{"agent":"supervisor","status":"error"}`,
	}
	if err := d.store.LogEvent(complete2); err != nil {
		t.Fatalf("log complete2: %v", err)
	}

	// Orphan start (no completion): agent=backend-dev, tool=Read, tool_call_id=call-orphan
	time.Sleep(10 * time.Millisecond)
	start3 := store.SessionEvent{
		SessionID: sessionID,
		Type:      "bridge:tool_started",
		Data:      `{"agent":"backend-dev","tool":"Read","path":"bar.go","tool_call_id":"call-orphan"}`,
	}
	if err := d.store.LogEvent(start3); err != nil {
		t.Fatalf("log start3: %v", err)
	}

	_ = now // used implicitly via timestamps in the store

	// Retrieve inserted event IDs from the store.
	events, err := d.store.QueryEvents(sessionID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}

	// Find the IDs of the three started events.
	var ids []int64
	for _, ev := range events {
		if ev.Type == "bridge:tool_started" {
			ids = append(ids, ev.ID)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 started events, found %d", len(ids))
	}
	return ids[0], ids[1], ids[2]
}

func TestHandleToolCalls_SessionNotFound(t *testing.T) {
	d := testDaemon(t)
	// Register the route — tests use d.server which is set up in testDaemon,
	// but handleToolCalls is not wired yet. Register it locally for this test.
	d.server.Handler.(*http.ServeMux).HandleFunc("GET /sessions/{id}/tool-calls", d.handleToolCalls)

	rr := doRequest(t, d, "GET", "/sessions/nonexistent/tool-calls", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleToolCalls_Aggregate(t *testing.T) {
	d := testDaemon(t)
	d.server.Handler.(*http.ServeMux).HandleFunc("GET /sessions/{id}/tool-calls", d.handleToolCalls)

	sessID := createTestSession(t, d)
	id1, id2, id3 := seedToolCallEvents(t, d, sessID)

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tool-calls", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var entries []toolCallEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Must have exactly 3 entries.
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}

	// --- Ordering: ascending by "at" ---
	for i := 1; i < len(entries); i++ {
		a, err1 := time.Parse(time.RFC3339Nano, entries[i-1].At)
		b, err2 := time.Parse(time.RFC3339Nano, entries[i].At)
		if err1 != nil || err2 != nil {
			t.Fatalf("parse timestamps: %v / %v", err1, err2)
		}
		if a.After(b) {
			t.Errorf("entry[%d].at (%s) is after entry[%d].at (%s) — not ascending", i-1, entries[i-1].At, i, entries[i].At)
		}
	}

	// --- Entry 0: backend-dev / Write ---
	e0 := entries[0]
	if e0.Agent != "backend-dev" {
		t.Errorf("entry[0].agent = %q, want backend-dev", e0.Agent)
	}
	if e0.Tool != "Write" {
		t.Errorf("entry[0].tool = %q, want Write", e0.Tool)
	}
	if e0.Path != "foo.go" {
		t.Errorf("entry[0].path = %q, want foo.go", e0.Path)
	}
	if e0.Status != "ok" {
		t.Errorf("entry[0].status = %q, want ok", e0.Status)
	}
	if e0.DurationMS <= 0 {
		t.Errorf("entry[0].duration_ms = %d, want >0", e0.DurationMS)
	}
	if e0.ID != fmt.Sprintf("%d", id1) {
		t.Errorf("entry[0].id = %q, want %d", e0.ID, id1)
	}

	// --- Entry 1: supervisor / Bash ---
	e1 := entries[1]
	if e1.Agent != "supervisor" {
		t.Errorf("entry[1].agent = %q, want supervisor", e1.Agent)
	}
	if e1.Tool != "Bash" {
		t.Errorf("entry[1].tool = %q, want Bash", e1.Tool)
	}
	if e1.Status != "error" {
		t.Errorf("entry[1].status = %q, want error", e1.Status)
	}
	if e1.DurationMS <= 0 {
		t.Errorf("entry[1].duration_ms = %d, want >0", e1.DurationMS)
	}
	if e1.ID != fmt.Sprintf("%d", id2) {
		t.Errorf("entry[1].id = %q, want %d", e1.ID, id2)
	}

	// --- Entry 2: orphan (pending) ---
	e2 := entries[2]
	if e2.Agent != "backend-dev" {
		t.Errorf("entry[2].agent = %q, want backend-dev", e2.Agent)
	}
	if e2.Tool != "Read" {
		t.Errorf("entry[2].tool = %q, want Read", e2.Tool)
	}
	if e2.Status != "pending" {
		t.Errorf("entry[2].status = %q, want pending", e2.Status)
	}
	if e2.DurationMS != 0 {
		t.Errorf("entry[2].duration_ms = %d, want 0", e2.DurationMS)
	}
	if e2.ID != fmt.Sprintf("%d", id3) {
		t.Errorf("entry[2].id = %q, want %d", e2.ID, id3)
	}
}

func TestHandleToolCalls_Empty(t *testing.T) {
	d := testDaemon(t)
	d.server.Handler.(*http.ServeMux).HandleFunc("GET /sessions/{id}/tool-calls", d.handleToolCalls)

	sessID := createTestSession(t, d)
	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/tool-calls", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var entries []toolCallEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty array, got %d entries", len(entries))
	}
}
