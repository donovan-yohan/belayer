package daemon

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
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
