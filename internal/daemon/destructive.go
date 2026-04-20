package daemon

import "regexp"

// DetectDestructive reports whether cmd matches a known destructive shell
// pattern. It returns the matched kind (e.g. "rm-rf") and ok=true on a match.
//
// Detection is whole-line regex applied to the raw command string. This is
// intentionally simple: we do NOT attempt to parse shell quoting, so a command
// like `echo 'rm -rf /'` will be flagged as "rm-rf". That is an accepted
// false-positive trade-off; the flag surfaces a warning, not a hard block.
// Document the limitation here so future readers understand the boundary.
//
// Patterns are case-insensitive where the commands themselves are
// case-insensitive (SQL keywords, git sub-commands). rm/dd are lowercase-only
// on Unix.
var destructivePatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	// rm -rf / rm -Rf / rm -fr / rm -fR — any two-char flag combo with r (or R) and f.
	// Requires the flags to be in a single token starting with '-'.
	// Anchored to a word boundary so "form" doesn't match.
	{
		kind: "rm-rf",
		re:   regexp.MustCompile(`(?i)\brm\s+-[rRfF]*[rR][rRfF]*\b`),
	},
	// git reset --hard
	{
		kind: "git-reset-hard",
		re:   regexp.MustCompile(`(?i)\bgit\s+reset\s+--hard\b`),
	},
	// git push --force / git push -f / git push --force-with-lease
	{
		kind: "git-force-push",
		re:   regexp.MustCompile(`(?i)\bgit\s+push\b.*\s(--force|--force-with-lease|-f)\b`),
	},
	// git clean -f / -fd / -fx / -fX (any combo starting with -f containing d/x/X)
	{
		kind: "git-clean",
		re:   regexp.MustCompile(`(?i)\bgit\s+clean\s+-[a-zA-Z]*f[a-zA-Z]*\b`),
	},
	// SQL: DROP TABLE / DROP DATABASE / TRUNCATE TABLE (case-insensitive)
	{
		kind: "sql-drop",
		re:   regexp.MustCompile(`(?i)\b(DROP\s+(TABLE|DATABASE)|TRUNCATE\s+TABLE)\b`),
	},
	// dd with of=/dev/ — writing directly to a device node
	{
		kind: "dd-to-device",
		re:   regexp.MustCompile(`\bdd\b.*\bof=/dev/`),
	},
}

// DetectDestructive checks cmd against all destructive patterns and returns
// the first matched kind and ok=true. Returns ("", false) if no pattern matches.
func DetectDestructive(cmd string) (kind string, ok bool) {
	for _, p := range destructivePatterns {
		if p.re.MatchString(cmd) {
			return p.kind, true
		}
	}
	return "", false
}
