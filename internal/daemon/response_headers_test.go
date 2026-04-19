package daemon

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// TestResponseHeaders_OnGetEvents verifies that all 6 standard Belayer response
// headers are present and have expected values after seeding a session with events.
func TestResponseHeaders_OnGetEvents(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "hdr-test",
		LogLevel: "standard",
	})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, body=%s", createRR.Code, createRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Spawn an agent so X-Agent-Roster is non-empty.
	spawnRR := doRequest(t, d, "POST", "/sessions/"+id+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "default",
	})
	if spawnRR.Code != http.StatusCreated {
		t.Fatalf("spawn agent: got %d, body=%s", spawnRR.Code, spawnRR.Body.String())
	}

	// Post 3 events.
	for i := 0; i < 3; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "test:event",
			Data: `{"agent":"supervisor","msg":"hello"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("log event %d: got %d, body=%s", i, rr.Code, rr.Body.String())
		}
	}

	// GET /events.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Assert all 6 headers present.
	headers := rr.Header()

	// X-Belayer-Schema
	schema := headers.Get("X-Belayer-Schema")
	if schema != "belayer-log/v1" {
		t.Errorf("X-Belayer-Schema: expected 'belayer-log/v1', got %q", schema)
	}

	// X-Last-Event-Id: must be a non-negative integer.
	lastEventID := headers.Get("X-Last-Event-Id")
	if lastEventID == "" {
		t.Error("X-Last-Event-Id header missing")
	} else {
		v, err := strconv.ParseInt(lastEventID, 10, 64)
		if err != nil || v < 0 {
			t.Errorf("X-Last-Event-Id: expected non-negative integer, got %q", lastEventID)
		}
	}

	// X-Event-Count: must be 5 — 1 session_created + 1 agent_spawned + 3 test events.
	eventCount := headers.Get("X-Event-Count")
	if eventCount != "5" {
		t.Errorf("X-Event-Count: expected '5' (1 session_created + 1 agent_spawned + 3 test events), got %q", eventCount)
	}

	// X-Session-Status: must be non-empty.
	sessionStatus := headers.Get("X-Session-Status")
	if sessionStatus == "" {
		t.Error("X-Session-Status header missing or empty")
	}

	// X-Log-Level: must be 'standard'.
	logLevel := headers.Get("X-Log-Level")
	if logLevel != "standard" {
		t.Errorf("X-Log-Level: expected 'standard', got %q", logLevel)
	}

	// X-Agent-Roster: must contain 'supervisor'.
	roster := headers.Get("X-Agent-Roster")
	if !strings.Contains(roster, "supervisor") {
		t.Errorf("X-Agent-Roster: expected 'supervisor' in %q", roster)
	}
}

// TestResponseHeaders_UnknownSession verifies behavior on an unknown session.
// handleGetEvents returns 200 with empty events array. Schema header must still be set;
// session-specific headers (X-Session-Status, X-Log-Level, X-Agent-Roster) may be absent.
func TestResponseHeaders_UnknownSession(t *testing.T) {
	d := testDaemon(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/does-not-exist/events", nil)
	d.server.Handler.ServeHTTP(rr, req)

	// Current handleGetEvents returns 200 with empty events for unknown sessions
	// (QueryEventsWindow returns empty, not 404).
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	headers := rr.Header()

	// X-Belayer-Schema must always be present.
	if headers.Get("X-Belayer-Schema") != "belayer-log/v1" {
		t.Errorf("X-Belayer-Schema: expected 'belayer-log/v1', got %q", headers.Get("X-Belayer-Schema"))
	}

	// X-Event-Count must be 0 (no events).
	if headers.Get("X-Event-Count") != "0" {
		t.Errorf("X-Event-Count: expected '0', got %q", headers.Get("X-Event-Count"))
	}

	// X-Session-Status and X-Log-Level may be absent for unknown session — no assertion.
}

// TestCompactTSV_GetEvents verifies ?format=compact produces valid TSV output.
func TestCompactTSV_GetEvents(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "tsv-test",
		LogLevel: "standard",
	})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, body=%s", createRR.Code, createRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Seed 2 explicit events — one with newline in data to test escaping.
	doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
		Type: "test:plain",
		Data: `{"agent":"tester","msg":"hello"}`,
	})
	doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
		Type: "test:newline",
		Data: "{\"agent\":\"tester\",\"msg\":\"line1\nline2\ttabbed\"}",
	})

	// GET /events?format=compact
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events?format=compact", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Content-Type must be TSV.
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/tab-separated-values") {
		t.Errorf("Content-Type: expected TSV, got %q", ct)
	}

	// Parse lines.
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}

	// Expect at least 3 lines: 1 session_created + 2 test events.
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 TSV lines, got %d:\n%s", len(lines), rr.Body.String())
	}

	// Verify structure: each line must have exactly 4 tab-separated fields.
	for i, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Errorf("line %d: expected 4 tab-separated fields, got %d: %q", i, len(fields), line)
		}
		// First field must be a positive integer (event ID).
		if _, err := strconv.ParseInt(fields[0], 10, 64); err != nil {
			t.Errorf("line %d: field[0] not integer: %q", i, fields[0])
		}
	}

	// Verify escape behavior: the test:newline event should have escaped newlines in summary.
	found := false
	for _, line := range lines {
		if strings.Contains(line, "test:newline") {
			found = true
			// Summary must not contain a literal newline or tab.
			if strings.Count(line, "\n") > 0 {
				t.Errorf("test:newline summary contains literal newline: %q", line)
			}
			// It should contain escaped versions.
			if !strings.Contains(line, `\n`) && !strings.Contains(line, `\t`) {
				t.Errorf("test:newline summary missing escape sequences: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("test:newline event not found in TSV output:\n%s", rr.Body.String())
	}
}

// TestSince_RepeatCallReturnsOnlyNew verifies per-reader cursor behaviour.
func TestSince_RepeatCallReturnsOnlyNew(t *testing.T) {
	d := testDaemon(t)

	// Create a session.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "since-test"})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d", createRR.Code)
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Seed 3 events.
	for i := 0; i < 3; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "ev:seed",
			Data: `{"agent":"tester"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed event %d: %d", i, rr.Code)
		}
	}

	// First call with ?since=r1 — should return all events (session_created + 3 seeds).
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events?since=r1", nil)
	d.server.Handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first since call: %d %s", rr1.Code, rr1.Body.String())
	}
	first := decodeJSON[[]map[string]interface{}](t, rr1)
	if len(first) < 4 {
		// session_created + 3 seed events = 4 minimum
		t.Fatalf("first call: expected >= 4 events, got %d", len(first))
	}

	// Seed 2 more events after the first read.
	for i := 0; i < 2; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "ev:new",
			Data: `{"agent":"tester"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("new event %d: %d", i, rr.Code)
		}
	}

	// Second call with ?since=r1 — should return only the 2 new events.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events?since=r1", nil)
	d.server.Handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second since call: %d %s", rr2.Code, rr2.Body.String())
	}
	second := decodeJSON[[]map[string]interface{}](t, rr2)
	if len(second) != 2 {
		t.Errorf("second call: expected 2 new events, got %d", len(second))
	}
}

// TestSince_RejectsInvalidReaderID verifies that malformed reader IDs return 400.
func TestSince_RejectsInvalidReaderID(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "since-invalid"})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d", createRR.Code)
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	invalidIDs := []string{"../foo", "foo bar", strings.Repeat("a", 65), "foo/bar"}
	for _, rid := range invalidIDs {
		rr := httptest.NewRecorder()
		// URL-encode the since parameter so httptest.NewRequest doesn't panic on
		// illegal characters (e.g. spaces, slashes) while still sending the raw
		// value to the handler via r.URL.Query().Get("since").
		rawURL := "/sessions/" + id + "/events?since=" + url.QueryEscape(rid)
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		d.server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("since=%q: expected 400, got %d (body=%s)", rid, rr.Code, rr.Body.String())
		}
	}
}

// TestLinkNextHeader_FullPage verifies Link rel=next is emitted when page is full.
func TestLinkNextHeader_FullPage(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "pagination-test"})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d", createRR.Code)
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Seed 5 explicit events (plus 1 session_created = 6 total, but we test relative).
	for i := 0; i < 5; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "ev:page",
			Data: `{"agent":"tester"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed event %d: %d", i, rr.Code)
		}
	}

	// GET /events?limit=3 — page is full, should have Link header.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events?limit=3", nil)
	d.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	linkHdr := rr.Header().Get("Link")
	if linkHdr == "" {
		t.Fatal("Link header missing on full page")
	}
	if !strings.Contains(linkHdr, `rel="next"`) {
		t.Errorf("Link header missing rel=next: %q", linkHdr)
	}
	// The next URL should have after= param and limit=3.
	if !strings.Contains(linkHdr, "after=") {
		t.Errorf("Link header missing after= param: %q", linkHdr)
	}
	if !strings.Contains(linkHdr, "limit=3") {
		t.Errorf("Link header missing limit=3: %q", linkHdr)
	}

	// Verify the events in the body are 3.
	events := decodeJSON[[]map[string]interface{}](t, rr)
	if len(events) != 3 {
		t.Errorf("expected 3 events in body, got %d", len(events))
	}

	// Extract after= value from Link header and verify it equals ID of 3rd event.
	// Link: </sessions/<id>/events?after=<lastID>&limit=3>; rel="next"
	thirdEventID := events[2]["id"]
	thirdIDStr := ""
	if f, ok := thirdEventID.(float64); ok {
		thirdIDStr = strconv.FormatInt(int64(f), 10)
	}
	if thirdIDStr != "" && !strings.Contains(linkHdr, "after="+thirdIDStr) {
		t.Errorf("Link after= value expected %s, not found in: %q", thirdIDStr, linkHdr)
	}
}

// TestLinkNextHeader_PartialPage verifies Link rel=next is absent when page is partial.
func TestLinkNextHeader_PartialPage(t *testing.T) {
	d := testDaemon(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "pagination-partial"})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: %d", createRR.Code)
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Seed only 2 events.
	for i := 0; i < 2; i++ {
		rr := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "ev:partial",
			Data: `{"agent":"tester"}`,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed event %d: %d", i, rr.Code)
		}
	}

	// GET /events?limit=10 — fewer events than limit, no Link header.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/events?limit=10", nil)
	d.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	linkHdr := rr.Header().Get("Link")
	if linkHdr != "" {
		t.Errorf("Link header should be absent on partial page, got %q", linkHdr)
	}
}
