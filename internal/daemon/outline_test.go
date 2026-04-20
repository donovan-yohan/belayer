package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithOutline returns a testDaemon that also has handleOutline registered.
func testDaemonWithOutline(t *testing.T) *Daemon {
	t.Helper()
	d := testDaemon(t)
	d.server.Handler.(*http.ServeMux).HandleFunc("GET /sessions/{id}/outline", d.handleOutline)
	return d
}

func TestOutline_OK(t *testing.T) {
	d := testDaemonWithOutline(t)

	// Seed: 1 session with log_level=standard.
	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{
		Name:     "outline-test",
		LogLevel: "standard",
	})
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create session: got %d, body=%s", createRR.Code, createRR.Body.String())
	}
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	// Seed: 2 agent runs via POST /sessions/{id}/agents.
	spawnRR1 := doRequest(t, d, "POST", "/sessions/"+id+"/agents", agentSpawnRequest{
		Name:    "supervisor",
		Role:    "supervisor",
		Profile: "default",
	})
	if spawnRR1.Code != http.StatusCreated {
		t.Fatalf("spawn supervisor: got %d, body=%s", spawnRR1.Code, spawnRR1.Body.String())
	}
	spawnRR2 := doRequest(t, d, "POST", "/sessions/"+id+"/agents", agentSpawnRequest{
		Name:    "backend-dev",
		Role:    "implementer",
		Profile: "default",
	})
	if spawnRR2.Code != http.StatusCreated {
		t.Fatalf("spawn backend-dev: got %d, body=%s", spawnRR2.Code, spawnRR2.Body.String())
	}

	// Seed: 1 artifact.
	artifactRR := doRequest(t, d, "POST", "/sessions/"+id+"/artifacts", artifactCreateRequest{
		Kind:     "spec",
		Path:     "docs/spec.md",
		Producer: "supervisor",
		Summary:  "main spec",
	})
	if artifactRR.Code != http.StatusCreated {
		t.Fatalf("create artifact: got %d, body=%s", artifactRR.Code, artifactRR.Body.String())
	}

	// Seed: 3 events: one bridge:tool_started, one agent_status:planning, one agent_status:implementing.
	evtRR1 := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
		Type: "bridge:tool_started",
		Data: `{"agent":"supervisor","tool":"read_file"}`,
	})
	if evtRR1.Code != http.StatusCreated {
		t.Fatalf("log bridge:tool_started: got %d, body=%s", evtRR1.Code, evtRR1.Body.String())
	}

	evtRR2 := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
		Type: "agent_status:planning",
		Data: `{"agent":"supervisor","status":"planning"}`,
	})
	if evtRR2.Code != http.StatusCreated {
		t.Fatalf("log agent_status:planning: got %d, body=%s", evtRR2.Code, evtRR2.Body.String())
	}

	evtRR3 := doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
		Type: "agent_status:implementing",
		Data: `{"agent":"supervisor","status":"implementing"}`,
	})
	if evtRR3.Code != http.StatusCreated {
		t.Fatalf("log agent_status:implementing: got %d, body=%s", evtRR3.Code, evtRR3.Body.String())
	}

	// Call GET /sessions/{id}/outline.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/outline", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Fatal("Content-Type header missing")
	}
	// Should start with application/json
	if len(ct) < len("application/json") || ct[:len("application/json")] != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	// Decode response.
	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Assert top-level keys present.
	for _, key := range []string{"session", "agents", "artifacts", "phases", "final_status"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("missing top-level key %q in response", key)
		}
	}

	// Assert agents array has 2 entries.
	var agents []map[string]json.RawMessage
	if err := json.Unmarshal(resp["agents"], &agents); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}

	// Assert phases array has both plan and implement entries in order.
	var phases []map[string]json.RawMessage
	if err := json.Unmarshal(resp["phases"], &phases); err != nil {
		t.Fatalf("decode phases: %v", err)
	}

	if len(phases) < 2 {
		t.Fatalf("expected at least 2 phases, got %d: %s", len(phases), string(resp["phases"]))
	}

	// Extract phase names in order.
	phaseNames := make([]string, len(phases))
	for i, p := range phases {
		var name string
		if err := json.Unmarshal(p["phase"], &name); err != nil {
			t.Fatalf("decode phase[%d].phase: %v", i, err)
		}
		phaseNames[i] = name
	}

	// Find plan and implement in order.
	planIdx := -1
	implIdx := -1
	for i, name := range phaseNames {
		if name == "plan" && planIdx == -1 {
			planIdx = i
		}
		if name == "implement" && implIdx == -1 {
			implIdx = i
		}
	}
	if planIdx == -1 {
		t.Error("phases array missing 'plan' entry")
	}
	if implIdx == -1 {
		t.Error("phases array missing 'implement' entry")
	}
	if planIdx != -1 && implIdx != -1 && planIdx >= implIdx {
		t.Errorf("expected 'plan' before 'implement', got indices plan=%d implement=%d", planIdx, implIdx)
	}

	// Assert session block has expected fields.
	var sessBlock map[string]json.RawMessage
	if err := json.Unmarshal(resp["session"], &sessBlock); err != nil {
		t.Fatalf("decode session block: %v", err)
	}
	for _, key := range []string{"id", "status", "log_level", "created_at"} {
		if _, ok := sessBlock[key]; !ok {
			t.Errorf("session block missing key %q", key)
		}
	}

	// Assert artifacts has 1 entry.
	var artifacts []store.Artifact
	if err := json.Unmarshal(resp["artifacts"], &artifacts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(artifacts))
	}

	// Assert final_status is the session status string (not nested object).
	var finalStatus string
	if err := json.Unmarshal(resp["final_status"], &finalStatus); err != nil {
		t.Fatalf("decode final_status: %v", err)
	}
	if finalStatus == "" {
		t.Error("final_status must not be empty")
	}
}

func TestOutline_ToolCallCounted(t *testing.T) {
	d := testDaemonWithOutline(t)

	createRR := doRequest(t, d, "POST", "/sessions", createSessionRequest{Name: "outline-toolcount"})
	sess := decodeJSON[sessionAPIResponse](t, createRR)
	id := sess.ID

	doRequest(t, d, "POST", "/sessions/"+id+"/agents", agentSpawnRequest{Name: "supervisor", Role: "supervisor", Profile: "default"})

	// Log 3 bridge:tool_started events for supervisor.
	for i := 0; i < 3; i++ {
		doRequest(t, d, "POST", "/sessions/"+id+"/events", logEventRequest{
			Type: "bridge:tool_started",
			Data: `{"agent":"supervisor","tool":"bash"}`,
		})
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/outline", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var agents []map[string]json.RawMessage
	if err := json.Unmarshal(resp["agents"], &agents); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("expected at least 1 agent")
	}

	// Find supervisor agent.
	var toolCalls float64
	for _, a := range agents {
		var name string
		json.Unmarshal(a["name"], &name)
		if name == "supervisor" {
			json.Unmarshal(a["tool_calls"], &toolCalls)
		}
	}
	if toolCalls != 3 {
		t.Errorf("expected tool_calls=3, got %v", toolCalls)
	}
}

func TestOutline_NotFound(t *testing.T) {
	d := testDaemonWithOutline(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/does-not-exist/outline", nil)
	d.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
