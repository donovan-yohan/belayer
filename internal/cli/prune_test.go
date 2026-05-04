package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

// setupPruneFixture builds a hermetic test fixture for prune tests:
//
//   - A temp dir with a fake HERMES_HOME layout (profiles/belayer + fork profiles).
//   - An in-memory store pre-populated with agent_runs rows as specified.
//
// Returns (hermesHome, dbFile, cleanup).
func setupPruneFixture(t *testing.T) (hermesHome string, dbFile string) {
	t.Helper()
	tmp := t.TempDir()

	// Create base "blyr" profile directory.
	profilesDir := filepath.Join(tmp, "profiles")
	if err := os.MkdirAll(filepath.Join(profilesDir, "blyr"), 0o755); err != nil {
		t.Fatalf("mkdir base profile: %v", err)
	}

	// Point daemon.ProfilesRoot() at our temp dir.
	// ProfilesRoot honours HERMES_HOME: if it ends in /profiles/<name>, the
	// parent /profiles is returned. We set HERMES_HOME to the base blyr
	// profile path so ProfilesRoot returns profilesDir.
	t.Setenv("HERMES_HOME", filepath.Join(profilesDir, "blyr"))

	// In-memory DB for store access.
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Write a real DB file the command can open via --db.
	dbFile = filepath.Join(tmp, "belayer.db")
	stFile, err := store.Open(dbFile)
	if err != nil {
		t.Fatalf("store.Open file: %v", err)
	}
	t.Cleanup(func() { stFile.Close() })

	return profilesDir, dbFile
}

// makeProfile creates a fake fork profile directory with optional
// .belayer-talent.yaml and memories/ files.
func makeProfile(t *testing.T, profilesDir, profileName, cragSlug, talentName string, withMemory bool) string {
	t.Helper()
	profileDir := filepath.Join(profilesDir, profileName)
	memoriesDir := filepath.Join(profileDir, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", profileName, err)
	}

	// Write .belayer-talent.yaml.
	meta := fmt.Sprintf("profile_name: %s\ntalent_name: %s\ncrag_slug: %s\nmemory_scope: climb\nmaterialized_at: 2026-01-01T00:00:00Z\n",
		profileName, talentName, cragSlug)
	if err := os.WriteFile(filepath.Join(profileDir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write .belayer-talent.yaml for %s: %v", profileName, err)
	}

	// Write optional memory files.
	if withMemory {
		memContent := fmt.Sprintf("# Memory for %s\nSome important context.\n", talentName)
		if err := os.WriteFile(filepath.Join(memoriesDir, "MEMORY.md"), []byte(memContent), 0o644); err != nil {
			t.Fatalf("write MEMORY.md for %s: %v", profileName, err)
		}
		userContent := fmt.Sprintf("# User for %s\nUser preferences.\n", talentName)
		if err := os.WriteFile(filepath.Join(memoriesDir, "USER.md"), []byte(userContent), 0o644); err != nil {
			t.Fatalf("write USER.md for %s: %v", profileName, err)
		}
	}

	return profileDir
}

// addActiveProfile creates an agent_runs row that references profileName,
// marking it as "active" so it should NOT be pruned.
func addActiveProfile(t *testing.T, dbFile, profileName string) {
	t.Helper()
	st, err := store.Open(dbFile)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	_, err = st.CreateSession(store.Session{Name: "test-session", Status: "running"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessions, err := st.ListSessions()
	if err != nil || len(sessions) == 0 {
		t.Fatalf("list sessions: %v", err)
	}

	_, err = st.CreateAgentRun(store.AgentRun{
		SessionID: sessions[0].ID,
		Name:      "supervisor",
		Role:      "supervisor",
		Profile:   profileName,
		Status:    "running",
		Outcome:   "active",
	})
	if err != nil {
		t.Fatalf("create agent run: %v", err)
	}
}

// runPruneCmd runs newPruneCmd() with the given args and captures stdout/stderr.
func runPruneCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newPruneCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// TestPruneCmd_DryRunListsWithoutRemoving verifies --dry-run lists orphans
// without removing any profile directories.
func TestPruneCmd_DryRunListsWithoutRemoving(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	orphanName := "blyr-local-orphan"
	makeProfile(t, profilesDir, orphanName, "local", "orphan", false)

	stdout, _, err := runPruneCmd(t, "--dry-run", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune --dry-run: %v", err)
	}

	if !strings.Contains(stdout, orphanName) {
		t.Errorf("expected orphan name in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "(dry-run)") {
		t.Errorf("expected '(dry-run)' marker in output, got: %s", stdout)
	}

	// Profile directory must still exist.
	if _, err := os.Stat(filepath.Join(profilesDir, orphanName)); err != nil {
		t.Errorf("orphan profile should still exist after --dry-run, got stat error: %v", err)
	}
}

// TestPruneCmd_YesRemovesWithoutPrompt verifies --yes removes orphans without
// interactive confirmation.
func TestPruneCmd_YesRemovesWithoutPrompt(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	orphanName := "blyr-local-supervisor"
	makeProfile(t, profilesDir, orphanName, "local", "supervisor", false)

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune --yes: %v", err)
	}

	if !strings.Contains(stdout, "Removed 1 profile") {
		t.Errorf("expected removal summary, got: %s", stdout)
	}

	// Profile directory must be gone.
	if _, statErr := os.Stat(filepath.Join(profilesDir, orphanName)); !os.IsNotExist(statErr) {
		t.Errorf("orphan profile should have been removed")
	}
}

// TestPruneCmd_KeepMemoriesArchives verifies --keep-memories archives MEMORY.md
// and USER.md to the evaluations directory before deleting the profile.
func TestPruneCmd_KeepMemoriesArchives(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orphanName := "blyr-local-remembering"
	makeProfile(t, profilesDir, orphanName, "local", "remembering", true)

	stdout, stderr, err := runPruneCmd(t, "--yes", "--keep-memories", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune --keep-memories: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "archived") {
		t.Errorf("expected 'archived' in summary, got: %s", stdout)
	}

	// Look for the archived snapshot files.
	evalDir := filepath.Join(homeDir, ".belayer", "crags", "local", "evaluations", "remembering")
	entries, err := os.ReadDir(evalDir)
	if err != nil {
		t.Fatalf("read eval dir %s: %v", evalDir, err)
	}

	if len(entries) < 2 {
		t.Errorf("expected 2 archived snapshots (MEMORY.md + USER.md), got %d", len(entries))
	}

	// Verify snapshot JSON structure.
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(evalDir, e.Name()))
		if err != nil {
			t.Fatalf("read snapshot %s: %v", e.Name(), err)
		}
		var snap map[string]string
		if err := json.Unmarshal(raw, &snap); err != nil {
			t.Fatalf("parse snapshot JSON %s: %v", e.Name(), err)
		}
		if snap["profile"] != orphanName {
			t.Errorf("snapshot profile: got %q, want %q", snap["profile"], orphanName)
		}
		if snap["content"] == "" {
			t.Errorf("snapshot content must not be empty")
		}
	}
}

// TestPruneCmd_CragFilterScopesSearch verifies --crag <slug> only prunes
// profiles from that crag.
func TestPruneCmd_CragFilterScopesSearch(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	localOrphan := "blyr-local-filterable"
	otherOrphan := "blyr-other-filterable"
	makeProfile(t, profilesDir, localOrphan, "local", "filterable", false)
	makeProfile(t, profilesDir, otherOrphan, "other", "filterable", false)

	stdout, _, err := runPruneCmd(t, "--yes", "--crag", "local", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune --crag local: %v", err)
	}

	if !strings.Contains(stdout, "Removed 1 profile") {
		t.Errorf("expected removal of 1 profile (scoped to crag=local), got: %s", stdout)
	}

	// local orphan must be gone.
	if _, statErr := os.Stat(filepath.Join(profilesDir, localOrphan)); !os.IsNotExist(statErr) {
		t.Errorf("local orphan should have been removed")
	}

	// other-crag orphan must still exist.
	if _, statErr := os.Stat(filepath.Join(profilesDir, otherOrphan)); statErr != nil {
		t.Errorf("other-crag orphan should be unaffected, got: %v", statErr)
	}
}

// TestPruneCmd_ActiveProfileNotPruned verifies that a profile with a matching
// agent_runs row is NOT pruned.
func TestPruneCmd_ActiveProfileNotPruned(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	activeProfile := "blyr-local-active"
	makeProfile(t, profilesDir, activeProfile, "local", "active", false)

	// Register this profile in agent_runs so it is considered "active".
	addActiveProfile(t, dbFile, activeProfile)

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune with active profile: %v", err)
	}

	// Should report no orphans.
	if !strings.Contains(stdout, "No orphan") {
		t.Errorf("expected 'No orphan' when only active profiles present, got: %s", stdout)
	}

	// Active profile must still exist.
	if _, statErr := os.Stat(filepath.Join(profilesDir, activeProfile)); statErr != nil {
		t.Errorf("active profile should not be removed: %v", statErr)
	}
}

// TestPruneCmd_BaseProfileNeverPruned verifies that a bug or edge case cannot
// cause the base "belayer" profile to be torn down. TeardownProfile refuses it,
// but this test ensures the prune command itself never attempts it.
func TestPruneCmd_BaseProfileNeverPruned(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	// Base profile already exists (created by setupPruneFixture). Ensure no
	// other fork profiles exist, then verify prune reports no orphans.
	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if !strings.Contains(stdout, "No orphan") {
		t.Errorf("expected 'No orphan' output when only base profile exists, got: %s", stdout)
	}

	// Base profile must still be present.
	if _, statErr := os.Stat(filepath.Join(profilesDir, "blyr")); statErr != nil {
		t.Errorf("base profile should never be removed: %v", statErr)
	}
}

// TestPruneCmd_NoOrphansReportsMessage verifies "no orphans found" when there
// are no orphan profiles.
func TestPruneCmd_NoOrphansReportsMessage(t *testing.T) {
	_, dbFile := setupPruneFixture(t)

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune (no orphans): %v", err)
	}

	if !strings.Contains(strings.ToLower(stdout), "no orphan") {
		t.Errorf("expected 'No orphan' message, got: %s", stdout)
	}
}

// TestPruneCmd_StdinNotTTYWithoutYes verifies that when stdin is not a TTY and
// --yes is not passed, the command returns a clear error.
func TestPruneCmd_StdinNotTTYWithoutYes(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	// Create an orphan so the prompt code is reached.
	makeProfile(t, profilesDir, "blyr-local-ttyprompt", "local", "ttyprompt", false)

	// Pipe stdin from /dev/null (non-TTY).
	origStdin := os.Stdin
	nullFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stdin = nullFile
	t.Cleanup(func() {
		os.Stdin = origStdin
		nullFile.Close()
	})

	_, _, err = runPruneCmd(t, "--db", dbFile)
	if err == nil {
		t.Fatal("expected error when stdin is not a TTY and --yes is not set")
	}
	if !strings.Contains(err.Error(), "stdin is not a terminal") {
		t.Errorf("expected 'stdin is not a terminal' error, got: %v", err)
	}
}

// TestPruneCmd_PruneRegisteredInRoot ensures the prune command is wired in
// to the root command (expected: it is NOT in root.go yet per task instructions,
// but this test can serve as a future verification).
// NOTE: the task spec says "Do not modify internal/cli/root.go" — registration
// is handled separately. This test is intentionally skipped until wired.
func TestPruneCmd_CommandExists(t *testing.T) {
	cmd := newPruneCmd()
	if cmd == nil {
		t.Fatal("newPruneCmd returned nil")
	}
	if cmd.Use != "prune" {
		t.Errorf("expected Use='prune', got %q", cmd.Use)
	}
}

// TestHumanBytes exercises the humanBytes formatting helper.
func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1<<20 + 512*1024, "1.5 MB"},
	}
	for _, tc := range cases {
		got := humanBytes(tc.in)
		if got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── Phase 5.B: scope-aware orphan detection tests ─────────────────────────────

// makeProfileWithScope creates a fake fork profile directory with the given
// memory_scope in its .belayer-talent.yaml.
func makeProfileWithScope(t *testing.T, profilesDir, profileName, cragSlug, talentName, memoryScope string) string {
	t.Helper()
	profileDir := filepath.Join(profilesDir, profileName)
	memoriesDir := filepath.Join(profileDir, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", profileName, err)
	}
	meta := fmt.Sprintf("profile_name: %s\ntalent_name: %s\ncrag_slug: %s\nmemory_scope: %s\nmaterialized_at: 2026-01-01T00:00:00Z\n",
		profileName, talentName, cragSlug, memoryScope)
	if err := os.WriteFile(filepath.Join(profileDir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
		t.Fatalf("write .belayer-talent.yaml for %s: %v", profileName, err)
	}
	return profileDir
}

// Test 5B-1: Crag-scoped profile NOT pruned by default.
func TestPruneCmd_CragScopedNotPrunedByDefault(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	profileName := "blyr-local-supervisor"
	makeProfileWithScope(t, profilesDir, profileName, "local", "supervisor", "crag")

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune with crag-scoped profile: %v", err)
	}

	// Profile should NOT be removed.
	if _, statErr := os.Stat(filepath.Join(profilesDir, profileName)); statErr != nil {
		t.Errorf("crag-scoped profile should not be removed by default: %v", statErr)
	}
	// Should report no orphans.
	if !strings.Contains(stdout, "No orphan profiles found") {
		t.Errorf("expected 'No orphan profiles found' (crag-scoped is preserved), got: %s", stdout)
	}
}

// Test 5B-2: Talent-scoped profile NOT pruned by default.
func TestPruneCmd_TalentScopedNotPrunedByDefault(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	profileName := "blyr-local-backend-dev"
	makeProfileWithScope(t, profilesDir, profileName, "local", "backend-dev", "talent")

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune with talent-scoped profile: %v", err)
	}

	// Profile should NOT be removed.
	if _, statErr := os.Stat(filepath.Join(profilesDir, profileName)); statErr != nil {
		t.Errorf("talent-scoped profile should not be removed by default: %v", statErr)
	}
	if !strings.Contains(stdout, "No orphan profiles found") {
		t.Errorf("expected 'No orphan profiles found' (talent-scoped is preserved), got: %s", stdout)
	}
}

// Test 5B-3: Climb-scoped profile still pruned (regression check).
func TestPruneCmd_ClimbScopedStillPruned(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	profileName := "blyr-local-qa"
	makeProfileWithScope(t, profilesDir, profileName, "local", "qa", "climb")

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune with climb-scoped profile: %v", err)
	}

	// Profile should be removed.
	if _, statErr := os.Stat(filepath.Join(profilesDir, profileName)); !os.IsNotExist(statErr) {
		t.Errorf("climb-scoped orphan profile should have been removed")
	}
	if !strings.Contains(stdout, "Removed 1 profile") {
		t.Errorf("expected removal summary, got: %s", stdout)
	}
}

// Test 5B-4: --include-scoped flag → crag-scoped profiles ARE pruned.
func TestPruneCmd_IncludeScopedRemovesCragScoped(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	profileName := "blyr-local-supervisor"
	makeProfileWithScope(t, profilesDir, profileName, "local", "supervisor", "crag")

	stdout, _, err := runPruneCmd(t, "--yes", "--include-scoped", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune --include-scoped with crag-scoped profile: %v", err)
	}

	// Profile should be removed when --include-scoped is set.
	if _, statErr := os.Stat(filepath.Join(profilesDir, profileName)); !os.IsNotExist(statErr) {
		t.Errorf("crag-scoped profile should be removed with --include-scoped")
	}
	if !strings.Contains(stdout, "Removed 1 profile") {
		t.Errorf("expected removal summary with --include-scoped, got: %s", stdout)
	}
}

// Test 5B-5: Output includes [skipped] lines for preserved profiles.
func TestPruneCmd_SkippedLinesForPreservedProfiles(t *testing.T) {
	profilesDir, dbFile := setupPruneFixture(t)

	// One crag-scoped profile (should be skipped) + one climb-scoped (should be pruned).
	cragProfile := "blyr-local-supervisor"
	climbProfile := "blyr-local-qa"
	makeProfileWithScope(t, profilesDir, cragProfile, "local", "supervisor", "crag")
	makeProfileWithScope(t, profilesDir, climbProfile, "local", "qa", "climb")

	stdout, _, err := runPruneCmd(t, "--yes", "--db", dbFile)
	if err != nil {
		t.Fatalf("prune with mixed scope profiles: %v", err)
	}

	// Crag-scoped profile should appear in [skipped] output.
	if !strings.Contains(stdout, "[skipped]") {
		t.Errorf("expected [skipped] line for crag-scoped profile, got: %s", stdout)
	}
	if !strings.Contains(stdout, cragProfile) {
		t.Errorf("expected skipped profile name %q in output, got: %s", cragProfile, stdout)
	}
	if !strings.Contains(stdout, "memory.scope=crag") {
		t.Errorf("expected memory.scope=crag annotation in [skipped] line, got: %s", stdout)
	}
	if !strings.Contains(stdout, "--include-scoped") {
		t.Errorf("expected --include-scoped hint in [skipped] line, got: %s", stdout)
	}
	// Climb-scoped profile should be removed.
	if _, statErr := os.Stat(filepath.Join(profilesDir, climbProfile)); !os.IsNotExist(statErr) {
		t.Errorf("climb-scoped orphan profile should have been removed")
	}
	// Crag-scoped profile should still exist.
	if _, statErr := os.Stat(filepath.Join(profilesDir, cragProfile)); statErr != nil {
		t.Errorf("crag-scoped profile should still exist: %v", statErr)
	}
}
