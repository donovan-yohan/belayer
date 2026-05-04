package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// setupHermesHome creates a hermetic HERMES_HOME tree and sets the env var.
// Returns the profiles root path. It redirects both HERMES_HOME and
// BELAYER_HOME so tests never touch the real ~/.hermes or ~/.belayer.
func setupHermesHome(t *testing.T) (profilesRoot string) {
	t.Helper()
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes-home")
	profilesRoot = filepath.Join(hermesHome, "profiles")
	if err := os.MkdirAll(profilesRoot, 0o755); err != nil {
		t.Fatalf("mkdir profiles root: %v", err)
	}
	t.Setenv("HERMES_HOME", hermesHome)
	return profilesRoot
}

// setupBelayerHome creates a hermetic BELAYER_HOME and sets the env var.
func setupBelayerHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	belayerDir := filepath.Join(tmp, "belayer-home")
	if err := os.MkdirAll(belayerDir, 0o755); err != nil {
		t.Fatalf("mkdir belayer home: %v", err)
	}
	t.Setenv("BELAYER_HOME", belayerDir)
	return belayerDir
}

// createFakeProfile creates a minimal materialized profile directory under
// profilesRoot. It writes a .belayer-talent.yaml so that the profile looks
// real to teardown logic and is distinguishable in assertions.
func createFakeProfile(t *testing.T, profilesRoot, name string) {
	t.Helper()
	dir := filepath.Join(profilesRoot, name)
	if err := os.MkdirAll(filepath.Join(dir, "memories"), 0o755); err != nil {
		t.Fatalf("mkdir profile %s: %v", name, err)
	}
}

// createFakeBaseProfile creates the canonical "belayer" base profile.
func createFakeBaseProfile(t *testing.T, profilesRoot string) {
	t.Helper()
	dir := filepath.Join(profilesRoot, "belayer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir base profile: %v", err)
	}
}

// createCragWorkspace creates a .belayer/crags/<slug>/ directory tree under
// belayerDir to simulate an existing per-crag workspace.
func createCragWorkspace(t *testing.T, belayerDir, slug string) string {
	t.Helper()
	dir := filepath.Join(belayerDir, "crags", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir crag workspace: %v", err)
	}
	return dir
}

// createTalentCatalog creates a talent-catalog/ directory to verify it is
// never removed during per-crag uninstall.
func createTalentCatalog(t *testing.T, belayerDir string) string {
	t.Helper()
	dir := filepath.Join(belayerDir, "talent-catalog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir talent catalog: %v", err)
	}
	// Plant a sentinel file so we can verify it survives.
	sentinel := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(sentinel, []byte("catalog: true"), 0o644); err != nil {
		t.Fatalf("write catalog sentinel: %v", err)
	}
	return dir
}

// runUninstallCmd wires up a fresh newUninstallCmd(), sets args, optionally
// pipes stdin, and executes. Returns stdout + stderr output and any error.
func runUninstallCmd(t *testing.T, args []string, stdinData string) (string, error) {
	t.Helper()
	cmd := newUninstallCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if stdinData != "" {
		cmd.SetIn(strings.NewReader(stdinData))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// ── Test 1: --crag <slug> --yes removes only that crag's profiles + workspace ─

func TestUninstallCrag_YesRemovesCragProfilesAndWorkspace(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	// Create two profiles for "software-co" and one for another crag "other".
	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createFakeProfile(t, profilesRoot, "belayer-software-co-backend-dev")
	createFakeProfile(t, profilesRoot, "belayer-other-supervisor")
	createFakeBaseProfile(t, profilesRoot)
	createCragWorkspace(t, belayerDir, "software-co")

	output, err := runUninstallCmd(t, []string{"--crag", "software-co", "--yes"}, "")
	if err != nil {
		t.Fatalf("uninstall --crag software-co --yes: %v\noutput: %s", err, output)
	}

	// The two software-co profiles must be gone.
	for _, p := range []string{"belayer-software-co-supervisor", "belayer-software-co-backend-dev"} {
		if _, err := os.Stat(filepath.Join(profilesRoot, p)); !os.IsNotExist(err) {
			t.Errorf("expected profile %s to be removed, stat: %v", p, err)
		}
	}

	// The crag workspace must be gone.
	cragWS := filepath.Join(belayerDir, "crags", "software-co")
	if _, err := os.Stat(cragWS); !os.IsNotExist(err) {
		t.Errorf("expected crag workspace %s to be removed, stat: %v", cragWS, err)
	}

	// Output should mention "2 profile(s)".
	if !strings.Contains(output, "2 profile") {
		t.Errorf("expected output to mention 2 profiles, got: %s", output)
	}
}

// ── Test 2: --crag <slug> preserves base belayer profile ─────────────────────

func TestUninstallCrag_PreservesBaseProfile(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createFakeBaseProfile(t, profilesRoot)
	createCragWorkspace(t, belayerDir, "software-co")

	_, err := runUninstallCmd(t, []string{"--crag", "software-co", "--yes"}, "")
	if err != nil {
		t.Fatalf("uninstall crag: %v", err)
	}

	baseDir := filepath.Join(profilesRoot, "belayer")
	if _, err := os.Stat(baseDir); err != nil {
		t.Errorf("expected base profile %s to survive, stat: %v", baseDir, err)
	}
}

// ── Test 3: --crag <slug> preserves other crags ───────────────────────────────

func TestUninstallCrag_PreservesOtherCrags(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createFakeProfile(t, profilesRoot, "belayer-other-supervisor")
	createFakeProfile(t, profilesRoot, "belayer-other-backend-dev")
	createCragWorkspace(t, belayerDir, "software-co")
	createCragWorkspace(t, belayerDir, "other")

	_, err := runUninstallCmd(t, []string{"--crag", "software-co", "--yes"}, "")
	if err != nil {
		t.Fatalf("uninstall crag: %v", err)
	}

	// Other crag profiles must survive.
	for _, p := range []string{"belayer-other-supervisor", "belayer-other-backend-dev"} {
		if _, err := os.Stat(filepath.Join(profilesRoot, p)); err != nil {
			t.Errorf("expected other-crag profile %s to survive, stat: %v", p, err)
		}
	}

	// Other crag workspace must survive.
	otherWS := filepath.Join(belayerDir, "crags", "other")
	if _, err := os.Stat(otherWS); err != nil {
		t.Errorf("expected other crag workspace %s to survive, stat: %v", otherWS, err)
	}
}

// ── Test 4: --crag <slug> preserves talent-catalog ────────────────────────────

func TestUninstallCrag_PreservesTalentCatalog(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createCragWorkspace(t, belayerDir, "software-co")
	catalogDir := createTalentCatalog(t, belayerDir)

	_, err := runUninstallCmd(t, []string{"--crag", "software-co", "--yes"}, "")
	if err != nil {
		t.Fatalf("uninstall crag: %v", err)
	}

	// talent-catalog must still exist.
	sentinel := filepath.Join(catalogDir, "catalog.yaml")
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("expected talent-catalog sentinel to survive, stat: %v", err)
	}
}

// ── Test 5: --crag --keep-memories writes archive outside crag dir ─────────────

func TestUninstallCrag_KeepMemoriesWritesArchiveOutsideCragDir(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	// Create a profile with memory files.
	profName := "belayer-software-co-supervisor"
	createFakeProfile(t, profilesRoot, profName)
	memoriesDir := filepath.Join(profilesRoot, profName, "memories")
	if err := os.WriteFile(filepath.Join(memoriesDir, "MEMORY.md"), []byte("# memory content"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoriesDir, "USER.md"), []byte("# user content"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	createCragWorkspace(t, belayerDir, "software-co")

	_, err := runUninstallCmd(t, []string{"--crag", "software-co", "--yes", "--keep-memories"}, "")
	if err != nil {
		t.Fatalf("uninstall crag --keep-memories: %v", err)
	}

	// Profile must be gone.
	if _, err := os.Stat(filepath.Join(profilesRoot, profName)); !os.IsNotExist(err) {
		t.Errorf("expected profile to be removed, stat: %v", err)
	}

	// Archive must exist outside the crag dir (i.e. under uninstall-archive/).
	archiveRoot := filepath.Join(belayerDir, "uninstall-archive")
	entries, err := os.ReadDir(archiveRoot)
	if err != nil {
		t.Fatalf("expected uninstall-archive to exist: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one archive entry under uninstall-archive/")
	}

	// Read the archive JSON and verify content.
	archiveSlugDir := entries[0]
	talentDirs, err := os.ReadDir(filepath.Join(archiveRoot, archiveSlugDir.Name()))
	if err != nil {
		t.Fatalf("read talent archive dir: %v", err)
	}
	if len(talentDirs) == 0 {
		t.Fatal("expected talent subdir inside archive")
	}

	talentDir := filepath.Join(archiveRoot, archiveSlugDir.Name(), talentDirs[0].Name())
	archiveFiles, err := os.ReadDir(talentDir)
	if err != nil {
		t.Fatalf("read talent archive dir entries: %v", err)
	}
	if len(archiveFiles) == 0 {
		t.Fatal("expected archive JSON file inside talent dir")
	}

	archiveData, err := os.ReadFile(filepath.Join(talentDir, archiveFiles[0].Name()))
	if err != nil {
		t.Fatalf("read archive JSON: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(archiveData, &payload); err != nil {
		t.Fatalf("parse archive JSON: %v", err)
	}
	if !strings.Contains(payload["MEMORY.md"], "memory content") {
		t.Errorf("expected MEMORY.md in archive, got: %v", payload)
	}
	if !strings.Contains(payload["USER.md"], "user content") {
		t.Errorf("expected USER.md in archive, got: %v", payload)
	}
}

// ── Test 6: --crag with invalid slug (path traversal) → error ─────────────────

func TestUninstallCrag_InvalidSlugRejectsPathTraversal(t *testing.T) {
	setupHermesHome(t)
	setupBelayerHome(t)

	tests := []struct {
		slug    string
		wantErr string
	}{
		{"../evil", "path separators"},
		{"../../etc/passwd", "path separators"},
		{"foo/../bar", "path separators"},
		{"..", "\"..\""},
		{"foo/bar", "path separators"},
		{"foo\\bar", "path separators"},
		{"UPPER", "invalid"},
		{"with spaces", "invalid"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.slug, func(t *testing.T) {
			_, err := runUninstallCmd(t, []string{"--crag", tc.slug, "--yes"}, "")
			if err == nil {
				t.Fatalf("expected error for slug %q, got none", tc.slug)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ── Test 7: Global uninstall --yes removes all belayer-* profiles + base + home

func TestUninstallGlobal_YesRemovesAllProfilesAndHome(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)

	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createFakeProfile(t, profilesRoot, "belayer-software-co-backend-dev")
	createFakeProfile(t, profilesRoot, "belayer-other-supervisor")
	createFakeBaseProfile(t, profilesRoot)
	// Plant something in belayer home to confirm it's removed.
	if err := os.WriteFile(filepath.Join(belayerDir, "daemon.db"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write daemon.db: %v", err)
	}

	output, err := runUninstallCmd(t, []string{"--yes"}, "")
	if err != nil {
		t.Fatalf("global uninstall --yes: %v\noutput: %s", err, output)
	}

	// All belayer-* profiles must be gone.
	for _, p := range []string{
		"belayer-software-co-supervisor",
		"belayer-software-co-backend-dev",
		"belayer-other-supervisor",
	} {
		if _, err := os.Stat(filepath.Join(profilesRoot, p)); !os.IsNotExist(err) {
			t.Errorf("expected profile %s to be removed, stat: %v", p, err)
		}
	}

	// Base belayer profile must be gone.
	baseDir := filepath.Join(profilesRoot, "belayer")
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		t.Errorf("expected base profile to be removed, stat: %v", err)
	}

	// Belayer home must be gone.
	if _, err := os.Stat(belayerDir); !os.IsNotExist(err) {
		t.Errorf("expected belayer home %s to be removed, stat: %v", belayerDir, err)
	}

	if !strings.Contains(output, "uninstalled") {
		t.Errorf("expected output to confirm uninstall, got: %s", output)
	}
}

// ── Test 8: Global uninstall preserves other ~/.hermes/profiles/ entries ───────

func TestUninstallGlobal_PreservesOtherHermesProfiles(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	setupBelayerHome(t)

	// Create belayer profiles.
	createFakeProfile(t, profilesRoot, "belayer-software-co-supervisor")
	createFakeBaseProfile(t, profilesRoot)

	// Create non-belayer profiles that must survive.
	for _, nonBelayer := range []string{"default", "other-tool", "my-custom"} {
		dir := filepath.Join(profilesRoot, nonBelayer)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir non-belayer profile %s: %v", nonBelayer, err)
		}
		// Plant a sentinel.
		sentinel := filepath.Join(dir, "sentinel.txt")
		if err := os.WriteFile(sentinel, []byte("keep me"), 0o644); err != nil {
			t.Fatalf("write sentinel for %s: %v", nonBelayer, err)
		}
	}

	_, err := runUninstallCmd(t, []string{"--yes"}, "")
	if err != nil {
		t.Fatalf("global uninstall: %v", err)
	}

	// Non-belayer profiles and their sentinels must survive.
	for _, nonBelayer := range []string{"default", "other-tool", "my-custom"} {
		sentinel := filepath.Join(profilesRoot, nonBelayer, "sentinel.txt")
		data, err := os.ReadFile(sentinel)
		if err != nil {
			t.Errorf("expected sentinel for profile %s to survive, read: %v", nonBelayer, err)
			continue
		}
		if string(data) != "keep me" {
			t.Errorf("sentinel for %s was modified: %q", nonBelayer, string(data))
		}
	}
}

// ── Test 9: No --yes + non-TTY stdin → error ─────────────────────────────────

func TestUninstallNoYes_NonTTYStdinReturnsError(t *testing.T) {
	// In test environments stdin is always a pipe (not a TTY), so we can
	// directly call the helper functions. We skip this test on exotic CI
	// setups where stdin happens to be a TTY (extremely unlikely in CI).
	if stdinIsTTY() {
		t.Skip("stdin is a TTY in this environment; test only meaningful in non-TTY context")
	}

	profilesRoot := setupHermesHome(t)
	belayerDir := setupBelayerHome(t)
	createFakeProfile(t, profilesRoot, "belayer-myapp-supervisor")
	createCragWorkspace(t, belayerDir, "myapp")

	// Per-crag without --yes.
	_, err := runUninstallCmd(t, []string{"--crag", "myapp"}, "")
	if err == nil {
		t.Fatal("expected error for non-TTY stdin without --yes (per-crag)")
	}
	if !strings.Contains(err.Error(), "TTY") && !strings.Contains(err.Error(), "tty") {
		t.Errorf("error = %q, want TTY-related message", err.Error())
	}

	// Global without --yes.
	createFakeBaseProfile(t, profilesRoot)
	_, err = runUninstallCmd(t, []string{}, "")
	if err == nil {
		t.Fatal("expected error for non-TTY stdin without --yes (global)")
	}
	if !strings.Contains(err.Error(), "TTY") && !strings.Contains(err.Error(), "tty") {
		t.Errorf("error = %q, want TTY-related message", err.Error())
	}
}

// ── Additional edge cases ─────────────────────────────────────────────────────

// TestUninstallCrag_IdempotentOnAlreadyRemoved verifies that re-running on
// already-uninstalled state returns cleanly.
func TestUninstallCrag_IdempotentOnAlreadyRemoved(t *testing.T) {
	setupHermesHome(t)
	setupBelayerHome(t)
	// Nothing exists — should print "nothing to remove" and return nil.
	output, err := runUninstallCmd(t, []string{"--crag", "nonexistent", "--yes"}, "")
	if err != nil {
		t.Fatalf("uninstall missing crag: %v", err)
	}
	if !strings.Contains(output, "Nothing to remove") {
		t.Errorf("expected 'Nothing to remove' message, got: %s", output)
	}
}

// TestUninstallGlobal_IdempotentOnAlreadyRemoved verifies global uninstall
// is safe to re-run when nothing exists.
func TestUninstallGlobal_IdempotentOnAlreadyRemoved(t *testing.T) {
	// Point HERMES_HOME at an empty (no profiles/ dir) tmp tree and BELAYER_HOME
	// at a path that does not yet exist. This simulates an already-uninstalled
	// state.
	tmp := t.TempDir()
	hermesHome := filepath.Join(tmp, "hermes-home")
	if err := os.MkdirAll(hermesHome, 0o755); err != nil {
		t.Fatalf("mkdir hermesHome: %v", err)
	}
	// profiles/ does not exist → ProfilesRoot() returns path but ReadDir gets ENOENT.
	t.Setenv("HERMES_HOME", hermesHome)
	// BELAYER_HOME points at a path that does not exist.
	t.Setenv("BELAYER_HOME", filepath.Join(tmp, "nonexistent-belayer"))

	output, err := runUninstallCmd(t, []string{"--yes"}, "")
	if err != nil {
		t.Fatalf("global uninstall on empty state: %v", err)
	}
	if !strings.Contains(output, "Nothing to remove") {
		t.Errorf("expected 'Nothing to remove' message, got: %s", output)
	}
}

// TestValidateCragSlug verifies path-traversal and character rejection.
func TestValidateCragSlug(t *testing.T) {
	tests := []struct {
		slug    string
		wantErr bool
	}{
		{"software-co", false},
		{"myapp", false},
		{"my-app-123", false},
		{"", true},
		{"../evil", true},
		{"..", true},
		{"foo/../bar", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"UPPER", true},
		{"with spaces", true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.slug, func(t *testing.T) {
			err := validateCragSlug(tc.slug)
			if tc.wantErr && err == nil {
				t.Errorf("validateCragSlug(%q): expected error, got nil", tc.slug)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateCragSlug(%q): unexpected error: %v", tc.slug, err)
			}
		})
	}
}

// TestListCragProfiles verifies that only profiles matching the crag prefix
// are returned and that profiles for other crags are excluded.
func TestListCragProfiles(t *testing.T) {
	profilesRoot := setupHermesHome(t)
	setupBelayerHome(t)

	for _, name := range []string{
		"belayer-software-co-supervisor",
		"belayer-software-co-backend-dev",
		"belayer-other-supervisor",
		"belayer",
		"default",
	} {
		dir := filepath.Join(profilesRoot, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	got, err := listCragProfiles(profilesRoot, "software-co")
	if err != nil {
		t.Fatalf("listCragProfiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d profiles, want 2: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.HasPrefix(g, "belayer-software-co-") {
			t.Errorf("unexpected profile %q in result", g)
		}
	}

	// Verify that "belayer" (base) and "default" (non-belayer) are never included.
	all, err := listBelayerProfiles(profilesRoot)
	if err != nil {
		t.Fatalf("listBelayerProfiles: %v", err)
	}
	for _, a := range all {
		if a == "belayer" || a == "default" {
			t.Errorf("listBelayerProfiles returned non-fork profile %q", a)
		}
	}
}
