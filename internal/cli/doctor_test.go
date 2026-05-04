package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/store"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// mkDoctorDB creates a temp SQLite database and returns its path.
func mkDoctorDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "belayer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	st.Close()
	return dbPath
}

// mkDoctorDBWithProfiles creates a store DB that references the given profile
// names in the agent_runs table (one session, one run per profile).
func mkDoctorDBWithProfiles(t *testing.T, profileNames ...string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "belayer.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Insert a single session.
	sessID, err := st.CreateSession(store.Session{Name: "test-session"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Insert one agent_run per profile name.
	for i, name := range profileNames {
		agentName := "agent" + string(rune('0'+i))
		run := store.AgentRun{
			SessionID: sessID,
			Name:      agentName,
			Profile:   name,
		}
		if _, err := st.CreateAgentRun(run); err != nil {
			t.Fatalf("create agent run for %s: %v", name, err)
		}
	}
	return dbPath
}

// mkProfileDir creates a minimal belayer-* profile directory under
// <hermesHome>/profiles/<name>/, optionally writing a .belayer-talent.yaml
// with the given crag slug and talent name.
func mkProfileDir(t *testing.T, hermesHome, profileName, cragSlug, talentName string) {
	t.Helper()
	dir := filepath.Join(hermesHome, "profiles", profileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkProfileDir: %v", err)
	}
	if cragSlug != "" {
		meta := "profile_name: " + profileName + "\n" +
			"talent_name: " + talentName + "\n" +
			"crag_slug: " + cragSlug + "\n" +
			"memory_scope: climb\n" +
			"materialized_at: 2026-01-01T00:00:00Z\n"
		if err := os.WriteFile(filepath.Join(dir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
			t.Fatalf("write .belayer-talent.yaml: %v", err)
		}
	}
}

// mkBaseProfile creates the base belayer profile dir under <hermesHome>/profiles/belayer/.
func mkBaseProfile(t *testing.T, hermesHome string) string {
	t.Helper()
	dir := filepath.Join(hermesHome, "profiles", "belayer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkBaseProfile: %v", err)
	}
	return dir
}

// runDoctorCmd executes newDoctorCmd() with the given args and returns the
// stdout and stderr strings plus any error.
func runDoctorCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd := newDoctorCmd()
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// Test 1: No profiles → reports empty.
func TestDoctorCmd_NoProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HERMES_HOME", filepath.Join(tmp, "hermes"))
	dbPath := mkDoctorDB(t)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "No belayer-* profiles found") {
		t.Errorf("expected 'No belayer-* profiles found', got:\n%s", stdout)
	}
}

// Test 2: Active profile (matching agent_runs row) → reports active.
func TestDoctorCmd_ActiveProfile(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-supervisor"
	mkProfileDir(t, hermesHome, profileName, "myproject", "supervisor")
	dbPath := mkDoctorDBWithProfiles(t, profileName)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[active]") {
		t.Errorf("expected [active] label, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, profileName) {
		t.Errorf("expected profile name %q in output, got:\n%s", profileName, stdout)
	}
	// Should not report an orphan.
	if strings.Contains(stdout, "[orphan]") {
		t.Errorf("unexpected [orphan] label for active profile, got:\n%s", stdout)
	}
}

// Test 3: Orphan profile (profile dir exists but no agent_runs row) → reports orphan.
func TestDoctorCmd_OrphanProfile(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-local-orphan-dev"
	mkProfileDir(t, hermesHome, profileName, "local", "orphan-dev")
	dbPath := mkDoctorDB(t) // empty store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[orphan]") {
		t.Errorf("expected [orphan] label, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "no matching agent_run") {
		t.Errorf("expected 'no matching agent_run' annotation, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "belayer prune") {
		t.Errorf("expected `belayer prune` hint in issues, got:\n%s", stdout)
	}
}

// Test 4: Orphan agent_runs row (profile dir gone) → reports issue.
func TestDoctorCmd_OrphanAgentRunsRow(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	// Store references a profile that has no directory.
	missingProfile := "belayer-local-missing-agent"
	dbPath := mkDoctorDBWithProfiles(t, missingProfile)
	// No directory created → orphan agent_runs row.

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "Orphan agent_runs") {
		t.Errorf("expected 'Orphan agent_runs' section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, missingProfile) {
		t.Errorf("expected missing profile name %q listed, got:\n%s", missingProfile, stdout)
	}
	if !strings.Contains(stdout, "agent_runs row exists but profile dir gone") {
		t.Errorf("expected annotation about missing dir, got:\n%s", stdout)
	}
}

// Test 5: --crag flag filters to only that crag's profiles.
func TestDoctorCmd_CragFilter(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileA := "belayer-alpha-supervisor"
	profileB := "belayer-beta-supervisor"
	mkProfileDir(t, hermesHome, profileA, "alpha", "supervisor")
	mkProfileDir(t, hermesHome, profileB, "beta", "supervisor")
	dbPath := mkDoctorDBWithProfiles(t, profileA, profileB)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath, "--crag", "alpha")
	if err != nil {
		t.Fatalf("doctor --crag alpha: %v", err)
	}

	if !strings.Contains(stdout, profileA) {
		t.Errorf("expected profile %q in filtered output, got:\n%s", profileA, stdout)
	}
	if strings.Contains(stdout, profileB) {
		t.Errorf("expected profile %q to be filtered out, got:\n%s", profileB, stdout)
	}
}

// Test 6: --json flag emits valid JSON with correct structure.
func TestDoctorCmd_JSONOutput(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-supervisor"
	mkProfileDir(t, hermesHome, profileName, "myproject", "supervisor")
	dbPath := mkDoctorDBWithProfiles(t, profileName)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v", err)
	}

	var report doctorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, stdout)
	}

	if report.TotalProfiles != 1 {
		t.Errorf("JSON total_profiles = %d, want 1", report.TotalProfiles)
	}
	if len(report.Crags) == 0 {
		t.Errorf("JSON crags empty, expected 1 crag")
	} else {
		if report.Crags[0].CragSlug != "myproject" {
			t.Errorf("JSON crags[0].crag_slug = %q, want %q", report.Crags[0].CragSlug, "myproject")
		}
		if len(report.Crags[0].Profiles) != 1 {
			t.Errorf("JSON crags[0].profiles len = %d, want 1", len(report.Crags[0].Profiles))
		} else if report.Crags[0].Profiles[0].Status != doctorProfileActive {
			t.Errorf("JSON crags[0].profiles[0].status = %q, want %q",
				report.Crags[0].Profiles[0].Status, doctorProfileActive)
		}
	}
	if report.OrphanCount != 0 {
		t.Errorf("JSON orphan_count = %d, want 0", report.OrphanCount)
	}
}

// Test 7: auth.json staleness → reports age and warning when > threshold.
func TestDoctorCmd_AuthStaleness(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)
	t.Setenv("BELAYER_AUTH_STALE_DAYS", "10")

	baseDir := mkBaseProfile(t, hermesHome)
	dbPath := mkDoctorDB(t)

	// Write an auth.json with an old mtime (40 days ago).
	authPath := filepath.Join(baseDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"token":"old"}`), 0o644); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	// Set mtime to 40 days ago.
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(authPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	// Should report auth.json with age.
	if !strings.Contains(stdout, "modified") {
		t.Errorf("expected 'modified' in output, got:\n%s", stdout)
	}
	// Should warn that it's > threshold.
	if !strings.Contains(stdout, "WARN") && !strings.Contains(stdout, "days old") {
		t.Errorf("expected staleness warning in output, got:\n%s", stdout)
	}
	// Should appear in issues.
	if !strings.Contains(stdout, "belayer auth ensure") {
		t.Errorf("expected `belayer auth ensure` hint in issues, got:\n%s", stdout)
	}
}

// Test 8: Missing base profile → reports "not created" hint.
func TestDoctorCmd_MissingBaseProfile(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)
	// No base profile dir created.
	dbPath := mkDoctorDB(t)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "not created") {
		t.Errorf("expected 'not created' hint for missing base profile, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "belayer auth ensure") {
		t.Errorf("expected `belayer auth ensure` hint, got:\n%s", stdout)
	}
}

// Test 9: Mixed active and orphan profiles in the same crag.
func TestDoctorCmd_MixedActiveOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	activeProfile := "belayer-myproject-supervisor"
	orphanProfile := "belayer-myproject-backend-dev"
	mkProfileDir(t, hermesHome, activeProfile, "myproject", "supervisor")
	mkProfileDir(t, hermesHome, orphanProfile, "myproject", "backend-dev")
	dbPath := mkDoctorDBWithProfiles(t, activeProfile) // only active is in store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[active]") {
		t.Errorf("expected [active] label, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[orphan]") {
		t.Errorf("expected [orphan] label, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 orphan profile(s)") {
		t.Errorf("expected orphan count in issues, got:\n%s", stdout)
	}
}

// Test 10: Disk usage is reported in output.
func TestDoctorCmd_DiskUsage(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-qa"
	mkProfileDir(t, hermesHome, profileName, "myproject", "qa")
	// Write a file into the profile dir to give it a known size.
	profileDir := filepath.Join(hermesHome, "profiles", profileName)
	if err := os.WriteFile(filepath.Join(profileDir, "testfile.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write testfile: %v", err)
	}
	dbPath := mkDoctorDBWithProfiles(t, profileName)

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	// Should contain a disk usage total.
	if !strings.Contains(stdout, "Total disk usage:") {
		t.Errorf("expected 'Total disk usage:' line, got:\n%s", stdout)
	}
	// Confirm it's > 0 (we wrote a file).
	if strings.Contains(stdout, "Total disk usage: 0 B across") {
		t.Errorf("expected non-zero disk usage, got:\n%s", stdout)
	}
}

// ── Phase 5.B: scope-aware orphan detection tests ─────────────────────────────

// mkProfileDirWithScope creates a minimal belayer-* profile directory with the
// given memory_scope. Useful for testing scope-aware orphan detection.
func mkProfileDirWithScope(t *testing.T, hermesHome, profileName, cragSlug, talentName, memoryScope string) {
	t.Helper()
	dir := filepath.Join(hermesHome, "profiles", profileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkProfileDirWithScope: %v", err)
	}
	meta := "profile_name: " + profileName + "\n" +
		"talent_name: " + talentName + "\n" +
		"crag_slug: " + cragSlug + "\n" +
		"memory_scope: " + memoryScope + "\n" +
		"materialized_at: 2026-01-01T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
		t.Fatalf("mkProfileDirWithScope: write .belayer-talent.yaml: %v", err)
	}
}

// Test 5B-1: Profile with memory.scope=crag and no matching run → reported
// [preserved-crag], NOT counted in orphan count.
func TestDoctorCmd_CragScopedPreservedNotOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-supervisor"
	mkProfileDirWithScope(t, hermesHome, profileName, "myproject", "supervisor", "crag")
	dbPath := mkDoctorDB(t) // empty store — no matching agent_runs row

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[preserved-crag]") {
		t.Errorf("expected [preserved-crag] label, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[orphan]") {
		t.Errorf("unexpected [orphan] label for crag-scoped profile, got:\n%s", stdout)
	}
	// Should NOT appear in the orphan count issues.
	if strings.Contains(stdout, "orphan profile(s) found") {
		t.Errorf("crag-scoped profile should not appear in orphan count issues, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "belayer prune") {
		t.Errorf("crag-scoped profile should not trigger prune recommendation, got:\n%s", stdout)
	}
}

// Test 5B-2: Profile with memory.scope=talent and no matching run → reported
// [preserved-talent].
func TestDoctorCmd_TalentScopedPreservedNotOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-backend-dev"
	mkProfileDirWithScope(t, hermesHome, profileName, "myproject", "backend-dev", "talent")
	dbPath := mkDoctorDB(t) // empty store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[preserved-talent]") {
		t.Errorf("expected [preserved-talent] label, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[orphan]") {
		t.Errorf("unexpected [orphan] label for talent-scoped profile, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "orphan profile(s) found") {
		t.Errorf("talent-scoped profile should not appear in orphan count issues, got:\n%s", stdout)
	}
}

// Test 5B-3: Profile with memory.scope=climb and no matching run → still [orphan]
// (regression check).
func TestDoctorCmd_ClimbScopedStillOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-qa"
	mkProfileDirWithScope(t, hermesHome, profileName, "myproject", "qa", "climb")
	dbPath := mkDoctorDB(t) // empty store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[orphan]") {
		t.Errorf("expected [orphan] label for climb-scoped profile, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[preserved-crag]") || strings.Contains(stdout, "[preserved-talent]") {
		t.Errorf("unexpected preserved label for climb-scoped profile, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "orphan profile(s) found") {
		t.Errorf("expected orphan count in issues, got:\n%s", stdout)
	}
}

// Test 5B-4: Profile with no .belayer-talent.yaml → defaults to orphan
// (climb default).
func TestDoctorCmd_NoMetadataFileDefaultsToOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	// Create profile dir WITHOUT any .belayer-talent.yaml.
	profileName := "belayer-myproject-reviewer"
	dir := filepath.Join(hermesHome, "profiles", profileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	dbPath := mkDoctorDB(t) // empty store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	if !strings.Contains(stdout, "[orphan]") {
		t.Errorf("expected [orphan] label when metadata file missing (defaults to climb), got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[preserved-") {
		t.Errorf("unexpected preserved label when no metadata file present, got:\n%s", stdout)
	}
}

// Test 5B-5: --include-scoped flag → preserved-crag reported as orphan.
func TestDoctorCmd_IncludeScopedFlagTreatsCragAsOrphan(t *testing.T) {
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)

	profileName := "belayer-myproject-supervisor"
	mkProfileDirWithScope(t, hermesHome, profileName, "myproject", "supervisor", "crag")
	dbPath := mkDoctorDB(t) // empty store

	stdout, _, err := runDoctorCmd(t, "--db", dbPath, "--include-scoped")
	if err != nil {
		t.Fatalf("doctor --include-scoped: %v", err)
	}

	if !strings.Contains(stdout, "[orphan]") {
		t.Errorf("expected [orphan] label with --include-scoped, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[preserved-crag]") {
		t.Errorf("unexpected [preserved-crag] with --include-scoped, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "orphan profile(s) found") {
		t.Errorf("expected orphan count in issues with --include-scoped, got:\n%s", stdout)
	}
}
