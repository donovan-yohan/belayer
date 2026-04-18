package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DefaultMode is the driver name resolved when .belayer/config.yaml has no
// sandbox section (or no sandbox.mode). The noop driver ships with belayer
// and is registered unconditionally.
const DefaultMode = "noop"

// ProviderTypeAPIKey is the only credential-broker provider type belayer
// currently understands. The upstream clamshell CLI supports more types
// (github, aws); belayer does not wire them yet.
const ProviderTypeAPIKey = "apikey"

// Default header and scheme used when a ProviderConfig omits them. These
// match the extend-clamshell upstream defaults and cover the Bearer-auth
// case (OPENAI_API_KEY, OPENCODE_GO_API_KEY, ...). Anthropic-style
// x-api-key is expressed by setting AuthScheme to the empty string.
const (
	DefaultAuthHeader = "Authorization"
	DefaultAuthScheme = "Bearer"
)

// ModeOverrideEnv, when set in the daemon process env, takes precedence over
// per-workspace config.yaml sandbox.mode. This is how an outer sandbox (e.g.
// clamshell running the daemon inside a one-container-per-run image) tells
// belayer "you're already sandboxed by me, use noop" without requiring every
// downstream repo's .belayer/config.yaml to opt out of mode: clamshell.
const ModeOverrideEnv = "BELAYER_SANDBOX_MODE"

// Settings holds the sandbox section of .belayer/config.yaml. A zero Settings
// signals default-noop behavior; see Settings.ModeOrDefault.
type Settings struct {
	Mode      string           `yaml:"mode"`
	Policy    string           `yaml:"policy"`
	Providers []ProviderConfig `yaml:"providers"`
}

// ProviderConfig declares a credential-brokering provider the daemon should
// register with the clamshell CLI before launching a sandbox. The real
// secret is read from the daemon's environment via SecretEnv (the config
// file never contains the secret itself) and projected into the sandbox as
// an opaque handle. The MITM proxy rewrites AuthHeader on outbound requests
// to Endpoints, substituting the handle for the real secret.
//
// Only Type == ProviderTypeAPIKey is supported today. Other types are
// parsed and validated but rejected at provider-upsert time so bad config
// is flagged even if the caller never attaches a clamshell sandbox.
type ProviderConfig struct {
	// Name is the provider's identifier in the clamshell provider store.
	// Must be a DNS-label-safe slug.
	Name string `yaml:"name"`

	// Type selects the clamshell provider type. Today: "apikey" only.
	Type string `yaml:"type"`

	// SecretEnv is the name of the environment variable holding the real
	// credential. The daemon reads it from os.Environ() at provider-upsert
	// time and passes it to clamshell via --from-existing. Not serialized
	// further — the file itself never contains the secret.
	SecretEnv string `yaml:"secret_env"`

	// Project lists the environment variables to project the opaque
	// handle into inside the sandbox. Typically equals [SecretEnv] but
	// can include legacy aliases (e.g., [OPENAI_API_KEY, OPENAI_KEY]).
	Project []string `yaml:"project"`

	// Endpoints lists the hostnames whose outbound HTTPS traffic the
	// proxy rewrites for this provider. Hostnames only; no URL scheme.
	Endpoints []string `yaml:"endpoints"`

	// AuthHeader is the HTTP header carrying the credential. Defaults to
	// "Authorization" when empty.
	AuthHeader string `yaml:"auth_header"`

	// AuthScheme is the prefix before the credential value. Defaults to
	// "Bearer". An explicitly-empty string (via AuthSchemeSet==true)
	// means no prefix — e.g., for the `x-api-key: <value>` shape.
	AuthScheme string `yaml:"auth_scheme"`

	// AuthSchemeSet records whether AuthScheme was explicitly specified
	// in YAML. This distinguishes "omitted (use default)" from "set to
	// empty string (no prefix)". The custom UnmarshalYAML implementation
	// below populates this field.
	AuthSchemeSet bool `yaml:"-"`
}

// UnmarshalYAML distinguishes an omitted auth_scheme field from one
// explicitly set to the empty string. Go's zero-value rules would
// otherwise collapse the two into an indistinguishable "".
func (p *ProviderConfig) UnmarshalYAML(node *yaml.Node) error {
	type raw struct {
		Name       string   `yaml:"name"`
		Type       string   `yaml:"type"`
		SecretEnv  string   `yaml:"secret_env"`
		Project    []string `yaml:"project"`
		Endpoints  []string `yaml:"endpoints"`
		AuthHeader string   `yaml:"auth_header"`
	}
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}
	p.Name = r.Name
	p.Type = r.Type
	p.SecretEnv = r.SecretEnv
	p.Project = r.Project
	p.Endpoints = r.Endpoints
	p.AuthHeader = r.AuthHeader

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content)-1; i += 2 {
			if node.Content[i].Value == "auth_scheme" {
				p.AuthSchemeSet = true
				p.AuthScheme = node.Content[i+1].Value
				break
			}
		}
	}
	return nil
}

// ResolvedAuthHeader returns the header name to use, applying the default.
func (p ProviderConfig) ResolvedAuthHeader() string {
	if p.AuthHeader == "" {
		return DefaultAuthHeader
	}
	return p.AuthHeader
}

// ResolvedAuthScheme returns the scheme prefix to use. When AuthSchemeSet
// is true, the explicit value (including empty string) wins. Otherwise
// the default is applied.
func (p ProviderConfig) ResolvedAuthScheme() string {
	if p.AuthSchemeSet {
		return p.AuthScheme
	}
	return DefaultAuthScheme
}

// Validate reports structural errors in the provider config. Callers
// invoke this before any upstream clamshell CLI call so misconfigurations
// surface with clear, config-relative messages.
func (p ProviderConfig) Validate() error {
	if p.Name == "" {
		return errors.New("provider: name is required")
	}
	if p.Type != ProviderTypeAPIKey {
		return fmt.Errorf("provider %q: unsupported type %q (only %q is supported)", p.Name, p.Type, ProviderTypeAPIKey)
	}
	if p.SecretEnv == "" {
		return fmt.Errorf("provider %q: secret_env is required", p.Name)
	}
	if len(p.Project) == 0 {
		return fmt.Errorf("provider %q: project must list at least one env key", p.Name)
	}
	for _, k := range p.Project {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("provider %q: project entries must be non-empty", p.Name)
		}
	}
	if len(p.Endpoints) == 0 {
		return fmt.Errorf("provider %q: endpoints must list at least one host", p.Name)
	}
	for _, h := range p.Endpoints {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("provider %q: endpoint entries must be non-empty", p.Name)
		}
	}
	return nil
}

// ModeOrDefault returns the effective sandbox mode, resolving in this order:
//  1. $BELAYER_SANDBOX_MODE, if set — an outer sandbox can override downstream
//     workspace config. Trimmed; empty string is treated as unset.
//  2. Settings.Mode, if non-empty — from the workspace's .belayer/config.yaml.
//  3. DefaultMode ("noop").
func (s Settings) ModeOrDefault() string {
	if override := strings.TrimSpace(os.Getenv(ModeOverrideEnv)); override != "" {
		return override
	}
	if s.Mode == "" {
		return DefaultMode
	}
	return s.Mode
}

// ValidateProviders returns the first structural error found across all
// configured providers, or nil if every provider is well-formed.
// Returns an error naming the duplicate when two providers share a name
// or two providers project the same env key.
func (s Settings) ValidateProviders() error {
	names := make(map[string]struct{}, len(s.Providers))
	projected := make(map[string]string, len(s.Providers))
	for i := range s.Providers {
		p := s.Providers[i]
		if err := p.Validate(); err != nil {
			return err
		}
		if _, dup := names[p.Name]; dup {
			return fmt.Errorf("provider %q: duplicate name", p.Name)
		}
		names[p.Name] = struct{}{}
		for _, k := range p.Project {
			if owner, taken := projected[k]; taken {
				return fmt.Errorf("provider %q: env key %q is also projected by provider %q", p.Name, k, owner)
			}
			projected[k] = p.Name
		}
	}
	return nil
}

// ProjectedKeys returns the flat list of env keys that any configured
// provider projects into the sandbox. Callers (BuildEnv) use this to
// redact the real secrets from the env-file written for docker exec.
func (s Settings) ProjectedKeys() []string {
	if len(s.Providers) == 0 {
		return nil
	}
	out := make([]string, 0)
	for _, p := range s.Providers {
		out = append(out, p.Project...)
	}
	return out
}

type settingsFile struct {
	Sandbox Settings `yaml:"sandbox"`
}

// LoadSettings reads the sandbox section from <workdir>/.belayer/config.yaml.
// A missing file, empty workdir, or missing section returns a zero Settings
// and nil error; callers treat an empty Mode as DefaultMode via ModeOrDefault.
func LoadSettings(workdir string) (Settings, error) {
	if workdir == "" {
		return Settings{}, nil
	}
	path := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("sandbox: read config: %w", err)
	}
	var f settingsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Settings{}, fmt.Errorf("sandbox: parse config: %w", err)
	}
	// Policy paths in config.yaml are written relative to the workspace root
	// (e.g. ".belayer/policies/default.yaml"). The daemon's cwd is unrelated,
	// so anchor non-absolute paths to workdir before handing them to drivers.
	if f.Sandbox.Policy != "" && !filepath.IsAbs(f.Sandbox.Policy) {
		f.Sandbox.Policy = filepath.Join(workdir, f.Sandbox.Policy)
	}
	if err := f.Sandbox.ValidateProviders(); err != nil {
		return Settings{}, fmt.Errorf("sandbox: %w", err)
	}
	return f.Sandbox, nil
}
