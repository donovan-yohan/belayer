package daemon

import (
	"regexp"
	"strings"
)

var (
	// keyRegex matches JSON fields whose key is a common secret name.
	keyRegex = regexp.MustCompile(`(?i)"(api[_-]?key|authorization|bearer|password|secret|token|credential)"\s*:\s*"[^"]*"`)
	// openaiRegex matches OpenAI-style sk-... tokens even outside JSON.
	openaiRegex = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)
	// bearerRegex matches free-form Bearer tokens in header-like strings.
	bearerRegex = regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)
)

// Scrub returns s with common secret material replaced by "<redacted>".
// It is intentionally conservative: it only rewrites matches for known key
// names and well-known token prefixes so non-secret fields pass through
// unchanged. Safe to call on arbitrary strings (JSON, plain text, logs).
func Scrub(s string) string {
	s = keyRegex.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.Index(m, ":")
		if i < 0 {
			return m
		}
		return m[:i+1] + `"<redacted>"`
	})
	s = openaiRegex.ReplaceAllString(s, "<redacted>")
	s = bearerRegex.ReplaceAllString(s, "Bearer <redacted>")
	return s
}
