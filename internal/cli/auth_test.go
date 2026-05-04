package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScaffoldBelayerProfile_FreshCreatesAllDirs verifies that a fresh
// scaffold call creates the full Hermes profile directory tree. The set
// must match hermes_cli/profiles.py#_PROFILE_DIRS so a profile we create
// is indistinguishable from one created by `hermes profile create`.
func TestScaffoldBelayerProfile_FreshCreatesAllDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "belayer-profile")

	created, err := scaffoldBelayerProfile(dir)
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if !created {
		t.Errorf("expected created=true on fresh scaffold, got false")
	}

	for _, sub := range hermesProfileDirs {
		path := filepath.Join(dir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s to exist: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", path)
		}
	}
}

// TestScaffoldBelayerProfile_Idempotent verifies that re-scaffolding an
// existing profile is a no-op and reports created=false. Operators may
// re-run `belayer auth ensure` after every Hermes upgrade to refresh the
// plugin; existing memories/sessions must not be touched.
func TestScaffoldBelayerProfile_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "belayer-profile")

	if _, err := scaffoldBelayerProfile(dir); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	// Plant a sentinel file in memories/ so we can confirm re-scaffold doesn't
	// nuke user state.
	sentinel := filepath.Join(dir, "memories", "MEMORY.md")
	if err := os.WriteFile(sentinel, []byte("user data"), 0o644); err != nil {
		t.Fatalf("plant sentinel: %v", err)
	}

	created, err := scaffoldBelayerProfile(dir)
	if err != nil {
		t.Fatalf("second scaffold: %v", err)
	}
	if created {
		t.Errorf("expected created=false on second scaffold, got true")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel disappeared: %v", err)
	}
	if string(data) != "user data" {
		t.Errorf("sentinel was modified: got %q", string(data))
	}
}

// TestBelayerProfileDir_HermesHomeOverride verifies the env-aware path
// resolution: HERMES_HOME under .hermes/profiles/<name>/ should anchor
// belayer at the same parent profiles/ dir; HERMES_HOME at a custom root
// should nest profiles/ underneath. This lets test fixtures redirect the
// whole layout to a tmpdir without touching the real ~/.hermes.
func TestBelayerProfileDir_HermesHomeOverride(t *testing.T) {
	tests := []struct {
		name       string
		hermesHome string
		want       string
	}{
		{
			name:       "profile-style HERMES_HOME",
			hermesHome: "/tmp/hermes/profiles/coder",
			want:       "/tmp/hermes/profiles/blyr",
		},
		{
			name:       "root-style HERMES_HOME",
			hermesHome: "/tmp/custom-hermes",
			want:       "/tmp/custom-hermes/profiles/blyr",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HERMES_HOME", tc.hermesHome)
			got, err := belayerProfileDir()
			if err != nil {
				t.Fatalf("belayerProfileDir: %v", err)
			}
			if got != tc.want {
				t.Errorf("belayerProfileDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBelayerProfileDir_NoEnvFallsBackToHome verifies the default path
// when HERMES_HOME is unset: ~/.hermes/profiles/blyr.
func TestBelayerProfileDir_NoEnvFallsBackToHome(t *testing.T) {
	t.Setenv("HERMES_HOME", "")
	got, err := belayerProfileDir()
	if err != nil {
		t.Fatalf("belayerProfileDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".hermes", "profiles", "blyr")
	if got != want {
		t.Errorf("belayerProfileDir() = %q, want %q", got, want)
	}
}

// TestAuthEnsure_SkipLogin_ScaffoldsAndInstallsPlugin verifies the
// end-to-end ensure flow with --skip-login: profile dirs created, plugin
// extracted, plugin enabled in config.yaml. The login step is skipped so
// the test runs without an interactive terminal or the hermes binary
// installed.
func TestAuthEnsure_SkipLogin_ScaffoldsAndInstallsPlugin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HERMES_HOME", filepath.Join(tmp, "fake-hermes-home"))

	cmd := newAuthEnsureCmd()
	cmd.SetArgs([]string{"--skip-login"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ensure --skip-login: %v\nstderr: %s", err, stderr.String())
	}

	profileDir := filepath.Join(tmp, "fake-hermes-home", "profiles", "blyr")

	for _, sub := range hermesProfileDirs {
		if _, err := os.Stat(filepath.Join(profileDir, sub)); err != nil {
			t.Errorf("expected %s to exist: %v", sub, err)
		}
	}

	manifest := filepath.Join(profileDir, "plugins", "belayer", "plugin.yaml")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("plugin manifest missing: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(profileDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(cfg), "belayer") {
		t.Errorf("config.yaml does not list belayer plugin:\n%s", string(cfg))
	}

	if !strings.Contains(stdout.String(), "Created blyr profile at") {
		t.Errorf("expected creation log line, got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Skipped 'hermes auth login'") {
		t.Errorf("expected skip-login log line, got: %q", stdout.String())
	}
}

// TestAuthStatus_ReportsMissingProfile verifies that `belayer auth status`
// gracefully reports an unscaffolded profile without erroring. Operators
// running it pre-setup should get a clear next-step hint, not a stack trace.
func TestAuthStatus_ReportsMissingProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HERMES_HOME", filepath.Join(tmp, "empty-hermes"))

	cmd := newAuthStatusCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "not created") {
		t.Errorf("expected 'not created' hint, got: %q", out)
	}
	if !strings.Contains(out, "belayer auth ensure") {
		t.Errorf("expected next-step hint to mention `belayer auth ensure`, got: %q", out)
	}
}

// TestAuthStatus_ReportsScaffoldedProfile verifies that after a successful
// ensure, status reports the plugin as installed and the auth.json as
// missing (since --skip-login was used).
func TestAuthStatus_ReportsScaffoldedProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HERMES_HOME", filepath.Join(tmp, "fake-hermes"))

	ensure := newAuthEnsureCmd()
	ensure.SetArgs([]string{"--skip-login"})
	ensure.SetOut(&bytes.Buffer{})
	ensure.SetErr(&bytes.Buffer{})
	if err := ensure.Execute(); err != nil {
		t.Fatalf("ensure prep: %v", err)
	}

	status := newAuthStatusCmd()
	var out bytes.Buffer
	status.SetOut(&out)
	status.SetErr(&bytes.Buffer{})
	if err := status.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Status: present") {
		t.Errorf("expected 'Status: present', got: %q", got)
	}
	if !strings.Contains(got, "belayer plugin: installed") {
		t.Errorf("expected plugin-installed line, got: %q", got)
	}
	if !strings.Contains(got, "auth.json: missing") {
		t.Errorf("expected auth.json missing hint after --skip-login, got: %q", got)
	}
}
