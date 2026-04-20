package daemon

// sse_test.go covers eng-review test-plan items:
//   T1   — multi-session cursor ordering
//   T4   — keepalive comment every <interval>
//   T5   — reconnect gap-free with global cursor
//   T_A0 — full backlog on connect without ?after=
//   T_A1_corner — mid-stream subscription add loses history (documents the corner)

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithKeepalive creates a daemon with a short SSE keepalive interval
// so T4 can verify keepalives without sleeping 15 seconds.
func testDaemonWithKeepalive(t *testing.T, keepalive time.Duration) *Daemon {
	t.Helper()
	d := testDaemon(t)
	d.sseKeepaliveInterval = keepalive
	return d
}

// TestSSE_T1_MultiSessionCursorOrdering seeds interleaved events across two
// sessions and verifies that GET /events/stream?sessions=A,B&after=0 delivers
// all events in global event-ID order and that IDs are strictly monotonic.
//
// Test-plan item: T1 — multi-session cursor.
func TestSSE_T1_MultiSessionCursorOrdering(t *testing.T) {
	d := testDaemon(t)

	// Create two sessions.
	s1 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "cursor-a"}))
	s2 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "cursor-b"}))

	// Log interleaved events: A1, B1, A2, B2, A3.
	// Each doRequest to /events goes through handleLogEvent → store.LogEvent
	// so IDs increment globally.
	doRequest(t, d, "POST", "/sessions/"+s1.ID+"/events", logEventRequest{Type: "A1"})
	doRequest(t, d, "POST", "/sessions/"+s2.ID+"/events", logEventRequest{Type: "B1"})
	doRequest(t, d, "POST", "/sessions/"+s1.ID+"/events", logEventRequest{Type: "A2"})
	doRequest(t, d, "POST", "/sessions/"+s2.ID+"/events", logEventRequest{Type: "B2"})
	doRequest(t, d, "POST", "/sessions/"+s1.ID+"/events", logEventRequest{Type: "A3"})

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Connect with ?after=0 to receive full backlog (including session_created events).
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s,%s&after=0", server.URL, s1.ID, s2.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	// Collect: daemon_hello + session_created(s1) + session_created(s2) + 5 domain events = 8.
	// We pass 8 so readSSEFrames returns quickly after all 8; the wait=1s on the
	// server side keeps the connection open if we're slow but we have a ctx timeout anyway.
	frames := readSSEFrames(t, resp.Body, 8, 3*time.Second)
	if len(frames) == 0 {
		t.Fatal("no frames received")
	}
	if frames[0].event != "daemon_hello" {
		t.Fatalf("first frame must be daemon_hello, got %q", frames[0].event)
	}

	// Extract domain frames and verify monotonically increasing IDs.
	// Domain frames carry the actual event type (e.g. "session_created", "A1").
	var lastID int64
	seenTypes := map[string]bool{}
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		var evt store.SessionEvent
		if err := json.Unmarshal([]byte(f.data), &evt); err != nil {
			t.Fatalf("parse domain frame %q: %v", f.event, err)
		}
		if evt.ID <= lastID {
			t.Errorf("non-monotonic ID: got %d after %d (type=%s)", evt.ID, lastID, evt.Type)
		}
		lastID = evt.ID
		seenTypes[evt.Type] = true
	}

	// All 5 interleaved events must appear.
	for _, want := range []string{"A1", "B1", "A2", "B2", "A3"} {
		if !seenTypes[want] {
			t.Errorf("event %q not received in stream", want)
		}
	}
}

// TestSSE_T4_KeepaliveComment verifies that the SSE handler emits ": keep-alive"
// comments when no domain events arrive. Uses a very short keepalive interval
// (50ms) to avoid sleeping 15 seconds in CI.
//
// Test-plan item: T4 — keepalive comment every 15s.
func TestSSE_T4_KeepaliveComment(t *testing.T) {
	// Use 50ms keepalive interval so the test completes in ~200ms.
	d := testDaemonWithKeepalive(t, 50*time.Millisecond)

	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "keepalive-test"}))

	// Snapshot max ID before connect so we pass ?after= and get a clean stream
	// with no backlog events (keepalive must arrive when no domain events).
	maxID, _ := d.store.MaxEventID()

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=%d", server.URL, sess.ID, maxID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	// Read the raw body looking for the keepalive comment. We can't use
	// readSSEFrames because comments don't produce frames. Scan line-by-line.
	doneCh := make(chan bool, 1)
	go func() {
		buf := make([]byte, 4096)
		var accumulated strings.Builder
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				accumulated.Write(buf[:n])
				if strings.Contains(accumulated.String(), ": keep-alive") {
					doneCh <- true
					return
				}
			}
			if err != nil {
				doneCh <- false
				return
			}
		}
	}()

	select {
	case found := <-doneCh:
		if !found {
			t.Fatal("body closed before keep-alive comment seen")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for keep-alive comment")
	}
}

// TestSSE_T5_ReconnectGapFree verifies that a consumer who disconnects and
// reconnects with ?after=<lastID> receives exactly the events written during
// the disconnect window — no duplicates, no gaps.
//
// Test-plan item: T5 — reconnect gap-free with global cursor.
func TestSSE_T5_ReconnectGapFree(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "reconnect-test"}))

	// Pre-seed 5 events that the first connection will consume.
	for i := 0; i < 5; i++ {
		doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: fmt.Sprintf("pre_%d", i)})
	}

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	// First connection: read all pre-seeded events using ?after=0.
	var reconnectAfter int64
	{
		ctx1, cancel1 := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel1()
		req, _ := http.NewRequestWithContext(ctx1, "GET",
			fmt.Sprintf("%s/events/stream?sessions=%s&after=0", server.URL, sess.ID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("first connection: %v", err)
		}
		// Read hello + session_created + 5 pre-seeded = 7 frames.
		frames := readSSEFrames(t, resp.Body, 7, 2*time.Second)
		resp.Body.Close()

		// Find the last domain frame ID.
		for _, f := range frames {
			if !isSSEControlFrame(f.event) && f.id != "" {
				var id int64
				fmt.Sscan(f.id, &id)
				if id > reconnectAfter {
					reconnectAfter = id
				}
			}
		}
		if reconnectAfter == 0 {
			t.Fatal("could not determine reconnectAfter from first connection")
		}
	}

	// Write events 43-50 during the disconnect window.
	for i := 0; i < 8; i++ {
		doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: fmt.Sprintf("gap_%d", i)})
	}

	// Second connection: reconnect with ?after=reconnectAfter.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=%d", server.URL, sess.ID, reconnectAfter), nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	defer resp2.Body.Close()

	// Read hello + 8 gap events.
	frames2 := readSSEFrames(t, resp2.Body, 9, 2*time.Second)
	if len(frames2) == 0 {
		t.Fatal("no frames from second connection")
	}
	if frames2[0].event != "daemon_hello" {
		t.Fatalf("first frame must be daemon_hello, got %q", frames2[0].event)
	}

	// Collect IDs from second connection and verify:
	// 1. No IDs <= reconnectAfter (no duplicates).
	// 2. All gap events present (no gaps).
	var gotIDs []int64
	seenGap := map[string]bool{}
	for _, f := range frames2[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		var evt store.SessionEvent
		if err := json.Unmarshal([]byte(f.data), &evt); err != nil {
			continue
		}
		if evt.ID <= reconnectAfter {
			t.Errorf("duplicate: got event id=%d which is <= reconnectAfter=%d", evt.ID, reconnectAfter)
		}
		gotIDs = append(gotIDs, evt.ID)
		seenGap[evt.Type] = true
	}

	// Verify gap events are in the stream.
	for i := 0; i < 8; i++ {
		typ := fmt.Sprintf("gap_%d", i)
		if !seenGap[typ] {
			t.Errorf("event %q missing from second connection stream", typ)
		}
	}

	// Verify strict ascending order.
	for i := 1; i < len(gotIDs); i++ {
		if gotIDs[i] <= gotIDs[i-1] {
			t.Errorf("non-monotonic IDs in second connection: gotIDs[%d]=%d <= gotIDs[%d]=%d",
				i, gotIDs[i], i-1, gotIDs[i-1])
		}
	}
}

// TestSSE_TA0_BacklogOnConnectNoAfter verifies that connecting to
// GET /events/stream without any ?after= or Last-Event-ID delivers the full
// backlog from id=0, matching LOG_FORMAT.md §4.
//
// Test-plan item: T_A0 — full backlog on connect without ?after=.
func TestSSE_TA0_BacklogOnConnectNoAfter(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "backlog-test"}))

	// Seed 10 events (plus session_created = 11 total).
	for i := 0; i < 10; i++ {
		doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: fmt.Sprintf("ev_%02d", i)})
	}

	// Snapshot max ID so we know what the full backlog contains.
	maxID, err := d.store.MaxEventID()
	if err != nil {
		t.Fatalf("MaxEventID: %v", err)
	}

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Connect WITHOUT ?after= (the A0 default: full backlog from 0).
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s", server.URL, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}

	// Read hello + 11 domain events = 12 frames.
	frames := readSSEFrames(t, resp.Body, 12, 3*time.Second)
	if len(frames) == 0 {
		t.Fatal("no frames received")
	}
	if frames[0].event != "daemon_hello" {
		t.Fatalf("first frame must be daemon_hello, got %q", frames[0].event)
	}

	// daemon_hello.last_id must equal maxID (the store high-water mark at connect time).
	var helloPayload map[string]any
	if err := json.Unmarshal([]byte(frames[0].data), &helloPayload); err != nil {
		t.Fatalf("parse daemon_hello: %v", err)
	}
	gotLastID, _ := helloPayload["last_id"].(float64)
	if int64(gotLastID) != maxID {
		t.Errorf("daemon_hello.last_id = %v, want %d (maxID)", gotLastID, maxID)
	}

	// Collect domain event types and verify all seeded events arrived.
	var seenTypes []string
	var lastReceivedID int64
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		var evt store.SessionEvent
		if err := json.Unmarshal([]byte(f.data), &evt); err != nil {
			t.Fatalf("parse domain frame %q: %v", f.event, err)
		}
		seenTypes = append(seenTypes, evt.Type)
		if evt.ID > lastReceivedID {
			lastReceivedID = evt.ID
		}
	}

	// Verify all seeded events are present.
	seenMap := make(map[string]bool, len(seenTypes))
	for _, s := range seenTypes {
		seenMap[s] = true
	}
	for i := 0; i < 10; i++ {
		typ := fmt.Sprintf("ev_%02d", i)
		if !seenMap[typ] {
			t.Errorf("expected event type %q in backlog, not found", typ)
		}
	}

	// Sanity: session_created should also be there.
	if !seenMap["session_created"] {
		t.Error("session_created event missing from backlog")
	}

	// IDs must be monotonically increasing (no gaps, no duplicates).
	var prevID int64
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		var evt store.SessionEvent
		json.Unmarshal([]byte(f.data), &evt)
		if evt.ID <= prevID {
			t.Errorf("non-monotonic ID sequence: %d after %d", evt.ID, prevID)
		}
		prevID = evt.ID
	}
}

// TestSSE_TA1_Corner_MidStreamSubscriptionLosesHistory documents and tests the
// known limitation when a consumer adds a new session to an existing subscription:
// events for the new session that were written BEFORE the consumer's cursor are
// NOT delivered on reconnect. This is expected behaviour — cragd avoids the
// corner by subscribing per-session so each session has its own cursor.
//
// Scenario: consumer watched A+B up to cursor=N. Session C existed all along but
// was not subscribed. On reconnect with A,B,C&after=N, C events with id<=N are
// invisible even though C had events written before N.
//
// Test-plan item: T_A1_corner — mid-stream subscription add loses history.
func TestSSE_TA1_Corner_MidStreamSubscriptionLosesHistory(t *testing.T) {
	d := testDaemon(t)

	// Create ALL sessions upfront so C's session_created has a low global ID.
	sA := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "A"}))
	sB := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "B"}))
	sC := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "C"}))

	// Seed events for all three sessions BEFORE taking the cursor snapshot.
	// C_historical must have a global ID <= cursorAfterAB.
	for i := 0; i < 3; i++ {
		doRequest(t, d, "POST", "/sessions/"+sA.ID+"/events", logEventRequest{Type: fmt.Sprintf("A_%d", i)})
	}
	doRequest(t, d, "POST", "/sessions/"+sC.ID+"/events", logEventRequest{Type: "C_historical"})
	for i := 0; i < 3; i++ {
		doRequest(t, d, "POST", "/sessions/"+sB.ID+"/events", logEventRequest{Type: fmt.Sprintf("B_%d", i)})
	}

	// Consumer originally subscribed only to A+B; cursor is now at max of A+B+C-historical.
	cursorAfterAB, _ := d.store.MaxEventID()

	// Seed C_post AFTER taking the cursor so it has id > cursorAfterAB.
	doRequest(t, d, "POST", "/sessions/"+sC.ID+"/events", logEventRequest{Type: "C_post"})

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Reconnect with sessions=A,B,C and after=cursorAfterAB.
	// C_historical (id <= cursorAfterAB) must NOT appear.
	// C_post (id > cursorAfterAB) MUST appear.
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s,%s,%s&after=%d",
			server.URL, sA.ID, sB.ID, sC.ID, cursorAfterAB), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	// Read hello + C_post = 2 frames.
	frames := readSSEFrames(t, resp.Body, 2, 2*time.Second)

	seenHistorical := false
	seenPost := false
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		if strings.Contains(f.data, "C_historical") {
			seenHistorical = true
		}
		if strings.Contains(f.data, "C_post") {
			seenPost = true
		}
	}

	// C_historical must NOT appear (it predates the cursor).
	if seenHistorical {
		t.Error("C_historical appeared in stream even though it predates the reconnect cursor; this violates the expected corner behaviour")
	}

	// C_post MUST appear (it was written after the cursor).
	if !seenPost {
		t.Error("C_post not found in stream after reconnect with after=cursorAfterAB")
	}

	// Document the limitation explicitly so future readers understand why.
	t.Log("CORNER DOCUMENTED: C_historical is lost because consumer reconnected with after=cursorAfterAB; " +
		"cragd avoids this by subscribing per-session so the per-session cursor is always correct")
}

// --- Task 5.1: filter param tests ---

// TestSSE_AgentFilter verifies that ?agent=<name> filters events so only those
// from the specified agent are emitted. Three events are seeded: two from
// "supervisor" and one from "backend-dev". Only the backend-dev event must appear.
func TestSSE_AgentFilter(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "agent-filter-test"}))

	// Seed events: two from supervisor, one from backend-dev.
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
		Type: "tool_call",
		Data: `{"agent":"supervisor","msg":"sup-1"}`,
	})
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
		Type: "tool_call",
		Data: `{"agent":"backend-dev","msg":"be-1"}`,
	})
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
		Type: "tool_call",
		Data: `{"agent":"supervisor","msg":"sup-2"}`,
	})

	// Snapshot maxID so we can request backlog only.
	maxID, _ := d.store.MaxEventID()
	_ = maxID

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Connect with after=0 and agent=backend-dev filter.
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=0&agent=backend-dev", server.URL, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Expect: daemon_hello + session_created(filtered out) + 1 backend-dev event.
	// session_created has no agent field so it will be filtered out.
	// We read 2 frames: hello + the backend-dev event.
	frames := readSSEFrames(t, resp.Body, 2, 3*time.Second)
	if len(frames) == 0 {
		t.Fatal("no frames received")
	}
	if frames[0].event != "daemon_hello" {
		t.Fatalf("first frame must be daemon_hello, got %q", frames[0].event)
	}

	// Collect domain events received.
	var domainFrames []sseFrame
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		domainFrames = append(domainFrames, f)
	}

	// Must have exactly one domain event (the backend-dev one).
	if len(domainFrames) != 1 {
		t.Fatalf("expected 1 domain event for agent=backend-dev, got %d", len(domainFrames))
	}
	if !strings.Contains(domainFrames[0].data, "backend-dev") {
		t.Errorf("expected backend-dev event, got data: %s", domainFrames[0].data)
	}
	if strings.Contains(domainFrames[0].data, "supervisor") {
		t.Errorf("supervisor event leaked through agent filter: %s", domainFrames[0].data)
	}
}

// TestSSE_TypePrefixFilter verifies that ?type_prefix=<prefix> filters events so
// only those whose Type starts with the prefix are emitted.
func TestSSE_TypePrefixFilter(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "type-prefix-test"}))

	// Seed events of various types.
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: "bridge:tool_started"})
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: "bridge:tool_completed"})
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{Type: "agent_status:planning"})

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Connect filtering for only bridge: events.
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=0&type_prefix=bridge:", server.URL, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Expect: daemon_hello + 2 bridge: events (session_created and agent_status:planning filtered out).
	frames := readSSEFrames(t, resp.Body, 3, 3*time.Second)
	if len(frames) == 0 {
		t.Fatal("no frames received")
	}
	if frames[0].event != "daemon_hello" {
		t.Fatalf("first frame must be daemon_hello, got %q", frames[0].event)
	}

	var domainFrames []sseFrame
	for _, f := range frames[1:] {
		if isSSEControlFrame(f.event) {
			continue
		}
		domainFrames = append(domainFrames, f)
	}

	if len(domainFrames) != 2 {
		t.Fatalf("expected 2 domain events for type_prefix=bridge:, got %d", len(domainFrames))
	}
	for _, f := range domainFrames {
		if !strings.HasPrefix(f.event, "bridge:") {
			t.Errorf("non-bridge event leaked through type_prefix filter: event=%q", f.event)
		}
	}
}

// TestSSE_TierFilter verifies that ?tier=standard returns only standard events,
// while ?tier=trace returns both standard and trace events.
func TestSSE_TierFilter(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "tier-filter-test"}))

	// Seed one standard event and one trace-tier event (type starts with trace:).
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
		Type: "tool_call",
		Data: `{"agent":"supervisor"}`,
	})
	doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
		Type: "trace:fs_snapshot",
		Data: `{"agent":"supervisor"}`,
	})

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	// --- tier=standard: should receive session_created + tool_call, NOT trace:fs_snapshot ---
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "GET",
			fmt.Sprintf("%s/events/stream?sessions=%s&after=0&tier=standard", server.URL, sess.ID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tier=standard: GET /events/stream: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tier=standard: expected 200, got %d", resp.StatusCode)
		}

		// hello + session_created + tool_call = 3 frames.
		frames := readSSEFrames(t, resp.Body, 3, 3*time.Second)

		for _, f := range frames[1:] {
			if isSSEControlFrame(f.event) {
				continue
			}
			if strings.HasPrefix(f.event, "trace:") {
				t.Errorf("tier=standard: trace event leaked through: event=%q", f.event)
			}
		}
	}()

	// --- tier=trace: should receive all events including trace:fs_snapshot ---
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		req, _ := http.NewRequestWithContext(ctx, "GET",
			fmt.Sprintf("%s/events/stream?sessions=%s&after=0&tier=trace", server.URL, sess.ID), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tier=trace: GET /events/stream: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tier=trace: expected 200, got %d", resp.StatusCode)
		}

		// hello + session_created + tool_call + trace:fs_snapshot = 4 frames.
		frames := readSSEFrames(t, resp.Body, 4, 3*time.Second)

		seen := map[string]bool{}
		for _, f := range frames[1:] {
			if !isSSEControlFrame(f.event) {
				seen[f.event] = true
			}
		}
		if !seen["tool_call"] {
			t.Error("tier=trace: tool_call event not received")
		}
		if !seen["trace:fs_snapshot"] {
			t.Error("tier=trace: trace:fs_snapshot event not received")
		}
	}()
}

// TestSSE_InvalidTier verifies that ?tier=bogus returns 400.
func TestSSE_InvalidTier(t *testing.T) {
	d := testDaemon(t)
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "invalid-tier-test"}))

	// Use doRequest (httptest.ResponseRecorder) to check the 400 before streaming.
	rr := doRequest(t, d, "GET", fmt.Sprintf("/events/stream?sessions=%s&tier=bogus", sess.ID), nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ?tier=bogus, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[map[string]string](t, rr)
	if resp["error"] == "" {
		t.Error("expected non-empty error message for invalid tier")
	}
}

// --- Task 5.2: session_digest control frame tests ---

// TestSSE_DigestAfter50Events verifies that a session_digest control frame is
// emitted after 50 domain events have been seen in a streaming session.
// It overrides sseDigestInterval to a large value so only the count trigger fires.
func TestSSE_DigestAfter50Events(t *testing.T) {
	d := testDaemon(t)
	// Set a very long digest interval so only the 50-event count triggers a digest.
	d.sseDigestInterval = 10 * time.Hour
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "digest-count-test"}))

	// Seed 55 events so the count threshold (50) is crossed.
	for i := 0; i < 55; i++ {
		doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
			Type: fmt.Sprintf("ev_%03d", i),
			Data: `{"agent":"supervisor"}`,
		})
	}

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect with after=0 to receive backlog including session_digest.
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=0", server.URL, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read: hello + session_created + 55 domain events + at least 1 session_digest.
	// We request enough frames to get past the 50-event threshold.
	frames := readSSEFrames(t, resp.Body, 60, 4*time.Second)

	var foundDigest bool
	for _, f := range frames {
		if f.event == "session_digest" {
			foundDigest = true
			// Validate digest has no id: line (control frame invariant).
			for _, line := range f.lines {
				if strings.HasPrefix(line, "id:") {
					t.Errorf("session_digest must not carry id: line, got: %q", line)
				}
			}
			// Validate required fields present in data.
			var payload map[string]any
			if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
				t.Fatalf("parse session_digest data: %v", err)
			}
			if _, ok := payload["at"]; !ok {
				t.Error("session_digest missing 'at' field")
			}
			if _, ok := payload["agents"]; !ok {
				t.Error("session_digest missing 'agents' field")
			}
			if _, ok := payload["phase"]; !ok {
				t.Error("session_digest missing 'phase' field")
			}
			break
		}
	}
	if !foundDigest {
		t.Error("no session_digest frame received after 55 domain events")
	}
}

// TestSSE_DigestDisabled verifies that ?digest=0 suppresses session_digest frames
// even when 55 domain events are streamed.
func TestSSE_DigestDisabled(t *testing.T) {
	d := testDaemon(t)
	// Set a very long interval so only count could trigger — but we disable it.
	d.sseDigestInterval = 10 * time.Hour
	sess := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "digest-disabled-test"}))

	// Seed 55 events.
	for i := 0; i < 55; i++ {
		doRequest(t, d, "POST", "/sessions/"+sess.ID+"/events", logEventRequest{
			Type: fmt.Sprintf("ev_%03d", i),
			Data: `{"agent":"supervisor"}`,
		})
	}

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s&after=0&digest=0", server.URL, sess.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read enough frames to cover backlog. Digest must NOT appear.
	frames := readSSEFrames(t, resp.Body, 60, 4*time.Second)
	for _, f := range frames {
		if f.event == "session_digest" {
			t.Error("session_digest appeared despite ?digest=0")
		}
	}
}

// TestSSE_MultiSessionSuppressesDigestAndPerSessionHeaders verifies that a
// stream subscribing to >1 session emits only X-Belayer-Schema (no
// X-Session-Status, X-Log-Level, or X-Agent-Roster — those reference a single
// session and would misrepresent every other subscribed session) and that
// session_digest control frames are suppressed (a single digest frame cannot
// honestly describe phase/agents across N sessions, and the digest's "latest
// event id" would advance on events from any subscribed session, conflating
// scopes).
//
// Addresses codex review concern (phase 6): per-session headers and digests
// on multi-session streams misrepresent the stream's scope.
func TestSSE_MultiSessionSuppressesDigestAndPerSessionHeaders(t *testing.T) {
	d := testDaemon(t)
	d.sseDigestInterval = 10 * time.Hour

	s1 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "multi-a"}))
	s2 := decodeJSON[sessionAPIResponse](t, doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "multi-b"}))

	// Seed 55 events across both sessions so the count threshold would fire
	// on a single-session stream.
	for i := 0; i < 30; i++ {
		doRequest(t, d, "POST", "/sessions/"+s1.ID+"/events", logEventRequest{
			Type: fmt.Sprintf("a_%03d", i),
			Data: `{"agent":"supervisor"}`,
		})
	}
	for i := 0; i < 25; i++ {
		doRequest(t, d, "POST", "/sessions/"+s2.ID+"/events", logEventRequest{
			Type: fmt.Sprintf("b_%03d", i),
			Data: `{"agent":"supervisor"}`,
		})
	}

	server := httptest.NewServer(d.server.Handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/events/stream?sessions=%s,%s&after=0&digest=1", server.URL, s1.ID, s2.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Only X-Belayer-Schema is safe on a multi-session stream.
	if got := resp.Header.Get("X-Belayer-Schema"); got != "belayer-log/v1" {
		t.Errorf("X-Belayer-Schema: got %q, want %q", got, "belayer-log/v1")
	}
	for _, h := range []string{"X-Session-Status", "X-Log-Level", "X-Agent-Roster", "X-Last-Event-Id", "X-Event-Count"} {
		if got := resp.Header.Get(h); got != "" {
			t.Errorf("multi-session stream leaked %s=%q (must be omitted — refers to a single session)", h, got)
		}
	}

	frames := readSSEFrames(t, resp.Body, 70, 4*time.Second)
	for _, f := range frames {
		if f.event == "session_digest" {
			t.Error("session_digest appeared on multi-session stream (must be suppressed)")
		}
	}
}
