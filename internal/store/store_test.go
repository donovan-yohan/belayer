package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func openMemory(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestCreateSession_ReturnsID verifies that CreateSession generates a non-empty
// ID and that GetSession can retrieve the created session.
func TestCreateSession_ReturnsID(t *testing.T) {
	s := openMemory(t)

	id, err := s.CreateSession(Session{Name: "test-session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, id)
	}
	if got.Name != "test-session" {
		t.Errorf("Name mismatch: got %q, want %q", got.Name, "test-session")
	}
	if got.Status != "pending" {
		t.Errorf("Status mismatch: got %q, want %q", got.Status, "pending")
	}
}

// TestCreateSession_ExplicitID verifies that a caller-supplied ID is preserved.
func TestCreateSession_ExplicitID(t *testing.T) {
	s := openMemory(t)

	want := "explicit-id-123"
	id, err := s.CreateSession(Session{ID: want, Name: "named"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id != want {
		t.Errorf("got ID %q, want %q", id, want)
	}
}

// TestGetSession_NotFound verifies that GetSession returns a wrapped sql.ErrNoRows
// for a non-existent session.
func TestGetSession_NotFound(t *testing.T) {
	s := openMemory(t)

	_, err := s.GetSession("does-not-exist")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows in error chain, got: %v", err)
	}
}

// TestListSessions_OrderedByCreatedAtDesc verifies that ListSessions returns
// sessions in descending creation order.
func TestListSessions_OrderedByCreatedAtDesc(t *testing.T) {
	s := openMemory(t)

	// Insert sessions with slightly different timestamps.
	base := time.Now().UTC()
	for i, name := range []string{"first", "second", "third"} {
		sess := Session{
			Name:      name,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if _, err := s.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession %q: %v", name, err)
		}
	}

	list, err := s.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(list))
	}
	if list[0].Name != "third" {
		t.Errorf("expected first element to be 'third' (most recent), got %q", list[0].Name)
	}
	if list[2].Name != "first" {
		t.Errorf("expected last element to be 'first' (oldest), got %q", list[2].Name)
	}
}

// TestListSessions_Empty verifies that ListSessions returns a nil or empty slice
// when no sessions exist.
func TestListSessions_Empty(t *testing.T) {
	s := openMemory(t)

	list, err := s.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d sessions", len(list))
	}
}

// TestUpdateSessionStatus verifies that UpdateSessionStatus changes the status
// and bumps updated_at.
func TestUpdateSessionStatus(t *testing.T) {
	s := openMemory(t)

	id, err := s.CreateSession(Session{Name: "status-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	before, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession before: %v", err)
	}

	// Small sleep to ensure updated_at actually advances.
	time.Sleep(2 * time.Millisecond)

	if err := s.UpdateSessionStatus(id, "active"); err != nil {
		t.Fatalf("UpdateSessionStatus: %v", err)
	}

	after, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession after: %v", err)
	}
	if after.Status != "active" {
		t.Errorf("expected status %q, got %q", "active", after.Status)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("expected updated_at to advance: before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestLogEvent_QueryEvents verifies a round-trip: log an event and retrieve it
// via QueryEvents.
func TestLogEvent_QueryEvents(t *testing.T) {
	s := openMemory(t)

	id, err := s.CreateSession(Session{Name: "event-session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	evt := SessionEvent{
		SessionID: id,
		Type:      "node_started",
		Data:      `{"node":"impl"}`,
	}
	if err := s.LogEvent(evt); err != nil {
		t.Fatalf("LogEvent: %v", err)
	}

	evts, err := s.QueryEvents(id)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type != "node_started" {
		t.Errorf("Type mismatch: got %q, want %q", evts[0].Type, "node_started")
	}
	if evts[0].Data != `{"node":"impl"}` {
		t.Errorf("Data mismatch: got %q", evts[0].Data)
	}
	if evts[0].SessionID != id {
		t.Errorf("SessionID mismatch: got %q, want %q", evts[0].SessionID, id)
	}
}

// TestQueryEvents_OrderedByTimestampASC verifies ordering of multiple events.
func TestQueryEvents_OrderedByTimestampASC(t *testing.T) {
	s := openMemory(t)

	id, _ := s.CreateSession(Session{Name: "order-test"})
	base := time.Now().UTC()

	types := []string{"alpha", "beta", "gamma"}
	for i, typ := range types {
		s.LogEvent(SessionEvent{
			SessionID: id,
			Type:      typ,
			Timestamp: base.Add(time.Duration(i) * time.Second),
		})
	}

	evts, err := s.QueryEvents(id)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(evts) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evts))
	}
	for i, want := range types {
		if evts[i].Type != want {
			t.Errorf("event[%d]: got type %q, want %q", i, evts[i].Type, want)
		}
	}
}

// TestSearchEvents_MatchesType verifies FTS5 search on the type field.
func TestSearchEvents_MatchesType(t *testing.T) {
	s := openMemory(t)

	id, _ := s.CreateSession(Session{Name: "fts-test"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "node_started", Data: `{}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "node_completed", Data: `{}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "gate_scored", Data: `{}`})

	results, err := s.SearchEvents("node_started")
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Type != "node_started" {
		t.Errorf("unexpected type %q", results[0].Type)
	}
}

// TestSearchEvents_MatchesData verifies FTS5 search on the data field.
func TestSearchEvents_MatchesData(t *testing.T) {
	s := openMemory(t)

	id, _ := s.CreateSession(Session{Name: "fts-data-test"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "node_completed", Data: `{"node":"implementer","score":0.9}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "node_completed", Data: `{"node":"reviewer","score":0.7}`})

	results, err := s.SearchEvents("implementer")
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Data != `{"node":"implementer","score":0.9}` {
		t.Errorf("unexpected data: %q", results[0].Data)
	}
}

// TestSearchEvents_NoMatches verifies that SearchEvents returns an empty (non-nil)
// slice when nothing matches.
func TestSearchEvents_NoMatches(t *testing.T) {
	s := openMemory(t)

	id, _ := s.CreateSession(Session{Name: "fts-empty"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "node_started", Data: `{}`})

	results, err := s.SearchEvents("nonexistent_term_xyz")
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice for no matches")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestCreateSession_Template verifies that template is persisted and retrieved.
func TestCreateSession_Template(t *testing.T) {
	s := openMemory(t)

	id, err := s.CreateSession(Session{Name: "tmpl", Template: "gstack"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Template != "gstack" {
		t.Errorf("Template mismatch: got %q, want %q", got.Template, "gstack")
	}
}
