package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/trace"
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

func TestQueryEventsAfter_FiltersByEventID(t *testing.T) {
	s := openMemory(t)

	id, _ := s.CreateSession(Session{Name: "after-test"})
	var eventIDs []int64
	for _, typ := range []string{"alpha", "beta", "gamma"} {
		if err := s.LogEvent(SessionEvent{SessionID: id, Type: typ}); err != nil {
			t.Fatalf("LogEvent(%s): %v", typ, err)
		}
		events, err := s.QueryEvents(id)
		if err != nil {
			t.Fatalf("QueryEvents: %v", err)
		}
		eventIDs = append(eventIDs, events[len(events)-1].ID)
	}

	evts, err := s.QueryEventsAfter(id, eventIDs[0])
	if err != nil {
		t.Fatalf("QueryEventsAfter: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("expected 2 events after first ID, got %d", len(evts))
	}
	if evts[0].Type != "beta" || evts[1].Type != "gamma" {
		t.Fatalf("unexpected events after first ID: %#v", evts)
	}
}

func TestQueryEventsForSessionsAfter_FiltersSessionsAndEventID(t *testing.T) {
	s := openMemory(t)

	sessionA, _ := s.CreateSession(Session{Name: "sess-a"})
	sessionB, _ := s.CreateSession(Session{Name: "sess-b"})
	sessionC, _ := s.CreateSession(Session{Name: "sess-c"})

	if err := s.LogEvent(SessionEvent{SessionID: sessionA, Type: "a-1"}); err != nil {
		t.Fatalf("LogEvent(a-1): %v", err)
	}
	eventsA, err := s.QueryEvents(sessionA)
	if err != nil {
		t.Fatalf("QueryEvents(a): %v", err)
	}
	cutoffID := eventsA[len(eventsA)-1].ID

	for _, evt := range []SessionEvent{
		{SessionID: sessionB, Type: "b-1"},
		{SessionID: sessionA, Type: "a-2"},
		{SessionID: sessionC, Type: "c-1"},
		{SessionID: sessionB, Type: "b-2"},
	} {
		if err := s.LogEvent(evt); err != nil {
			t.Fatalf("LogEvent(%s): %v", evt.Type, err)
		}
	}

	evts, err := s.QueryEventsForSessionsAfter([]string{sessionA, sessionB}, cutoffID)
	if err != nil {
		t.Fatalf("QueryEventsForSessionsAfter: %v", err)
	}
	if len(evts) != 3 {
		t.Fatalf("expected 3 events, got %d", len(evts))
	}
	got := []string{evts[0].Type, evts[1].Type, evts[2].Type}
	want := []string{"b-1", "a-2", "b-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
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

// --- SearchEventsV1 tests ---

func TestSearchEventsV1_QPredicateOnly(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "v1-q"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "bridge:hello", Data: `{"msg":"found"}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "other:thing", Data: `{"msg":"not"}`})

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{Q: "found"})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Type != "bridge:hello" {
		t.Errorf("unexpected type %q", results[0].Type)
	}
}

func TestSearchEventsV1_SessionFilter(t *testing.T) {
	s := openMemory(t)
	idA, _ := s.CreateSession(Session{Name: "sess-a"})
	idB, _ := s.CreateSession(Session{Name: "sess-b"})
	s.LogEvent(SessionEvent{SessionID: idA, Type: "ev", Data: `{"x":"a"}`})
	s.LogEvent(SessionEvent{SessionID: idA, Type: "ev", Data: `{"x":"a2"}`})
	s.LogEvent(SessionEvent{SessionID: idB, Type: "ev", Data: `{"x":"b"}`})

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{SessionID: idA})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 events for session A, got %d", len(results))
	}
	for _, r := range results {
		if r.SessionID != idA {
			t.Errorf("expected session %q, got %q", idA, r.SessionID)
		}
	}
}

func TestSearchEventsV1_TypePrefix(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "type-prefix"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "bridge:foo", Data: `{}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "bridge:bar", Data: `{}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "other:baz", Data: `{}`})

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{TypePrefix: "bridge:"})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 bridge events, got %d", len(results))
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Type, "bridge:") {
			t.Errorf("unexpected type %q", r.Type)
		}
	}
}

func TestSearchEventsV1_AgentFilter(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "agent-filter"})
	s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{"agent":"sup"}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{"agent":"impl"}`})
	s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{"agent":"sup"}`})

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{Agent: "sup"})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 sup events, got %d", len(results))
	}
}

func TestSearchEventsV1_AfterBefore(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "after-before"})
	for i := 0; i < 5; i++ {
		s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{}`})
	}
	all, _ := s.QueryEvents(id)
	if len(all) < 5 {
		t.Fatalf("need 5 events, got %d", len(all))
	}
	afterID := all[1].ID  // skip first two
	beforeID := all[4].ID // skip last one

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{AfterID: afterID, BeforeID: beforeID})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	// Should return events[2] and events[3] (between after and before exclusive)
	if len(results) != 2 {
		t.Fatalf("expected 2 events in window, got %d", len(results))
	}
	for _, r := range results {
		if r.ID <= afterID || r.ID >= beforeID {
			t.Errorf("event id %d out of window (%d, %d)", r.ID, afterID, beforeID)
		}
	}
}

func TestSearchEventsV1_LimitCap(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "limit-cap"})
	for i := 0; i < 1200; i++ {
		s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{}`})
	}

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{DescOrder: true})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) != 1000 {
		t.Fatalf("expected 1000 (cap), got %d", len(results))
	}
}

func TestSearchEventsV1_DescOrder(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "desc-order"})
	for i := 0; i < 5; i++ {
		s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{}`})
	}

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{DescOrder: true})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].ID > results[i-1].ID {
			t.Errorf("results not in DESC order: [%d].id=%d > [%d].id=%d", i, results[i].ID, i-1, results[i-1].ID)
		}
	}
}

func TestSearchEventsV1_EmptyAllPredicates(t *testing.T) {
	s := openMemory(t)
	id, _ := s.CreateSession(Session{Name: "empty-preds"})
	for i := 0; i < 5; i++ {
		s.LogEvent(SessionEvent{SessionID: id, Type: "ev", Data: `{}`})
	}

	results, err := s.SearchEventsV1(context.Background(), SearchPredicates{DescOrder: true})
	if err != nil {
		t.Fatalf("SearchEventsV1: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	// Verify DESC ordering.
	for i := 1; i < len(results); i++ {
		if results[i].ID > results[i-1].ID {
			t.Errorf("not DESC: results[%d].ID=%d > results[%d].ID=%d", i, results[i].ID, i-1, results[i-1].ID)
		}
	}
}

func TestSearchEventsV1_ContextCancelled(t *testing.T) {
	s := openMemory(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := s.SearchEventsV1(ctx, SearchPredicates{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// TestMaxEventID_EmptyAndNonEmpty verifies that MaxEventID returns 0 for an
// empty store and the correct max ID after events are written.
func TestMaxEventID_EmptyAndNonEmpty(t *testing.T) {
	s := openMemory(t)

	// Empty store should return 0.
	id, err := s.MaxEventID()
	if err != nil {
		t.Fatalf("MaxEventID on empty store: %v", err)
	}
	if id != 0 {
		t.Errorf("empty store: expected 0, got %d", id)
	}

	// Create a session and write some events.
	sessID, _ := s.CreateSession(Session{Name: "max-id-test"})
	for _, typ := range []string{"a", "b", "c"} {
		if err := s.LogEvent(SessionEvent{SessionID: sessID, Type: typ, Data: "{}"}); err != nil {
			t.Fatalf("LogEvent(%s): %v", typ, err)
		}
	}

	maxID, err := s.MaxEventID()
	if err != nil {
		t.Fatalf("MaxEventID after writes: %v", err)
	}
	if maxID <= 0 {
		t.Fatalf("expected positive max ID, got %d", maxID)
	}

	// Verify it matches the last event.
	events, err := s.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	lastEventID := events[len(events)-1].ID
	if maxID != lastEventID {
		t.Errorf("MaxEventID=%d != last event ID=%d", maxID, lastEventID)
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

// TestMigrate_Idempotent verifies that calling Migrate twice does not error
// (covers the hermes_session_id column addition being idempotent).
func TestMigrate_Idempotent(t *testing.T) {
	s := openMemory(t)
	// Migrate was already called by Open. Call it again directly.
	if err := Migrate(s.db); err != nil {
		t.Fatalf("second Migrate call failed: %v", err)
	}
}

// TestUpdateAgentRunHermesSessionID verifies that hermes_session_id is persisted
// and round-trips correctly via GetAgentRun.
func TestUpdateAgentRunHermesSessionID(t *testing.T) {
	s := openMemory(t)

	sessID, err := s.CreateSession(Session{Name: "hermes-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.CreateAgentRun(AgentRun{
		SessionID: sessID,
		Name:      "planner",
	}); err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}

	const wantHermesID = "hermes-abc-123"
	if err := s.UpdateAgentRunHermesSessionID(sessID, "planner", wantHermesID); err != nil {
		t.Fatalf("UpdateAgentRunHermesSessionID: %v", err)
	}

	run, err := s.GetAgentRun(sessID, "planner")
	if err != nil {
		t.Fatalf("GetAgentRun: %v", err)
	}
	if run.HermesSessionID != wantHermesID {
		t.Errorf("HermesSessionID: got %q, want %q", run.HermesSessionID, wantHermesID)
	}
}

// TestCreateMessage_PendingMessages verifies that CreateMessage + PendingMessages
// returns only undelivered messages for the correct recipient.
func TestCreateMessage_PendingMessages(t *testing.T) {
	s := openMemory(t)

	sessID, err := s.CreateSession(Session{Name: "msg-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	base := time.Now().UTC()

	// Message for agent-a.
	id1, err := s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-a",
		Type:        "instruction",
		Content:     "do task 1",
		CreatedAt:   base,
	})
	if err != nil {
		t.Fatalf("CreateMessage 1: %v", err)
	}
	_ = id1

	// Message for agent-b (should not appear in agent-a's pending list).
	_, err = s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-b",
		Type:        "instruction",
		Content:     "do task 2",
		CreatedAt:   base.Add(time.Millisecond),
	})
	if err != nil {
		t.Fatalf("CreateMessage 2: %v", err)
	}

	pending, err := s.PendingMessages(sessID, "agent-a", "")
	if err != nil {
		t.Fatalf("PendingMessages: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending message for agent-a, got %d", len(pending))
	}
	if pending[0].Content != "do task 1" {
		t.Errorf("unexpected content: %q", pending[0].Content)
	}
	if pending[0].Delivered {
		t.Errorf("expected Delivered=false")
	}
}

// TestMarkDelivered_ExcludesFromPending verifies that MarkDelivered causes a
// message to no longer appear in PendingMessages.
func TestMarkDelivered_ExcludesFromPending(t *testing.T) {
	s := openMemory(t)

	sessID, _ := s.CreateSession(Session{Name: "deliver-test"})

	msgID, err := s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-a",
		Content:     "deliver me",
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	// Confirm it's pending.
	pending, err := s.PendingMessages(sessID, "agent-a", "")
	if err != nil {
		t.Fatalf("PendingMessages before mark: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending message, got %d", len(pending))
	}

	if err := s.MarkDelivered(msgID); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// Confirm it's gone from pending.
	pending, err = s.PendingMessages(sessID, "agent-a", "")
	if err != nil {
		t.Fatalf("PendingMessages after mark: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending messages after delivery, got %d", len(pending))
	}
}

// TestPendingMessages_AfterID verifies that the afterID parameter filters out
// messages created at or before the reference message's created_at.
func TestPendingMessages_AfterID(t *testing.T) {
	s := openMemory(t)

	sessID, _ := s.CreateSession(Session{Name: "after-id-test"})

	base := time.Now().UTC()
	id1, err := s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-a",
		Content:     "first",
		CreatedAt:   base,
	})
	if err != nil {
		t.Fatalf("CreateMessage 1: %v", err)
	}
	_, err = s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-a",
		Content:     "second",
		CreatedAt:   base.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("CreateMessage 2: %v", err)
	}
	_, err = s.CreateMessage(Message{
		SessionID:   sessID,
		SenderID:    "planner",
		RecipientID: "agent-a",
		Content:     "third",
		CreatedAt:   base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("CreateMessage 3: %v", err)
	}

	// Ask for messages after the first one.
	pending, err := s.PendingMessages(sessID, "agent-a", id1)
	if err != nil {
		t.Fatalf("PendingMessages(afterID): %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 messages after id1, got %d", len(pending))
	}
	if pending[0].Content != "second" {
		t.Errorf("pending[0]: got %q, want %q", pending[0].Content, "second")
	}
	if pending[1].Content != "third" {
		t.Errorf("pending[1]: got %q, want %q", pending[1].Content, "third")
	}
}

func TestMigrate_TraceColumnsAndReaderCursors(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, err := s.DB().Query("PRAGMA table_info(events)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, c := range []string{"trace_file", "trace_offset", "trace_length"} {
		if !cols[c] {
			t.Errorf("events missing column %s", c)
		}
	}
	if _, err := s.DB().Exec(`INSERT INTO reader_cursors(reader_id, session_id, last_id) VALUES ('r','s',0)`); err != nil {
		t.Fatalf("reader_cursors insert: %v", err)
	}
	// Upsert semantics: PK (reader_id, session_id) means second insert with same pair must conflict.
	_, err = s.DB().Exec(`INSERT INTO reader_cursors(reader_id, session_id, last_id) VALUES ('r','s',5)`)
	if err == nil {
		t.Fatal("expected PK conflict on duplicate (reader_id, session_id)")
	}
}

// seedStoreEvents inserts n events into the store for sessionID and returns
// the assigned IDs in ascending order.
func seedStoreEvents(t *testing.T, s *Store, sessionID string, n int) []int64 {
	t.Helper()
	for i := 1; i <= n; i++ {
		if err := s.LogEvent(SessionEvent{
			SessionID: sessionID,
			Type:      fmt.Sprintf("ev_%d", i),
		}); err != nil {
			t.Fatalf("LogEvent %d: %v", i, err)
		}
	}
	events, err := s.QueryEvents(sessionID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	ids := make([]int64, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}

// TestQueryEventsWindow_AfterOnly verifies that afterID > 0 returns only events
// with id > afterID.
func TestQueryEventsWindow_AfterOnly(t *testing.T) {
	s := openMemory(t)
	sessID, err := s.CreateSession(Session{Name: "win-after"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ids := seedStoreEvents(t, s, sessID, 10)
	cutoff := ids[4] // events at ids[5..9] should be returned

	got, err := s.QueryEventsWindow(sessID, cutoff, 0, 0)
	if err != nil {
		t.Fatalf("QueryEventsWindow: %v", err)
	}
	for _, e := range got {
		if e.ID <= cutoff {
			t.Errorf("event id=%d should be > cutoff=%d", e.ID, cutoff)
		}
	}
	if len(got) != 5 {
		t.Errorf("expected 5 events after ids[4], got %d", len(got))
	}
}

// TestQueryEventsWindow_BeforeOnly verifies that beforeID > 0 returns only events
// with id < beforeID.
func TestQueryEventsWindow_BeforeOnly(t *testing.T) {
	s := openMemory(t)
	sessID, err := s.CreateSession(Session{Name: "win-before"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ids := seedStoreEvents(t, s, sessID, 10)
	cutoff := ids[5] // events at ids[0..4] should be returned

	got, err := s.QueryEventsWindow(sessID, 0, cutoff, 0)
	if err != nil {
		t.Fatalf("QueryEventsWindow: %v", err)
	}
	for _, e := range got {
		if e.ID >= cutoff {
			t.Errorf("event id=%d should be < cutoff=%d", e.ID, cutoff)
		}
	}
	if len(got) != 5 {
		t.Errorf("expected 5 events before ids[5], got %d", len(got))
	}
}

// TestQueryEventsWindow_AfterAndBefore verifies that both bounds work together.
func TestQueryEventsWindow_AfterAndBefore(t *testing.T) {
	s := openMemory(t)
	sessID, err := s.CreateSession(Session{Name: "win-both"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ids := seedStoreEvents(t, s, sessID, 10)
	afterID := ids[2]  // exclusive lower bound
	beforeID := ids[7] // exclusive upper bound
	// Expected: ids[3..6] = 4 events

	got, err := s.QueryEventsWindow(sessID, afterID, beforeID, 0)
	if err != nil {
		t.Fatalf("QueryEventsWindow: %v", err)
	}
	for _, e := range got {
		if e.ID <= afterID {
			t.Errorf("event id=%d should be > afterID=%d", e.ID, afterID)
		}
		if e.ID >= beforeID {
			t.Errorf("event id=%d should be < beforeID=%d", e.ID, beforeID)
		}
	}
	if len(got) != 4 {
		t.Errorf("expected 4 events in window, got %d", len(got))
	}
}

// TestInsertEventWithSpill_SetsFragmentColumns verifies that InsertEventWithSpill
// populates trace_file, trace_offset, and trace_length when a non-zero Fragment is
// provided, and that QueryEvents returns the same values in the SessionEvent struct.
func TestInsertEventWithSpill_SetsFragmentColumns(t *testing.T) {
	s := openMemory(t)

	sessID, err := s.CreateSession(Session{Name: "spill-test", LogLevel: "trace"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	frag := trace.Fragment{
		Path:   "/tmp/traces/sess/agent/0001.jsonl",
		Offset: 1234,
		Length: 5678,
	}
	evt := SessionEvent{
		SessionID: sessID,
		Type:      "tool_call",
		Data:      `{"agent":"implementer","_trace":true}`,
	}
	if err := s.InsertEventWithSpill(evt, frag); err != nil {
		t.Fatalf("InsertEventWithSpill: %v", err)
	}

	// Read back the raw columns via s.DB().
	var traceFile sql.NullString
	var traceOffset, traceLength sql.NullInt64
	row := s.DB().QueryRow(
		`SELECT trace_file, trace_offset, trace_length FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sessID,
	)
	if err := row.Scan(&traceFile, &traceOffset, &traceLength); err != nil {
		t.Fatalf("SELECT trace columns: %v", err)
	}

	if !traceFile.Valid || traceFile.String != frag.Path {
		t.Errorf("trace_file: got %v, want %q", traceFile, frag.Path)
	}
	if !traceOffset.Valid || traceOffset.Int64 != frag.Offset {
		t.Errorf("trace_offset: got %v, want %d", traceOffset, frag.Offset)
	}
	if !traceLength.Valid || traceLength.Int64 != frag.Length {
		t.Errorf("trace_length: got %v, want %d", traceLength, frag.Length)
	}

	// Also verify the columns are returned via QueryEvents (not just raw DB).
	events, err := s.QueryEvents(sessID)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("QueryEvents: expected 1 event, got %d", len(events))
	}
	got := events[0]

	if got.TraceFile != frag.Path {
		t.Errorf("QueryEvents TraceFile: got %q, want %q", got.TraceFile, frag.Path)
	}
	if got.TraceOffset != frag.Offset {
		t.Errorf("QueryEvents TraceOffset: got %d, want %d", got.TraceOffset, frag.Offset)
	}
	if got.TraceLength != frag.Length {
		t.Errorf("QueryEvents TraceLength: got %d, want %d", got.TraceLength, frag.Length)
	}
	// TraceFragment should be derived from the basename without extensions.
	wantFragment := "0001"
	if got.TraceFragment != wantFragment {
		t.Errorf("QueryEvents TraceFragment: got %q, want %q", got.TraceFragment, wantFragment)
	}
}

// TestInsertEventWithSpill_NullWhenFragmentEmpty verifies that InsertEventWithSpill
// stores NULL trace columns when a zero Fragment (frag.Path == "") is provided.
func TestInsertEventWithSpill_NullWhenFragmentEmpty(t *testing.T) {
	s := openMemory(t)

	sessID, err := s.CreateSession(Session{Name: "no-spill-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	evt := SessionEvent{
		SessionID: sessID,
		Type:      "tool_call",
		Data:      `{"agent":"implementer","result":"ok"}`,
	}
	if err := s.InsertEventWithSpill(evt, trace.Fragment{}); err != nil {
		t.Fatalf("InsertEventWithSpill (empty fragment): %v", err)
	}

	var traceFile sql.NullString
	var traceOffset, traceLength sql.NullInt64
	row := s.DB().QueryRow(
		`SELECT trace_file, trace_offset, trace_length FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sessID,
	)
	if err := row.Scan(&traceFile, &traceOffset, &traceLength); err != nil {
		t.Fatalf("SELECT trace columns: %v", err)
	}

	if traceFile.Valid {
		t.Errorf("trace_file: expected NULL, got %q", traceFile.String)
	}
	if traceOffset.Valid {
		t.Errorf("trace_offset: expected NULL, got %d", traceOffset.Int64)
	}
	if traceLength.Valid {
		t.Errorf("trace_length: expected NULL, got %d", traceLength.Int64)
	}
}
