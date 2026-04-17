package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/sandbox"
)

func TestLoadSettingsMissingFileReturnsZero(t *testing.T) {
	s, err := sandbox.LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSettings with no config.yaml returned error: %v", err)
	}
	if s.Mode != "" || s.Policy != "" {
		t.Errorf("expected zero Settings, got %+v", s)
	}
	if got := s.ModeOrDefault(); got != sandbox.DefaultMode {
		t.Errorf("ModeOrDefault on zero Settings = %q, want %q", got, sandbox.DefaultMode)
	}
}

func TestLoadSettingsEmptyWorkdirReturnsZero(t *testing.T) {
	s, err := sandbox.LoadSettings("")
	if err != nil {
		t.Fatalf("LoadSettings(\"\") returned error: %v", err)
	}
	if s.Mode != "" {
		t.Errorf("expected empty Mode, got %q", s.Mode)
	}
}

func TestLoadSettingsParsesSandboxSection(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
runtime:
  up: "pnpm dev"
sandbox:
  mode: clamshell
  policy: .belayer/policies/belayer-standard.yaml
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Mode != "clamshell" {
		t.Errorf("Mode = %q, want clamshell", s.Mode)
	}
	// Relative Policy paths in config.yaml must be anchored to workdir so
	// drivers can os.ReadFile them regardless of the daemon's cwd.
	wantPolicy := filepath.Join(dir, ".belayer/policies/belayer-standard.yaml")
	if s.Policy != wantPolicy {
		t.Errorf("Policy = %q, want %q", s.Policy, wantPolicy)
	}
	if got := s.ModeOrDefault(); got != "clamshell" {
		t.Errorf("ModeOrDefault = %q, want clamshell", got)
	}
}

func TestLoadSettingsMissingSectionYieldsDefault(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
runtime:
  up: "pnpm dev"
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Mode != "" {
		t.Errorf("Mode = %q, want empty", s.Mode)
	}
	if got := s.ModeOrDefault(); got != sandbox.DefaultMode {
		t.Errorf("ModeOrDefault = %q, want %q", got, sandbox.DefaultMode)
	}
}

func TestLoadSettingsAbsolutePolicyIsPreserved(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  mode: clamshell
  policy: /etc/belayer/strict.yaml
`)
	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Policy != "/etc/belayer/strict.yaml" {
		t.Errorf("absolute Policy = %q, want unchanged", s.Policy)
	}
}

func TestLoadSettingsInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, "sandbox: [not-a-map\n")
	_, err := sandbox.LoadSettings(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadSettingsParsesAPIKeyProvider(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  mode: clamshell
  providers:
    - name: opencode
      type: apikey
      secret_env: OPENCODE_GO_API_KEY
      project: [OPENCODE_GO_API_KEY]
      endpoints: [opencode.ai]
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(s.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(s.Providers))
	}
	p := s.Providers[0]
	if p.Name != "opencode" || p.Type != "apikey" || p.SecretEnv != "OPENCODE_GO_API_KEY" {
		t.Errorf("provider fields wrong: %+v", p)
	}
	// Defaults apply when fields are omitted.
	if got := p.ResolvedAuthHeader(); got != "Authorization" {
		t.Errorf("ResolvedAuthHeader default = %q, want Authorization", got)
	}
	if got := p.ResolvedAuthScheme(); got != "Bearer" {
		t.Errorf("ResolvedAuthScheme default = %q, want Bearer", got)
	}
}

func TestLoadSettingsDistinguishesEmptyAuthSchemeFromOmitted(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  providers:
    - name: anthropic
      type: apikey
      secret_env: ANTHROPIC_API_KEY
      project: [ANTHROPIC_API_KEY]
      endpoints: [api.anthropic.com]
      auth_header: x-api-key
      auth_scheme: ""
`)

	s, err := sandbox.LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	p := s.Providers[0]
	if !p.AuthSchemeSet {
		t.Errorf("AuthSchemeSet = false, want true (auth_scheme was explicitly set to empty string)")
	}
	if got := p.ResolvedAuthScheme(); got != "" {
		t.Errorf("ResolvedAuthScheme = %q, want empty (explicit override)", got)
	}
	if got := p.ResolvedAuthHeader(); got != "x-api-key" {
		t.Errorf("ResolvedAuthHeader = %q, want x-api-key", got)
	}
}

func TestLoadSettingsRejectsUnsupportedProviderType(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  providers:
    - name: gh
      type: github
      secret_env: GITHUB_TOKEN
      project: [GITHUB_TOKEN]
      endpoints: [github.com]
`)
	_, err := sandbox.LoadSettings(dir)
	if err == nil {
		t.Fatal("expected error for unsupported provider type, got nil")
	}
}

func TestLoadSettingsRejectsMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{
			"missing_name",
			`sandbox:
  providers:
    - type: apikey
      secret_env: OPENCODE_GO_API_KEY
      project: [OPENCODE_GO_API_KEY]
      endpoints: [opencode.ai]
`,
		},
		{
			"missing_secret_env",
			`sandbox:
  providers:
    - name: opencode
      type: apikey
      project: [OPENCODE_GO_API_KEY]
      endpoints: [opencode.ai]
`,
		},
		{
			"empty_project",
			`sandbox:
  providers:
    - name: opencode
      type: apikey
      secret_env: OPENCODE_GO_API_KEY
      project: []
      endpoints: [opencode.ai]
`,
		},
		{
			"empty_endpoints",
			`sandbox:
  providers:
    - name: opencode
      type: apikey
      secret_env: OPENCODE_GO_API_KEY
      project: [OPENCODE_GO_API_KEY]
      endpoints: []
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeConfigYAML(t, dir, tc.config)
			if _, err := sandbox.LoadSettings(dir); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestLoadSettingsRejectsDuplicateProviderName(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  providers:
    - name: dup
      type: apikey
      secret_env: A
      project: [A]
      endpoints: [a.example]
    - name: dup
      type: apikey
      secret_env: B
      project: [B]
      endpoints: [b.example]
`)
	if _, err := sandbox.LoadSettings(dir); err == nil {
		t.Fatal("expected duplicate-name error, got nil")
	}
}

func TestLoadSettingsRejectsCollidingProjectedKeys(t *testing.T) {
	dir := t.TempDir()
	writeConfigYAML(t, dir, `
sandbox:
  providers:
    - name: a
      type: apikey
      secret_env: FOO
      project: [SHARED_KEY]
      endpoints: [a.example]
    - name: b
      type: apikey
      secret_env: BAR
      project: [SHARED_KEY]
      endpoints: [b.example]
`)
	if _, err := sandbox.LoadSettings(dir); err == nil {
		t.Fatal("expected colliding-projected-key error, got nil")
	}
}

func TestSettingsProjectedKeysAggregatesAcrossProviders(t *testing.T) {
	s := sandbox.Settings{
		Providers: []sandbox.ProviderConfig{
			{Name: "a", Type: "apikey", SecretEnv: "A", Project: []string{"A", "A_ALIAS"}, Endpoints: []string{"a.example"}},
			{Name: "b", Type: "apikey", SecretEnv: "B", Project: []string{"B"}, Endpoints: []string{"b.example"}},
		},
	}
	got := s.ProjectedKeys()
	want := []string{"A", "A_ALIAS", "B"}
	if len(got) != len(want) {
		t.Fatalf("ProjectedKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ProjectedKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func writeConfigYAML(t *testing.T, dir, contents string) {
	t.Helper()
	belayerDir := filepath.Join(dir, ".belayer")
	if err := os.MkdirAll(belayerDir, 0o700); err != nil {
		t.Fatalf("mkdir .belayer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(belayerDir, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}
