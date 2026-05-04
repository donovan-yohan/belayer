package daemon

// Phase 5.D integration tests — end-to-end coverage of parallel mains sharing
// one profile directory under the Phase 5.A stable naming scheme (no per-run
// instance hash).
//
// Scenario 1 — TestPhase5Integration_ParallelMainsShareProfile:
//   Spawn backend-dev-1, backend-dev-2 → same profile → write sentinel →
//   spawn backend-dev-3 → sentinel visible → session teardown with crag scope →
//   profile dir survives.
//
// Scenario 2 — TestPhase5Integration_ParallelMainsConcurrentStateDB:
//   Two goroutines open the same profile/state.db (WAL mode) and each do N
//   inserts. Validates WAL concurrency without deadlock.
//
// Helpers reused from:
//   - setupBaseBelayerProfile  (agents_profile_spawn_test.go)
//   - setupForkProfile          (agents_profile_teardown_test.go)
//   - addAgentRunToSession      (agents_profile_session_teardown_test.go)
//   - markSessionTerminal       (agents_profile_session_teardown_test.go)
//   - mustWrite                 (agents_identity_test.go)
//   - setupIntegrationBase      (agents_profile_integration_test.go)
//   - createSession             (agents_profile_integration_test.go)

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/donovan-yohan/belayer/internal/bridge"
	_ "modernc.org/sqlite"
)

// ── Scenario 1: Parallel mains share one profile + sentinel persistence ───────

// TestPhase5Integration_ParallelMainsShareProfile verifies that:
//  1. Two parallel mains (backend-dev-1, backend-dev-2) with Identity=backend-dev
//     both resolve to the same profile name "belayer-local-backend-dev" (no hash).
//  2. Both agent_runs rows record that same profile name.
//  3. The profile dir exists exactly once on disk with valid symlinks.
//  4. A sentinel written to <profile>/memories/MEMORY.md (simulating one agent's
//     write) is visible to a third backend-dev-3 that is spawned afterward.
//  5. After session teardown, with a crag-scoped talent.yaml present, the profile
//     dir survives (crag-scoped profiles are preserved on session end).
func TestPhase5Integration_ParallelMainsShareProfile(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	workspace := t.TempDir()

	// Write talent.yaml with memory.scope=crag so the profile is preserved on
	// session teardown (see step 5).
	mustWrite(
		t,
		filepath.Join(workspace, ".belayer", "agents", "backend-dev", "talent.yaml"),
		"schema_version: \"belayer-talent/v1\"\nmemory:\n  scope: crag\n",
	)
	mustWrite(
		t,
		filepath.Join(workspace, ".belayer", "agents", "backend-dev", "system-prompt.md"),
		"you are backend-dev",
	)
	mustWrite(
		t,
		filepath.Join(workspace, ".belayer", "agents", "backend-dev", "agent.yaml"),
		"kind: main\n",
	)

	d := testDaemon(t)

	// supervisor is required by checkSessionStalled logic.
	sessID := createSession(t, d, "phase5-parallel-mains-test", workspace)
	addAgentRunToSession(t, d, sessID, "supervisor", "default")

	// Capture profiles from bridge spawn calls.
	var mu sync.Mutex
	profiles := make(map[string]string) // agent name → fork profile
	d.spawnBridgeAgent = func(req agentSpawnRequest) (*bridge.Process, error) {
		mu.Lock()
		profiles[req.Name] = req.Profile
		mu.Unlock()
		return nil, nil
	}

	// Step 1 & 2: Spawn backend-dev-1 and backend-dev-2 with Identity=backend-dev.
	for _, name := range []string{"backend-dev-1", "backend-dev-2"} {
		rr := doRequest(t, d, "POST", "/sessions/"+sessID+"/agents", agentSpawnRequest{
			Name:     name,
			Identity: "backend-dev",
			Role:     "backend-dev",
			Kind:     "main",
			Profile:  belayerBaseProfileName,
			Workdir:  workspace,
		})
		if rr.Code != 201 {
			t.Fatalf("spawn %q: %d %s", name, rr.Code, rr.Body.String())
		}
	}

	mu.Lock()
	p1 := profiles["backend-dev-1"]
	p2 := profiles["backend-dev-2"]
	mu.Unlock()

	// Both must resolve to "belayer-local-backend-dev" (Phase 5.A stable naming).
	const wantProfile = "belayer-local-backend-dev"
	if p1 != wantProfile {
		t.Errorf("backend-dev-1 profile = %q, want %q", p1, wantProfile)
	}
	if p2 != wantProfile {
		t.Errorf("backend-dev-2 profile = %q, want %q", p2, wantProfile)
	}
	if p1 != p2 {
		t.Errorf("parallel mains must share one fork profile; got backend-dev-1=%q backend-dev-2=%q", p1, p2)
	}

	// Step 3: Both agent_runs rows must record the shared profile.
	for _, name := range []string{"backend-dev-1", "backend-dev-2"} {
		stored, err := d.store.GetAgentRun(sessID, name)
		if err != nil {
			t.Fatalf("GetAgentRun %q: %v", name, err)
		}
		if stored.Profile != wantProfile {
			t.Errorf("agent_runs.profile for %q = %q, want %q", name, stored.Profile, wantProfile)
		}
	}

	// Step 4: Profile dir must exist exactly once on disk.
	forkDir := filepath.Join(profilesRoot, wantProfile)
	if _, err := os.Stat(forkDir); err != nil {
		t.Fatalf("fork profile dir %s not created: %v", forkDir, err)
	}

	// Verify symlinks: auth.json, plugins/belayer, skills.
	authLink := filepath.Join(forkDir, "auth.json")
	if _, err := os.Readlink(authLink); err != nil {
		t.Errorf("fork %s: auth.json symlink missing or broken: %v", wantProfile, err)
	}
	pluginLink := filepath.Join(forkDir, "plugins", "belayer")
	if _, err := os.Readlink(pluginLink); err != nil {
		t.Errorf("fork %s: plugins/belayer symlink missing or broken: %v", wantProfile, err)
	}
	skillsLink := filepath.Join(forkDir, "skills")
	if _, err := os.Readlink(skillsLink); err != nil {
		t.Errorf("fork %s: skills symlink missing or broken: %v", wantProfile, err)
	}

	// No second copy of the profile dir should exist.
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		t.Fatalf("read profilesRoot: %v", err)
	}
	var forkCount int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "belayer-local-backend-dev") {
			forkCount++
		}
	}
	if forkCount != 1 {
		t.Errorf("expected exactly 1 fork dir for backend-dev, found %d", forkCount)
	}

	// Step 5: Write sentinel to <profile>/memories/MEMORY.md (simulating one agent's write).
	memoriesDir := filepath.Join(forkDir, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir memories: %v", err)
	}
	const sentinelContent = "sentinel: backend-dev shared memory pool"
	sentinelPath := filepath.Join(memoriesDir, "MEMORY.md")
	if err := os.WriteFile(sentinelPath, []byte(sentinelContent), 0o644); err != nil {
		t.Fatalf("write sentinel MEMORY.md: %v", err)
	}

	// Step 6: Spawn backend-dev-3 — must resolve to the same profile.
	// The sentinel written by step 5 must be readable afterward.
	rr3 := doRequest(t, d, "POST", "/sessions/"+sessID+"/agents", agentSpawnRequest{
		Name:     "backend-dev-3",
		Identity: "backend-dev",
		Role:     "backend-dev",
		Kind:     "main",
		Profile:  belayerBaseProfileName,
		Workdir:  workspace,
	})
	if rr3.Code != 201 {
		t.Fatalf("spawn backend-dev-3: %d %s", rr3.Code, rr3.Body.String())
	}

	mu.Lock()
	p3 := profiles["backend-dev-3"]
	mu.Unlock()

	if p3 != wantProfile {
		t.Errorf("backend-dev-3 profile = %q, want %q (third parallel spawn must reuse same profile)", p3, wantProfile)
	}

	// Sentinel must still be readable (third spawn must not clobber memories/).
	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("sentinel MEMORY.md not readable after third spawn: %v", err)
	}
	if string(got) != sentinelContent {
		t.Errorf("sentinel content changed after third spawn: got %q, want %q", string(got), sentinelContent)
	}

	// Step 7: Tear down session (terminal status). With memory.scope=crag the profile
	// dir must survive the 3.C session-end sweep.
	markSessionTerminal(t, d, sessID, "complete")

	if _, err := os.Stat(forkDir); err != nil {
		t.Errorf("crag-scoped profile dir %s should survive session teardown, but was removed: %v", forkDir, err)
	}

	// Sentinel must also survive session teardown.
	got2, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("sentinel MEMORY.md gone after session teardown (profile should be preserved for crag scope): %v", err)
	}
	if string(got2) != sentinelContent {
		t.Errorf("sentinel content changed after session teardown: got %q, want %q", string(got2), sentinelContent)
	}
}

// ── Scenario 2: Concurrent SQLite writes to shared state.db ─────────────────

// TestPhase5Integration_ParallelMainsConcurrentStateDB validates that two
// goroutines can open the same SQLite database (WAL mode) simultaneously and
// perform concurrent inserts without deadlock or data corruption.
//
// This simulates the "two parallel mains sharing one profile" scenario at the
// Hermes state.db level: both agents open <profile>/state.db and write rows.
// The test uses raw database/sql + modernc.org/sqlite (the same driver used by
// the belayer store) to exercise WAL + concurrent access.
//
// NOTE: Hermes itself adds BEGIN IMMEDIATE + jitter retry on the Python side
// (hermes_state.py:167-201). This Go test validates the underlying SQLite WAL
// behaviour that makes those retries safe. We cannot import Hermes's Python
// SessionDB directly, so we exercise the SQLite layer directly.
func TestPhase5Integration_ParallelMainsConcurrentStateDB(t *testing.T) {
	profilesRoot, _ := setupIntegrationBase(t)

	// Create the shared profile dir that both "agents" will use.
	forkDir := filepath.Join(profilesRoot, "belayer-local-backend-dev")
	if err := os.MkdirAll(forkDir, 0o755); err != nil {
		t.Fatalf("mkdir fork profile: %v", err)
	}

	dbPath := filepath.Join(forkDir, "state.db")
	// Include busy_timeout to simulate the retry semantics that Hermes applies on
	// the Python side via BEGIN IMMEDIATE + jitter retry (hermes_state.py:167-201).
	// Without busy_timeout, concurrent SQLite writers from the same process receive
	// "database is locked" immediately (no retry) — that is a test-design issue,
	// not a production bug. In production, Hermes Python handles the retry loop;
	// here we delegate to SQLite's built-in busy handler (5 s timeout).
	dbDSN := fmt.Sprintf("file:%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath)

	// Bootstrap the schema using the first connection.
	setupDB, err := sql.Open("sqlite", dbDSN)
	if err != nil {
		t.Fatalf("open setup db: %v", err)
	}
	_, err = setupDB.Exec(`CREATE TABLE IF NOT EXISTS entries (id INTEGER PRIMARY KEY AUTOINCREMENT, agent TEXT NOT NULL, payload TEXT NOT NULL)`)
	if err != nil {
		setupDB.Close()
		t.Fatalf("create schema: %v", err)
	}
	setupDB.Close()

	const insertsPerAgent = 50

	// Open two separate connections (simulating two agent processes).
	db1, err := sql.Open("sqlite", dbDSN)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer db1.Close()

	db2, err := sql.Open("sqlite", dbDSN)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)

	insertN := func(db *sql.DB, agentName string, n int, errOut *error) {
		defer wg.Done()
		for i := 0; i < n; i++ {
			payload := fmt.Sprintf("message-%d", i)
			_, execErr := db.Exec(`INSERT INTO entries (agent, payload) VALUES (?, ?)`, agentName, payload)
			if execErr != nil {
				*errOut = fmt.Errorf("agent %s insert %d: %w", agentName, i, execErr)
				return
			}
		}
	}

	wg.Add(2)
	go insertN(db1, "backend-dev-1", insertsPerAgent, &errs[0])
	go insertN(db2, "backend-dev-2", insertsPerAgent, &errs[1])
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d failed: %v", i, e)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// Verify final row count == 2N (no lost inserts, no phantom rows).
	var count int
	if err := db1.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	wantCount := 2 * insertsPerAgent
	if count != wantCount {
		t.Errorf("row count = %d, want %d (some inserts were lost or duplicated)", count, wantCount)
	}
}
