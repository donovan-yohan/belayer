package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithTraceAndSlice builds on testDaemonWithTrace but also registers
// the trace-slice route so E2E tests can exercise it.
func testDaemonWithTraceAndSlice(t *testing.T) (*Daemon, string) {
	t.Helper()
	d, dir := testDaemonWithTrace(t)
	// Add the trace-slice route to the existing mux.
	d.server.Handler.(*http.ServeMux).HandleFunc(
		"GET /sessions/{id}/trace/{agent}/{fragment}",
		d.handleTraceSlice,
	)
	return d, dir
}

// TestTraceE2E_TraceSessionCapturesAndServesFragments verifies the full round-trip
// via HTTP-visible metadata:
//  1. Create session, POST 5 large payloads.
//  2. GET /sessions/{id}/events — assert each event has trace_file, trace_offset,
//     trace_length, and trace_fragment populated.
//  3. Use those values to issue GET /sessions/{id}/trace/{agent}/{fragment}?offset=X&length=Y
//     and assert the body contains the scrubbed marker.
func TestTraceE2E_TraceSessionCapturesAndServesFragments(t *testing.T) {
	d, dir := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "e2e-trace",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const agentName = "supervisor"
	const numPayloads = 5

	markers := make([]string, numPayloads)
	for i := 0; i < numPayloads; i++ {
		markers[i] = fmt.Sprintf("MARKER_%d_%s", i, strings.Repeat("x", 8))
	}

	for i := 0; i < numPayloads; i++ {
		// Build a clearly ≥64 KB payload containing the unique marker.
		// Use 70 KB to be well above the 65536-byte spill threshold.
		filler := strings.Repeat("Z", 70*1024)
		payload := fmt.Sprintf(`{"agent":%q,"marker":%q,"filler":%q}`, agentName, markers[i], filler)
		rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
			Type: "tool_call",
			Data: payload,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("payload %d: expected 201, got %d: %s", i, rr.Code, rr.Body.String())
		}
	}

	// Fragment file should still exist at <dir>/traces/<sessID>/<agentName>/0001.jsonl.
	fragDir := filepath.Join(dir, "traces", sessID, agentName)
	fragPath := filepath.Join(fragDir, "0001.jsonl")
	fileData, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("read fragment file %q: %v", fragPath, err)
	}
	fragContent := string(fileData)
	for i, marker := range markers {
		if !strings.Contains(fragContent, marker) {
			t.Errorf("fragment file missing marker[%d]=%q", i, marker)
		}
	}

	// --- Step 2: GET /sessions/{id}/events via HTTP and parse JSON response. ---
	rrGet := doRequest(t, d, "GET", "/sessions/"+sessID+"/events", nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET events: expected 200, got %d: %s", rrGet.Code, rrGet.Body.String())
	}
	var httpEvents []store.SessionEvent
	if err := json.NewDecoder(rrGet.Body).Decode(&httpEvents); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(httpEvents) != numPayloads {
		t.Fatalf("expected %d events from HTTP, got %d", numPayloads, len(httpEvents))
	}

	for i, evt := range httpEvents {
		// Sentinel JSON check.
		var sentinel struct {
			Agent string `json:"agent"`
			Trace bool   `json:"_trace"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
			t.Fatalf("event[%d]: unmarshal sentinel %q: %v", i, evt.Data, err)
		}
		if sentinel.Agent != agentName {
			t.Errorf("event[%d]: sentinel agent=%q, want %q", i, sentinel.Agent, agentName)
		}
		if !sentinel.Trace {
			t.Errorf("event[%d]: sentinel _trace expected true", i)
		}

		// Trace metadata must be present in the HTTP response.
		if evt.TraceFile == "" {
			t.Errorf("event[%d]: trace_file empty in HTTP response", i)
		}
		if evt.TraceLength == 0 {
			t.Errorf("event[%d]: trace_length is 0 in HTTP response", i)
		}
		if evt.TraceFragment == "" {
			t.Errorf("event[%d]: trace_fragment empty in HTTP response", i)
		}
	}

	// --- Step 3: Use HTTP metadata to issue slice requests and verify content. ---
	for i, evt := range httpEvents {
		if evt.TraceFile == "" {
			continue // already errored above
		}
		url := fmt.Sprintf("/sessions/%s/trace/%s/%s?offset=%d&length=%d",
			sessID, agentName, evt.TraceFragment, evt.TraceOffset, evt.TraceLength)
		rr := doRequest(t, d, "GET", url, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("event[%d]: slice GET %s: expected 200, got %d: %s",
				i, url, rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, markers[i]) {
			t.Errorf("event[%d]: slice body missing marker %q (body len=%d)", i, markers[i], len(body))
		}
	}

	_ = dir
}

// TestTraceE2E_TraceSessionCapturesAndServesFragments_Sealed verifies the zst branch:
// after calling CloseAgent the fragment is sealed and the endpoint decompresses it.
func TestTraceE2E_TraceSessionCapturesAndServesFragments_Sealed(t *testing.T) {
	d, dir := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "e2e-trace-sealed",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const agentName = "implementer"
	marker := "SEALED_MARKER_" + strings.Repeat("y", 8)
	// Use 70 KB to be well above the 65536-byte spill threshold.
	filler := strings.Repeat("W", 70*1024)
	payload := fmt.Sprintf(`{"agent":%q,"marker":%q,"filler":%q}`, agentName, marker, filler)

	rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
		Type: "tool_call",
		Data: payload,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Read trace columns before sealing.
	var tf string
	var to, tl int64
	row := d.store.DB().QueryRow(
		`SELECT COALESCE(trace_file,''), COALESCE(trace_offset,0), COALESCE(trace_length,0)
		 FROM events WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
		sessID,
	)
	if err := row.Scan(&tf, &to, &tl); err != nil {
		t.Fatalf("scan trace columns: %v", err)
	}

	// Seal the fragment (triggers async zstd compression).
	if err := d.traceWriter.CloseAgent(sessID, agentName); err != nil {
		t.Fatalf("CloseAgent: %v", err)
	}

	// Sealed file should now be <fragDir>/0001.jsonl.zst
	fragDir := filepath.Join(dir, "traces", sessID, agentName)
	sealedPath := filepath.Join(fragDir, "0001.jsonl.zst")
	if _, err := os.Stat(sealedPath); err != nil {
		t.Fatalf("sealed fragment not found at %s: %v", sealedPath, err)
	}

	// Derive fragment ID for request.
	base := filepath.Base(tf)
	fragID := strings.TrimSuffix(base, ".jsonl")
	fragID = strings.TrimSuffix(fragID, ".jsonl.zst")

	url := fmt.Sprintf("/sessions/%s/trace/%s/%s?offset=%d&length=%d",
		sessID, agentName, fragID, to, tl)
	rr2 := doRequest(t, d, "GET", url, nil)
	if rr2.Code != http.StatusOK {
		t.Fatalf("slice GET %s: expected 200, got %d: %s", url, rr2.Code, rr2.Body.String())
	}
	body := rr2.Body.String()
	if !strings.Contains(body, marker) {
		t.Errorf("sealed slice body missing marker %q (body len=%d)", marker, len(body))
	}

	_ = dir
}

// TestTraceE2E_StandardTierNoFragments verifies that standard-tier sessions do
// not produce fragment files, every event has null trace_* columns in the DB,
// and the HTTP response returns no trace metadata. Data fields are structured
// JSON sentinel objects (not raw truncated strings).
func TestTraceE2E_StandardTierNoFragments(t *testing.T) {
	d, dir := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "e2e-standard",
		LogLevel: LogLevelStandard,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const agentName = "implementer"
	const numPayloads = 5

	for i := 0; i < numPayloads; i++ {
		// Payload must be ≥ 65536 bytes to trigger truncation path.
		filler := strings.Repeat("S", 70*1024)
		payload := fmt.Sprintf(`{"agent":%q,"idx":%d,"filler":%q}`, agentName, i, filler)
		rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/events", logEventRequest{
			Type: "tool_call",
			Data: payload,
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("payload %d: expected 201, got %d: %s", i, rr.Code, rr.Body.String())
		}
	}

	// No trace directory should exist for this session.
	tracePath := filepath.Join(dir, "traces", sessID)
	if _, err := os.Stat(tracePath); err == nil {
		t.Errorf("expected no trace dir for standard-tier session, but %s exists", tracePath)
	}

	// GET events via HTTP and verify: no trace metadata, structured JSON sentinel as data.
	rrGet := doRequest(t, d, "GET", "/sessions/"+sessID+"/events", nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET events: expected 200, got %d: %s", rrGet.Code, rrGet.Body.String())
	}
	var httpEvents []store.SessionEvent
	if err := json.NewDecoder(rrGet.Body).Decode(&httpEvents); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(httpEvents) != numPayloads {
		t.Fatalf("expected %d events from HTTP, got %d", numPayloads, len(httpEvents))
	}

	for i, evt := range httpEvents {
		// Data must be a valid structured JSON sentinel.
		var sentinel struct {
			Truncated bool   `json:"_truncated"`
			Tier      string `json:"tier"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &sentinel); err != nil {
			t.Fatalf("event[%d]: data is not valid JSON: %v\ndata: %s", i, err, evt.Data)
		}
		if !sentinel.Truncated {
			t.Errorf("event[%d]: sentinel._truncated: expected true", i)
		}
		if sentinel.Tier != "standard" {
			t.Errorf("event[%d]: sentinel.tier: got %q, want %q", i, sentinel.Tier, "standard")
		}
		if sentinel.Reason == "" {
			t.Errorf("event[%d]: sentinel.reason: expected non-empty", i)
		}

		// No trace metadata should appear in the HTTP response.
		if evt.TraceFile != "" {
			t.Errorf("event[%d]: trace_file should be absent for standard tier, got %q", i, evt.TraceFile)
		}
		if evt.TraceFragment != "" {
			t.Errorf("event[%d]: trace_fragment should be absent for standard tier, got %q", i, evt.TraceFragment)
		}

		// Verify raw DB columns are still NULL.
		var traceFileNull, traceOffsetNull, traceLengthNull bool
		row := d.store.DB().QueryRow(
			`SELECT trace_file IS NULL, trace_offset IS NULL, trace_length IS NULL
			 FROM events WHERE id = ?`,
			evt.ID,
		)
		if err := row.Scan(&traceFileNull, &traceOffsetNull, &traceLengthNull); err != nil {
			t.Fatalf("event[%d]: scan trace columns: %v", i, err)
		}
		if !traceFileNull {
			t.Errorf("event[%d]: trace_file expected NULL for standard tier", i)
		}
		if !traceOffsetNull {
			t.Errorf("event[%d]: trace_offset expected NULL for standard tier", i)
		}
		if !traceLengthNull {
			t.Errorf("event[%d]: trace_length expected NULL for standard tier", i)
		}
	}

	_ = dir
}

// TestTraceE2E_SliceEndpoint_404OnNonTrace verifies that the slice endpoint
// returns 404 for a session that is not at trace tier.
func TestTraceE2E_SliceEndpoint_404OnNonTrace(t *testing.T) {
	d, _ := testDaemonWithTraceAndSlice(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "e2e-standard-slice",
		LogLevel: LogLevelStandard,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	url := fmt.Sprintf("/sessions/%s/trace/whatever/0001?offset=0&length=1", sessID)
	rr := doRequest(t, d, "GET", url, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error field in response, got %v", resp)
	}
}

