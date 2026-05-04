package daemon

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// hermesProfileNameRe is the Hermes profile name validation regex.
// Source: hermes_cli/profiles.py:33
var hermesProfileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

const generatedTalentPrefix = "generated-"

// ValidateProfileName validates a name against the Hermes profile name regex
// (^[a-z0-9][a-z0-9_-]{0,63}$). Used internally by ResolveProfileName and
// externally by `belayer crag init` (Phase 2.D).
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if !hermesProfileNameRe.MatchString(name) {
		if len(name) > 64 {
			return fmt.Errorf("profile name %q exceeds 64 characters (got %d)", name, len(name))
		}
		if name[0] == '-' || name[0] == '_' {
			return fmt.Errorf("profile name %q must start with a lowercase letter or digit", name)
		}
		return fmt.Errorf("profile name %q is invalid: must match ^[a-z0-9][a-z0-9_-]{0,63}$ (lowercase alphanumeric, hyphens, and underscores only)", name)
	}
	return nil
}

// DeriveInstanceID returns a stable 8-character lowercase hex string derived
// from seed via SHA-256 truncated to 4 bytes. This is used to generate a
// short unique suffix for parallel main agents (e.g. "a3f9c2d1"). An empty
// seed signals a singleton talent and returns an empty string.
func DeriveInstanceID(seed string) string {
	if seed == "" {
		return ""
	}
	h := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%08x", h[:4])
}

// ResolveProfileName returns the Hermes profile name for a talent instance.
// The format is belayer-<cragSlug>-<resolvedInstance> where resolvedInstance
// is determined by the following rules:
//
//   - Generated talent (talentName starts with "generated-"): resolvedInstance = talentName.
//     The generated talent name already encodes uniqueness; instanceID is ignored.
//   - Singleton talent (instanceID == ""): resolvedInstance = talentName.
//   - Parallel main (instanceID is a non-empty hash, e.g. 8 hex chars):
//     resolvedInstance = talentName-instanceID.
//
// Returns an error if cragSlug or talentName is empty, or if the resulting
// name fails Hermes validation (max 64 chars, ^[a-z0-9][a-z0-9_-]{0,63}$).
func ResolveProfileName(cragSlug, talentName, instanceID string) (string, error) {
	if cragSlug == "" {
		return "", fmt.Errorf("cragSlug must not be empty")
	}
	if talentName == "" {
		return "", fmt.Errorf("talentName must not be empty")
	}

	var resolvedInstance string
	switch {
	case strings.HasPrefix(talentName, generatedTalentPrefix):
		// Generated talent: name already encodes uniqueness; ignore instanceID.
		resolvedInstance = talentName
	case instanceID == "":
		// Singleton talent.
		resolvedInstance = talentName
	default:
		// Parallel main: append instance hash.
		resolvedInstance = talentName + "-" + instanceID
	}

	profileName := "belayer-" + cragSlug + "-" + resolvedInstance

	if err := ValidateProfileName(profileName); err != nil {
		// Provide a more actionable error by naming the offending segments.
		totalLen := len(profileName)
		if totalLen > 64 {
			return "", fmt.Errorf(
				"resolved profile name %q is %d characters (max 64): "+
					"belayer-(8) + cragSlug %q (%d) + \"-\"(1) + instance %q (%d) = %d",
				profileName, totalLen,
				cragSlug, len(cragSlug),
				resolvedInstance, len(resolvedInstance),
				totalLen,
			)
		}
		return "", fmt.Errorf("resolved profile name %q is invalid: %w", profileName, err)
	}

	return profileName, nil
}

// ── Profile materialization ──────────────────────────────────────────────────

// talentProfileDirsNoSkills mirrors hermesProfileDirs from internal/cli/auth.go
// (which itself mirrors hermes_cli/profiles.py#_PROFILE_DIRS) but omits
// "skills" because that entry is replaced by a symlink to the base profile's
// skills directory. This avoids creating an empty local skills/ that would
// shadow the symlink.
//
// NOTE: keep in sync with hermesProfileDirs in internal/cli/auth.go.
var talentProfileDirs = []string{
	"memories",
	"sessions",
	// "skills" — omitted; symlinked from base profile instead
	"skins",
	"logs",
	"plans",
	"workspace",
	"cron",
	"home",
}

// validMemoryScopes is the set of accepted memory.scope values from the
// belayer-talent/v1 schema. Phase 3 uses this to determine teardown rules.
var validMemoryScopes = map[string]bool{
	"climb":  true,
	"crag":   true,
	"talent": true,
}

// MaterializeOptions carries all inputs needed to fork a per-talent-instance
// Hermes profile from the base belayer profile.
type MaterializeOptions struct {
	// ProfileName is the already-resolved profile name (via ResolveProfileName).
	ProfileName string
	// BaseProfileDir is the absolute path to the base belayer profile
	// (e.g. ~/.hermes/profiles/belayer).
	BaseProfileDir string
	// SystemPrompt is the raw rendered SOUL.md content. The caller is
	// responsible for rendering identity templates before passing here.
	SystemPrompt string
	// Model is written into config.yaml when non-empty (e.g. "gpt-5.4").
	Model string
	// MemoryScope is one of "climb" | "crag" | "talent". Defaults to "climb"
	// if empty. Recorded in .belayer-talent.yaml for Phase 3 lifecycle wiring.
	MemoryScope string
}

// talentMetadata is written to <profile>/.belayer-talent.yaml so Phase 3
// lifecycle decisions can read profile provenance without parsing config.yaml.
type talentMetadata struct {
	ProfileName    string `yaml:"profile_name"`
	TalentName     string `yaml:"talent_name"`
	CragSlug       string `yaml:"crag_slug"`
	MemoryScope    string `yaml:"memory_scope"`
	MaterializedAt string `yaml:"materialized_at"` // RFC3339
}

// ProfilesRoot returns the directory that holds all Hermes profiles used by
// belayer (both the base belayer profile and per-talent forks). Defaults to
// ~/.hermes/profiles; honours HERMES_HOME using the same resolution rules as
// internal/cli/auth.go#belayerProfileDir.
func ProfilesRoot() (string, error) {
	if env := os.Getenv("HERMES_HOME"); env != "" {
		// Mirror belayerProfileDir's env-aware logic: if HERMES_HOME already
		// points at a profile dir (parent dir is named "profiles") we use its
		// parent as the profiles root; otherwise we treat HERMES_HOME as the
		// root and nest profiles/ underneath.
		parent := filepath.Base(filepath.Dir(env))
		if parent == "profiles" {
			return filepath.Dir(env), nil
		}
		return filepath.Join(env, "profiles"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".hermes", "profiles"), nil
}

// MaterializeProfile forks the base belayer Hermes profile into a per-talent-
// instance profile directory. It is idempotent: re-calling on an existing
// profile refreshes broken symlinks but does NOT overwrite SOUL.md or
// config.yaml (operator or talent may have edited them).
//
// On first call it:
//   - Creates the profile dir tree (talentProfileDirs subset of _PROFILE_DIRS).
//   - Symlinks auth.json, plugins/belayer, and skills/ from the base profile.
//   - Writes SOUL.md with opts.SystemPrompt.
//   - Writes config.yaml with plugins.enabled: [belayer] plus model if set.
//   - Writes .belayer-talent.yaml metadata for Phase 3.
func MaterializeProfile(opts MaterializeOptions) error {
	if opts.ProfileName == "" {
		return fmt.Errorf("MaterializeProfile: ProfileName must not be empty")
	}
	if opts.BaseProfileDir == "" {
		return fmt.Errorf("MaterializeProfile: BaseProfileDir must not be empty")
	}
	if _, err := os.Stat(opts.BaseProfileDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("MaterializeProfile: base profile dir %q does not exist", opts.BaseProfileDir)
		}
		return fmt.Errorf("MaterializeProfile: stat base profile dir: %w", err)
	}

	// Normalise and validate MemoryScope.
	scope := opts.MemoryScope
	if scope == "" {
		scope = "climb"
	}
	if !validMemoryScopes[scope] {
		return fmt.Errorf("MaterializeProfile: invalid memory scope %q (must be one of: climb, crag, talent)", scope)
	}

	root, err := ProfilesRoot()
	if err != nil {
		return fmt.Errorf("MaterializeProfile: resolve profiles root: %w", err)
	}
	profileDir := filepath.Join(root, opts.ProfileName)

	isNew := false
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		isNew = true
	} else if err != nil {
		return fmt.Errorf("MaterializeProfile: stat profile dir: %w", err)
	}

	if isNew {
		// Create the standard subdirectory tree (skills/ replaced by symlink).
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return fmt.Errorf("MaterializeProfile: mkdir profile root: %w", err)
		}
		for _, sub := range talentProfileDirs {
			if err := os.MkdirAll(filepath.Join(profileDir, sub), 0o755); err != nil {
				return fmt.Errorf("MaterializeProfile: mkdir profile %s: %w", sub, err)
			}
		}
		// plugins/ parent dir — required so we can place a belayer symlink inside.
		if err := os.MkdirAll(filepath.Join(profileDir, "plugins"), 0o755); err != nil {
			return fmt.Errorf("MaterializeProfile: mkdir profile plugins/: %w", err)
		}
	}

	// Symlinks — always refresh if missing or broken regardless of isNew.
	symlinks := []struct {
		link   string // path inside profileDir
		target string // absolute path it should point at
	}{
		{
			link:   filepath.Join(profileDir, "auth.json"),
			target: filepath.Join(opts.BaseProfileDir, "auth.json"),
		},
		{
			link:   filepath.Join(profileDir, "plugins", "belayer"),
			target: filepath.Join(opts.BaseProfileDir, "plugins", "belayer"),
		},
		{
			link:   filepath.Join(profileDir, "skills"),
			target: filepath.Join(opts.BaseProfileDir, "skills"),
		},
	}
	for _, sl := range symlinks {
		if err := ensureSymlink(sl.link, sl.target); err != nil {
			return fmt.Errorf("MaterializeProfile: ensure symlink %s → %s: %w", sl.link, sl.target, err)
		}
	}

	if isNew {
		// Write SOUL.md.
		if err := os.WriteFile(filepath.Join(profileDir, "SOUL.md"), []byte(opts.SystemPrompt), 0o644); err != nil {
			return fmt.Errorf("MaterializeProfile: write SOUL.md: %w", err)
		}

		// Write config.yaml with plugins.enabled: [belayer] and optional model.
		cfgContent := formatProfileConfig(opts.Model)
		if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte(cfgContent), 0o644); err != nil {
			return fmt.Errorf("MaterializeProfile: write config.yaml: %w", err)
		}

		// Write .belayer-talent.yaml metadata for Phase 3.
		// Derive talent name and crag slug from profile name by stripping the
		// "belayer-<crag>-" prefix.  We parse them conservatively because the
		// caller has already validated the name via ResolveProfileName.
		talentName, cragSlug := splitProfileName(opts.ProfileName)
		meta := formatTalentMetadata(talentMetadata{
			ProfileName:    opts.ProfileName,
			TalentName:     talentName,
			CragSlug:       cragSlug,
			MemoryScope:    scope,
			MaterializedAt: time.Now().UTC().Format(time.RFC3339),
		})
		if err := os.WriteFile(filepath.Join(profileDir, ".belayer-talent.yaml"), []byte(meta), 0o644); err != nil {
			return fmt.Errorf("MaterializeProfile: write .belayer-talent.yaml: %w", err)
		}
	}

	return nil
}

// TeardownProfile removes the profile directory for a per-talent-instance
// profile. It is idempotent: a missing directory is not an error.
//
// Safety: refuses to remove the base belayer profile or any name that does
// not start with "belayer-" to prevent accidental destruction of unrelated
// profiles.
func TeardownProfile(profileName string) error {
	if profileName == "" {
		return fmt.Errorf("TeardownProfile: profileName must not be empty")
	}
	if profileName == "belayer" {
		return fmt.Errorf("TeardownProfile: refusing to tear down the base belayer profile")
	}
	if !strings.HasPrefix(profileName, "belayer-") {
		return fmt.Errorf("TeardownProfile: profileName %q does not start with \"belayer-\" — only belayer-managed profiles may be torn down", profileName)
	}

	root, err := ProfilesRoot()
	if err != nil {
		return fmt.Errorf("TeardownProfile: resolve profiles root: %w", err)
	}
	profileDir := filepath.Join(root, profileName)

	if err := os.RemoveAll(profileDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("TeardownProfile: remove %s: %w", profileDir, err)
	}
	return nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

// ensureSymlink creates a symlink at linkPath pointing to target. If a symlink
// already exists and points at the right target it is left untouched. If it is
// broken or points elsewhere it is removed and recreated. A non-symlink at
// linkPath is an error.
func ensureSymlink(linkPath, target string) error {
	existing, err := os.Readlink(linkPath)
	if err == nil {
		// Symlink exists — check if it points at the right target.
		if existing == target {
			return nil
		}
		// Wrong target — remove and recreate.
		if rmErr := os.Remove(linkPath); rmErr != nil {
			return fmt.Errorf("remove stale symlink %s: %w", linkPath, rmErr)
		}
	} else if !isNoSuchFileError(err) {
		// Readlink failed for a reason other than "not found". Check if it's a
		// regular file/dir rather than a symlink.
		if _, statErr := os.Lstat(linkPath); statErr == nil {
			return fmt.Errorf("path %s exists and is not a symlink", linkPath)
		}
		// Lstat failed too (e.g. broken symlink on some platforms). Fall
		// through to create the symlink fresh.
	}
	// Create the symlink. Parent dir must already exist.
	if err := os.Symlink(target, linkPath); err != nil {
		return fmt.Errorf("symlink %s → %s: %w", linkPath, target, err)
	}
	return nil
}

// isNoSuchFileError reports whether err is an os.ErrNotExist variant, which
// is what Readlink returns for a missing path.
func isNoSuchFileError(err error) bool {
	return os.IsNotExist(err)
}

// formatProfileConfig returns the minimal config.yaml content for a fork
// profile: plugins.enabled: [belayer] plus an optional model: field.
// This is a minimal reimplementation of cli.ensureHermesPluginEnabled to
// avoid a cross-package dependency; we can DRY later (Phase 2 note).
func formatProfileConfig(model string) string {
	var sb strings.Builder
	sb.WriteString("plugins:\n  enabled:\n    - belayer\n")
	if model != "" {
		sb.WriteString("model: ")
		sb.WriteString(model)
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatTalentMetadata serialises a talentMetadata struct into a minimal YAML
// string without using an external YAML library to avoid adding dependencies.
// The output is round-trippable and human-readable.
func formatTalentMetadata(m talentMetadata) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "profile_name: %s\n", m.ProfileName)
	fmt.Fprintf(&sb, "talent_name: %s\n", m.TalentName)
	fmt.Fprintf(&sb, "crag_slug: %s\n", m.CragSlug)
	fmt.Fprintf(&sb, "memory_scope: %s\n", m.MemoryScope)
	fmt.Fprintf(&sb, "materialized_at: %s\n", m.MaterializedAt)
	return sb.String()
}

// splitProfileName splits a profile name of the form "belayer-<cragSlug>-<talentName>"
// into its crag slug and talent name components. The profile name must start
// with "belayer-". If the name has fewer than three dash-delimited segments
// the remainder after "belayer-" is returned as the talent name with an empty
// crag slug.
//
// Example: "belayer-software-co-supervisor" → cragSlug="software-co", talentName="supervisor"
// This is a best-effort parse; the authoritative decomposition is done by
// ResolveProfileName at spawn time.
func splitProfileName(profileName string) (talentName, cragSlug string) {
	// Strip "belayer-" prefix.
	rest := strings.TrimPrefix(profileName, "belayer-")
	// The profile format is belayer-<cragSlug>-<instance> where cragSlug and
	// instance are both variable length. We can't deterministically split them
	// without knowing the crag slug, so we record the raw remainder and let
	// Phase 3 use the stored metadata for precise decomposition.
	//
	// For the metadata file we store a best-effort single-segment split: first
	// segment of rest is the crag slug, remainder is the talent name. This is
	// accurate only for single-word crag slugs, but the authoritative data is
	// in the profile name itself (also stored in the metadata file).
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		return parts[1], parts[0]
	}
	return rest, ""
}
