package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cragSlug   string
		talentName string
		instanceID string
		want       string
		wantErrSub string // non-empty → expect error containing this substring
	}{
		{
			name:       "singleton supervisor in software-co crag",
			cragSlug:   "software-co",
			talentName: "supervisor",
			instanceID: "",
			want:       "belayer-software-co-supervisor",
		},
		{
			name:       "parallel main with hash suffix",
			cragSlug:   "foo",
			talentName: "backend-dev",
			instanceID: "a3f9c2d1",
			want:       "belayer-foo-backend-dev-a3f9c2d1",
		},
		{
			name:       "generated talent instanceID ignored",
			cragSlug:   "foo",
			talentName: "generated-reviewer-1",
			instanceID: "ignored-hash",
			want:       "belayer-foo-generated-reviewer-1",
		},
		{
			name:       "generated talent no instanceID",
			cragSlug:   "foo",
			talentName: "generated-reviewer-1",
			instanceID: "",
			want:       "belayer-foo-generated-reviewer-1",
		},
		{
			name:       "singleton web-dev",
			cragSlug:   "myproject",
			talentName: "web-dev",
			instanceID: "",
			want:       "belayer-myproject-web-dev",
		},
		{
			name:       "empty cragSlug returns error",
			cragSlug:   "",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "cragSlug must not be empty",
		},
		{
			name:       "empty talentName returns error",
			cragSlug:   "foo",
			talentName: "",
			instanceID: "",
			wantErrSub: "talentName must not be empty",
		},
		{
			name:       "oversize result (long crag + long talent + hash) returns error mentioning total length",
			cragSlug:   "twenty-char-cragslug",          // 20 chars
			talentName: "thirty-five-char-talent-name-xx", // 32 chars to push over limit
			instanceID: "a3f9c2d1",                       // 8 chars → total: 8+20+1+32+1+8 = 70 > 64
			wantErrSub: "characters (max 64)",
		},
		{
			name:       "crag with uppercase rejected",
			cragSlug:   "Software-Co",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "invalid",
		},
		{
			name:       "crag with dot rejected",
			cragSlug:   "my.crag",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "invalid",
		},
		{
			name:       "generated talent with regex-incompatible chars returns error",
			cragSlug:   "foo",
			talentName: "generated-Reviewer.1",
			instanceID: "",
			wantErrSub: "invalid",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveProfileName(tc.cragSlug, tc.talentName, tc.instanceID)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("ResolveProfileName(%q, %q, %q) = %q, want error containing %q",
						tc.cragSlug, tc.talentName, tc.instanceID, got, tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProfileName(%q, %q, %q) unexpected error: %v",
					tc.cragSlug, tc.talentName, tc.instanceID, err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantErrSub string // non-empty → expect error containing this substring
	}{
		// Valid cases.
		{name: "bare belayer", input: "belayer"},
		{name: "scoped profile", input: "belayer-foo-bar"},
		{name: "two alnum chars", input: "a1"},
		{name: "hyphens and underscores", input: "a-b_c-d"},
		{name: "all lowercase digits", input: "0abc123"},
		{name: "exactly 64 chars", input: strings.Repeat("a", 64)},

		// Invalid cases.
		{
			name:       "empty string",
			input:      "",
			wantErrSub: "must not be empty",
		},
		{
			name:       "starts with hyphen",
			input:      "-belayer",
			wantErrSub: "must start with",
		},
		{
			name:       "starts with underscore",
			input:      "_belayer",
			wantErrSub: "must start with",
		},
		{
			name:       "has uppercase",
			input:      "Belayer",
			wantErrSub: "invalid",
		},
		{
			name:       "has dot",
			input:      "bel.ayer",
			wantErrSub: "invalid",
		},
		{
			name:       "has slash",
			input:      "bel/ayer",
			wantErrSub: "invalid",
		},
		{
			name:       "exceeds 64 chars",
			input:      strings.Repeat("a", 65),
			wantErrSub: "exceeds 64 characters",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProfileName(tc.input)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("ValidateProfileName(%q) = nil, want error containing %q", tc.input, tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateProfileName(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestDeriveInstanceID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seed string
	}{
		{name: "uuid-style seed", seed: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "short seed", seed: "abc"},
		{name: "another seed", seed: "xyz123"},
	}

	t.Run("empty seed returns empty string", func(t *testing.T) {
		t.Parallel()
		if got := DeriveInstanceID(""); got != "" {
			t.Errorf("DeriveInstanceID(\"\") = %q, want \"\"", got)
		}
	})

	t.Run("output is exactly 8 lowercase hex chars", func(t *testing.T) {
		t.Parallel()
		for _, tc := range tests {
			got := DeriveInstanceID(tc.seed)
			if len(got) != 8 {
				t.Errorf("DeriveInstanceID(%q) = %q, want 8 chars", tc.seed, got)
			}
			for _, ch := range got {
				if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
					t.Errorf("DeriveInstanceID(%q) = %q contains non-lowercase-hex char %q", tc.seed, got, ch)
				}
			}
		}
	})

	t.Run("deterministic: same seed same output", func(t *testing.T) {
		t.Parallel()
		for _, tc := range tests {
			a := DeriveInstanceID(tc.seed)
			b := DeriveInstanceID(tc.seed)
			if a != b {
				t.Errorf("DeriveInstanceID(%q) not deterministic: %q != %q", tc.seed, a, b)
			}
		}
	})

	t.Run("different seeds produce different outputs", func(t *testing.T) {
		t.Parallel()
		seen := make(map[string]string)
		for _, tc := range tests {
			got := DeriveInstanceID(tc.seed)
			for prevSeed, prevID := range seen {
				if got == prevID {
					t.Errorf("DeriveInstanceID(%q) == DeriveInstanceID(%q) = %q (collision)", tc.seed, prevSeed, got)
				}
			}
			seen[tc.seed] = got
		}
	})
}

// ── ProfilesRoot tests ────────────────────────────────────────────────────────

// TestProfilesRoot must NOT be parallel because subtests call t.Setenv which
// modifies process-wide environment variables.
func TestProfilesRoot(t *testing.T) {
	t.Run("falls back to ~/.hermes/profiles when HERMES_HOME unset", func(t *testing.T) {
		t.Setenv("HERMES_HOME", "")
		got, err := ProfilesRoot()
		if err != nil {
			t.Fatalf("ProfilesRoot() unexpected error: %v", err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".hermes", "profiles")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("honours HERMES_HOME as profile dir (parent is 'profiles')", func(t *testing.T) {
		// Simulate HERMES_HOME pointing directly at a named profile:
		// e.g. HERMES_HOME=/tmp/profiles/belayer → root is /tmp/profiles
		tmp := t.TempDir()
		hermesHome := filepath.Join(tmp, "profiles", "belayer")
		t.Setenv("HERMES_HOME", hermesHome)
		got, err := ProfilesRoot()
		if err != nil {
			t.Fatalf("ProfilesRoot() unexpected error: %v", err)
		}
		want := filepath.Join(tmp, "profiles")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("honours HERMES_HOME as root dir (parent is not 'profiles')", func(t *testing.T) {
		// Simulate HERMES_HOME pointing at a bare dir: profiles/ is nested.
		tmp := t.TempDir()
		t.Setenv("HERMES_HOME", tmp)
		got, err := ProfilesRoot()
		if err != nil {
			t.Fatalf("ProfilesRoot() unexpected error: %v", err)
		}
		want := filepath.Join(tmp, "profiles")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// ── MaterializeProfile tests ──────────────────────────────────────────────────

// setupBaseProfile creates a minimal fake base belayer profile at
// <root>/belayer/ with auth.json, plugins/belayer/plugin.yaml, and skills/.
// Returns the path to the base profile dir.
func setupBaseProfile(t *testing.T, root string) string {
	t.Helper()
	base := filepath.Join(root, "belayer")
	for _, sub := range []string{
		"plugins/belayer",
		"skills",
		"memories",
		"sessions",
	} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			t.Fatalf("setup base profile: mkdir %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "auth.json"), []byte(`{"token":"test"}`), 0o600); err != nil {
		t.Fatalf("setup base profile: write auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "plugins", "belayer", "plugin.yaml"), []byte("name: belayer\n"), 0o644); err != nil {
		t.Fatalf("setup base profile: write plugin.yaml: %v", err)
	}
	return base
}

// profilesRootForTest returns a temp dir wired as the profiles root by
// setting HERMES_HOME so that ProfilesRoot() resolves to tmpDir/profiles.
func profilesRootForTest(t *testing.T) (profilesRoot string, baseProfile string) {
	t.Helper()
	tmp := t.TempDir()
	profilesRoot = filepath.Join(tmp, "profiles")
	if err := os.MkdirAll(profilesRoot, 0o755); err != nil {
		t.Fatalf("mkdir profiles root: %v", err)
	}
	// Point HERMES_HOME at a "profile" dir inside profilesRoot so that
	// ProfilesRoot() detects parent == "profiles" and returns profilesRoot.
	t.Setenv("HERMES_HOME", filepath.Join(profilesRoot, "belayer"))
	baseProfile = setupBaseProfile(t, profilesRoot)
	return profilesRoot, baseProfile
}

// TestMaterializeProfile must NOT be parallel because subtests use t.Setenv
// via profilesRootForTest (modifies process-wide HERMES_HOME).
func TestMaterializeProfile(t *testing.T) {
	t.Run("fresh materialization creates dir tree", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-supervisor",
			BaseProfileDir: base,
			SystemPrompt:   "You are the supervisor.",
			Model:          "gpt-5.4",
			MemoryScope:    "climb",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		profileDir := filepath.Join(root, opts.ProfileName)

		// All non-symlinked dirs must exist.
		for _, sub := range talentProfileDirs {
			p := filepath.Join(profileDir, sub)
			if info, err := os.Stat(p); err != nil {
				t.Errorf("dir %s missing: %v", sub, err)
			} else if !info.IsDir() {
				t.Errorf("expected dir at %s, got non-dir", p)
			}
		}
	})

	t.Run("all three symlinks created with correct targets", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-backend-dev",
			BaseProfileDir: base,
			SystemPrompt:   "You are backend-dev.",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		profileDir := filepath.Join(root, opts.ProfileName)

		cases := []struct {
			link   string
			target string
		}{
			{filepath.Join(profileDir, "auth.json"), filepath.Join(base, "auth.json")},
			{filepath.Join(profileDir, "plugins", "belayer"), filepath.Join(base, "plugins", "belayer")},
			{filepath.Join(profileDir, "skills"), filepath.Join(base, "skills")},
		}
		for _, c := range cases {
			got, err := os.Readlink(c.link)
			if err != nil {
				t.Errorf("Readlink(%s): %v", c.link, err)
				continue
			}
			if got != c.target {
				t.Errorf("symlink %s → %q, want %q", c.link, got, c.target)
			}
		}
	})

	t.Run("SOUL.md contains SystemPrompt verbatim", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		soul := "You are a special reviewer.\n\nBe thorough."
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-reviewer",
			BaseProfileDir: base,
			SystemPrompt:   soul,
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(root, opts.ProfileName, "SOUL.md"))
		if err != nil {
			t.Fatalf("read SOUL.md: %v", err)
		}
		if string(got) != soul {
			t.Errorf("SOUL.md = %q, want %q", string(got), soul)
		}
	})

	t.Run("config.yaml has plugins.enabled and model when provided", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-web-dev",
			BaseProfileDir: base,
			SystemPrompt:   "You are web-dev.",
			Model:          "gpt-5.4",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, opts.ProfileName, "config.yaml"))
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "belayer") {
			t.Errorf("config.yaml missing 'belayer' plugin entry:\n%s", content)
		}
		if !strings.Contains(content, "enabled") {
			t.Errorf("config.yaml missing 'enabled' key:\n%s", content)
		}
		if !strings.Contains(content, "gpt-5.4") {
			t.Errorf("config.yaml missing model 'gpt-5.4':\n%s", content)
		}
	})

	t.Run("config.yaml model value is quoted preventing newline injection", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-injected-model",
			BaseProfileDir: base,
			SystemPrompt:   "soul",
			// A model string with an embedded newline must not inject YAML keys.
			Model: "gpt-5.4\nmalicious: true",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, opts.ProfileName, "config.yaml"))
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		content := string(data)
		// The injected newline must not produce a bare top-level YAML key.
		// We check that no line starts with "malicious:" (which would happen if
		// the newline were unescaped and the attacker-controlled suffix were
		// written as a new YAML key).
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, "malicious:") {
				t.Errorf("config.yaml has injected top-level key 'malicious:' at line %q:\n%s", line, content)
			}
		}
		// The escaped model value must appear on the model: line (quoted with \n).
		if !strings.Contains(content, `"gpt-5.4\nmalicious: true"`) {
			t.Errorf("config.yaml should contain model value quoted with escaped newline, got:\n%s", content)
		}
	})

	t.Run("config.yaml has plugins.enabled without model when Model is empty", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-qa",
			BaseProfileDir: base,
			SystemPrompt:   "You are qa.",
			Model:          "", // no model
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, opts.ProfileName, "config.yaml"))
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "belayer") {
			t.Errorf("config.yaml missing 'belayer' plugin entry")
		}
		if strings.Contains(content, "model:") {
			t.Errorf("config.yaml should not contain 'model:' when Model is empty, got:\n%s", content)
		}
	})

	t.Run(".belayer-talent.yaml has all metadata fields", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-pm",
			BaseProfileDir: base,
			SystemPrompt:   "You are pm.",
			MemoryScope:    "crag",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, opts.ProfileName, ".belayer-talent.yaml"))
		if err != nil {
			t.Fatalf("read .belayer-talent.yaml: %v", err)
		}
		content := string(data)
		for _, want := range []string{
			"profile_name:",
			"talent_name:",
			"crag_slug:",
			"memory_scope:",
			"materialized_at:",
			"belayer-myproject-pm",
			"crag",
		} {
			if !strings.Contains(content, want) {
				t.Errorf(".belayer-talent.yaml missing %q:\n%s", want, content)
			}
		}
	})

	t.Run("MemoryScope defaults to climb when empty", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-scout",
			BaseProfileDir: base,
			SystemPrompt:   "You are scout.",
			MemoryScope:    "",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("MaterializeProfile() unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(root, opts.ProfileName, ".belayer-talent.yaml"))
		if err != nil {
			t.Fatalf("read .belayer-talent.yaml: %v", err)
		}
		if !strings.Contains(string(data), "memory_scope: climb") {
			t.Errorf(".belayer-talent.yaml should have memory_scope: climb, got:\n%s", string(data))
		}
	})

	t.Run("idempotent: re-running on existing profile returns no error and does not overwrite SOUL.md", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-supervisor2",
			BaseProfileDir: base,
			SystemPrompt:   "Original soul.",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("first MaterializeProfile() unexpected error: %v", err)
		}
		// Edit SOUL.md to simulate operator edit.
		soulPath := filepath.Join(root, opts.ProfileName, "SOUL.md")
		edited := "Operator-edited soul."
		if err := os.WriteFile(soulPath, []byte(edited), 0o644); err != nil {
			t.Fatalf("write edited SOUL.md: %v", err)
		}
		// Re-run with a different SystemPrompt — SOUL.md must not be overwritten.
		opts.SystemPrompt = "New soul that must not replace edited."
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("second MaterializeProfile() unexpected error: %v", err)
		}
		got, err := os.ReadFile(soulPath)
		if err != nil {
			t.Fatalf("read SOUL.md after second materialize: %v", err)
		}
		if string(got) != edited {
			t.Errorf("SOUL.md was overwritten: got %q, want %q", string(got), edited)
		}
	})

	t.Run("idempotent: plugins/ deleted after first run is recreated on re-run", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-plugins-rerun",
			BaseProfileDir: base,
			SystemPrompt:   "soul",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("first MaterializeProfile() unexpected error: %v", err)
		}
		profileDir := filepath.Join(root, opts.ProfileName)

		// Simulate manual deletion of the plugins/ directory.
		if err := os.RemoveAll(filepath.Join(profileDir, "plugins")); err != nil {
			t.Fatalf("remove plugins/: %v", err)
		}

		// Re-run must recreate plugins/ and the belayer symlink inside it.
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("second MaterializeProfile() unexpected error: %v", err)
		}

		belayerLink := filepath.Join(profileDir, "plugins", "belayer")
		got, err := os.Readlink(belayerLink)
		if err != nil {
			t.Fatalf("Readlink(plugins/belayer) after re-run: %v", err)
		}
		want := filepath.Join(base, "plugins", "belayer")
		if got != want {
			t.Errorf("plugins/belayer symlink = %q, want %q", got, want)
		}
	})

	t.Run("idempotent: broken symlink is refreshed on re-run", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-broken-sym",
			BaseProfileDir: base,
			SystemPrompt:   "soul",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("first MaterializeProfile() unexpected error: %v", err)
		}
		// Break the auth.json symlink.
		authLink := filepath.Join(root, opts.ProfileName, "auth.json")
		if err := os.Remove(authLink); err != nil {
			t.Fatalf("remove symlink: %v", err)
		}
		if err := os.Symlink("/nonexistent/path/auth.json", authLink); err != nil {
			t.Fatalf("create broken symlink: %v", err)
		}
		// Re-run must fix the symlink.
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("second MaterializeProfile() unexpected error: %v", err)
		}
		got, err := os.Readlink(authLink)
		if err != nil {
			t.Fatalf("Readlink after refresh: %v", err)
		}
		want := filepath.Join(base, "auth.json")
		if got != want {
			t.Errorf("auth.json symlink = %q, want %q", got, want)
		}
	})

	t.Run("invalid memory scope returns error", func(t *testing.T) {
		_, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-badscope",
			BaseProfileDir: base,
			SystemPrompt:   "soul",
			MemoryScope:    "session", // invalid
		}
		if err := MaterializeProfile(opts); err == nil {
			t.Error("expected error for invalid memory scope, got nil")
		} else if !strings.Contains(err.Error(), "invalid memory scope") {
			t.Errorf("error %q does not mention 'invalid memory scope'", err.Error())
		}
	})

	t.Run("empty ProfileName returns error", func(t *testing.T) {
		_, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			BaseProfileDir: base,
			SystemPrompt:   "soul",
		}
		if err := MaterializeProfile(opts); err == nil {
			t.Error("expected error for empty ProfileName, got nil")
		}
	})

	t.Run("missing base profile dir returns error", func(t *testing.T) {
		profilesRootForTest(t) // sets HERMES_HOME
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-nobody",
			BaseProfileDir: "/nonexistent/base/profile",
			SystemPrompt:   "soul",
		}
		if err := MaterializeProfile(opts); err == nil {
			t.Error("expected error for missing base profile dir, got nil")
		} else if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("error %q does not mention 'does not exist'", err.Error())
		}
	})
}

// ── TeardownProfile tests ─────────────────────────────────────────────────────

// TestTeardownProfile must NOT be parallel because subtests use t.Setenv
// via profilesRootForTest (modifies process-wide HERMES_HOME).
func TestTeardownProfile(t *testing.T) {
	t.Run("existing profile removed cleanly", func(t *testing.T) {
		root, base := profilesRootForTest(t)
		opts := MaterializeOptions{
			ProfileName:    "belayer-myproject-teardown",
			BaseProfileDir: base,
			SystemPrompt:   "soul",
		}
		if err := MaterializeProfile(opts); err != nil {
			t.Fatalf("materialize: %v", err)
		}
		profileDir := filepath.Join(root, opts.ProfileName)
		if _, err := os.Stat(profileDir); err != nil {
			t.Fatalf("profile dir should exist before teardown: %v", err)
		}
		if err := TeardownProfile(opts.ProfileName); err != nil {
			t.Fatalf("TeardownProfile() unexpected error: %v", err)
		}
		if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
			t.Errorf("profile dir %s should be gone after teardown, stat returned: %v", profileDir, err)
		}
	})

	t.Run("missing profile is not an error", func(t *testing.T) {
		profilesRootForTest(t)
		if err := TeardownProfile("belayer-nonexistent-talent"); err != nil {
			t.Fatalf("TeardownProfile() on missing profile should not error, got: %v", err)
		}
	})

	t.Run("refuses to delete the base belayer profile", func(t *testing.T) {
		profilesRootForTest(t)
		if err := TeardownProfile("belayer"); err == nil {
			t.Error("expected error when trying to delete base belayer profile, got nil")
		} else if !strings.Contains(err.Error(), "refusing to tear down the base belayer profile") {
			t.Errorf("error %q does not mention refusal", err.Error())
		}
	})

	t.Run("refuses to delete a name not starting with belayer-", func(t *testing.T) {
		profilesRootForTest(t)
		if err := TeardownProfile("default"); err == nil {
			t.Error("expected error when trying to delete non-belayer profile, got nil")
		} else if !strings.Contains(err.Error(), "does not start with") {
			t.Errorf("error %q does not mention missing prefix", err.Error())
		}
	})

	t.Run("refuses empty profileName", func(t *testing.T) {
		profilesRootForTest(t)
		if err := TeardownProfile(""); err == nil {
			t.Error("expected error for empty profileName, got nil")
		}
	})
}
