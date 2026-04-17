package daemon

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// seedEvents logs n events of the form "ev_1", "ev_2", ... into sessionID.
// Returns the IDs of the created events in order.
func seedEvents(t *testing.T, d *Daemon, sessionID string, n int) []int64 {
	t.Helper()
	var ids []int64
	for i := 1; i <= n; i++ {
		doRequest(t, d, "POST", "/sessions/"+sessionID+"/events", logEventRequest{
			Type: fmt.Sprintf("ev_%d", i),
		})
	}
	// Fetch all events to get their real IDs.
	rr := doRequest(t, d, "GET", "/sessions/"+sessionID+"/events", nil)
	all := decodeJSON[[]store.SessionEvent](t, rr)
	for _, e := range all {
		ids = append(ids, e.ID)
	}
	return ids
}

// TestGetEvents_BeforeBound seeds 10 events, queries with ?before=<id[4]>,
// and expects only the events with id < id[4] (i.e. events at indices 0..3
// of the full sorted set, which are session_created + ev_1 .. ev_3).
func TestGetEvents_BeforeBound(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "before-bound"}))

	ids := seedEvents(t, d, sess.ID, 10)
	// ids[0] = session_created, ids[1..10] = ev_1..ev_10.
	// We want events with id < ids[4] (exclusive), so we get ids[0..3].
	cutoff := ids[4]

	rr := doRequest(t, d, "GET", fmt.Sprintf("/sessions/%s/events?before=%d", sess.ID, cutoff), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)

	for _, e := range events {
		if e.ID >= cutoff {
			t.Errorf("event id=%d should not appear when before=%d", e.ID, cutoff)
		}
	}
	if len(events) != 4 {
		t.Errorf("expected 4 events (ids[0..3]), got %d: %+v", len(events), events)
	}
}

// TestGetEvents_AfterAndBefore seeds events and queries with both ?after= and ?before=.
func TestGetEvents_AfterAndBefore(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "after-before"}))

	ids := seedEvents(t, d, sess.ID, 10)
	// after=ids[3], before=ids[8] → ids[4..7] (4 events).
	afterID := ids[3]
	beforeID := ids[8]

	rr := doRequest(t, d, "GET", fmt.Sprintf("/sessions/%s/events?after=%d&before=%d", sess.ID, afterID, beforeID), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)

	for _, e := range events {
		if e.ID <= afterID {
			t.Errorf("event id=%d should not appear (not > afterID=%d)", e.ID, afterID)
		}
		if e.ID >= beforeID {
			t.Errorf("event id=%d should not appear (not < beforeID=%d)", e.ID, beforeID)
		}
	}
	if len(events) != 4 {
		t.Errorf("expected 4 events (ids[4..7]), got %d", len(events))
	}
}

// TestGetEvents_LimitCap seeds 100 events, queries ?limit=10, expects exactly 10.
func TestGetEvents_LimitCap(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "limit-cap"}))

	seedEvents(t, d, sess.ID, 100)

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/events?limit=10", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) != 10 {
		t.Errorf("expected 10 events, got %d", len(events))
	}
}

// TestGetEvents_LimitAbove1000ClampedTo1000 seeds 1500 events, queries
// ?limit=5000, expects exactly 1000 (server-side cap).
func TestGetEvents_LimitAbove1000ClampedTo1000(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "limit-clamp"}))

	// Seed 1500 events. This is slow but necessary to verify the cap.
	// Use direct store access to avoid HTTP overhead.
	for i := 0; i < 1500; i++ {
		if err := d.store.LogEvent(store.SessionEvent{SessionID: sess.ID, Type: fmt.Sprintf("ev_%d", i)}); err != nil {
			t.Fatalf("LogEvent: %v", err)
		}
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/events?limit=5000", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	events := decodeJSON[[]store.SessionEvent](t, rr)
	if len(events) != 1000 {
		t.Errorf("expected 1000 events (capped), got %d", len(events))
	}
}

// TestGetEvents_NegativeBefore400 verifies ?before=-1 returns 400.
func TestGetEvents_NegativeBefore400(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "neg-before"}))

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/events?before=-1", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestGetEvents_NegativeLimit400 verifies ?limit=-1 returns 400.
func TestGetEvents_NegativeLimit400(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "neg-limit"}))

	rr := doRequest(t, d, "GET", "/sessions/"+sess.ID+"/events?limit=-1", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
