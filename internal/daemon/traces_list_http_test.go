package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// testDaemonWithTracesList returns a daemon with the traces-list route
// registered. It uses the testDaemonWithTrace helper so traceBase is set.
func testDaemonWithTracesList(t *testing.T) (*Daemon, string) {
	t.Helper()
	d, dir := testDaemonWithTrace(t)
	d.server.Handler.(*http.ServeMux).HandleFunc(
		"GET /sessions/{id}/traces",
		d.handleListTraces,
	)
	return d, dir
}

// TestListTraces_404IfNotTraceTier verifies that a verbose-tier session returns
// 404 from GET /sessions/{id}/traces.
func TestListTraces_404IfNotTraceTier(t *testing.T) {
	d, _ := testDaemonWithTracesList(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "verbose-no-traces",
		LogLevel: LogLevelVerbose,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/traces", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for verbose tier, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestListTraces_404IfStandardTier verifies that a standard-tier session also
// returns 404.
func TestListTraces_404IfStandardTier(t *testing.T) {
	d, _ := testDaemonWithTracesList(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "standard-no-traces",
		LogLevel: LogLevelStandard,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/traces", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for standard tier, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestListTraces_OK seeds two fragments (one plain .jsonl, one .jsonl.zst) and
// verifies the listing returns both with correct compressed flags.
func TestListTraces_OK(t *testing.T) {
	d, dir := testDaemonWithTracesList(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "trace-list-ok",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Seed fragments under <traceBase>/<sessID>/<agent>/.
	agentDir := filepath.Join(dir, "traces", sessID, "backend-dev")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Plain fragment.
	if err := os.WriteFile(filepath.Join(agentDir, "0001.jsonl"), []byte(`{"n":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile plain: %v", err)
	}
	// Compressed fragment.
	if err := os.WriteFile(filepath.Join(agentDir, "0002.jsonl.zst"), []byte("compressed-bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile zst: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/traces", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	entries := decodeJSON[[]traceFragmentEntry](t, rr)
	if len(entries) != 2 {
		t.Fatalf("expected 2 trace entries, got %d: %#v", len(entries), entries)
	}

	// Index by fragment name for easier assertions.
	byFrag := map[string]traceFragmentEntry{}
	for _, e := range entries {
		byFrag[e.Fragment] = e
		if e.Agent != "backend-dev" {
			t.Errorf("unexpected agent %q", e.Agent)
		}
	}

	plain, ok := byFrag["0001"]
	if !ok {
		t.Fatal("missing fragment 0001")
	}
	if plain.Compressed {
		t.Error("0001.jsonl should not be compressed")
	}
	if plain.Size == 0 {
		t.Error("0001 fragment size should be non-zero")
	}

	zst, ok := byFrag["0002"]
	if !ok {
		t.Fatal("missing fragment 0002")
	}
	if !zst.Compressed {
		t.Error("0002.jsonl.zst should be marked compressed")
	}
}

// TestListTraces_EmptyDir verifies that a trace-tier session with no fragments
// returns 200 + empty array.
func TestListTraces_EmptyDir(t *testing.T) {
	d, _ := testDaemonWithTracesList(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "trace-empty",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/traces", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	entries := decodeJSON[[]traceFragmentEntry](t, rr)
	if len(entries) != 0 {
		t.Fatalf("expected empty array, got %d entries", len(entries))
	}
}

// TestListTraces_MultipleAgents verifies that fragments from multiple agents
// are all returned.
func TestListTraces_MultipleAgents(t *testing.T) {
	d, dir := testDaemonWithTracesList(t)

	sessID, err := d.store.CreateSession(store.Session{
		Name:     "trace-multi-agents",
		LogLevel: LogLevelTrace,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	agents := []string{"supervisor", "backend-dev", "reviewer"}
	for _, agent := range agents {
		agentDir := filepath.Join(dir, "traces", sessID, agent)
		if err := os.MkdirAll(agentDir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", agent, err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "0001.jsonl"), []byte(`{"a":1}`+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", agent, err)
		}
	}

	rr := doRequest(t, d, "GET", "/sessions/"+sessID+"/traces", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	entries := decodeJSON[[]traceFragmentEntry](t, rr)
	if len(entries) != len(agents) {
		t.Fatalf("expected %d entries (one per agent), got %d: %#v", len(agents), len(entries), entries)
	}
}
