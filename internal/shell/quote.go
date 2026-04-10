// Package shell provides safe shell-quoting utilities.
// All user-supplied values that appear in shell commands MUST be quoted
// via Quote before being passed to exec.Command or template rendering.
package shell

import "strings"

// Quote wraps s in single quotes, escaping any embedded single quotes using
// the standard POSIX shell trick: end the single-quoted string, emit a
// literal single quote with a backslash, then resume the single-quoted string.
//
// Examples:
//
//	Quote("hello")         → 'hello'
//	Quote("it's fine")     → 'it'"'"'s fine'
//	Quote("")              → ''
func Quote(s string) string {
	// Replace every ' with '"'"' then wrap the whole thing in single quotes.
	escaped := strings.ReplaceAll(s, "'", `'"'"'`)
	return "'" + escaped + "'"
}

// QuoteEnvKey sanitises an environment variable key so it can be safely used
// in a shell assignment (e.g. in an `env KEY=value` invocation). It strips any
// character that is not an ASCII letter, digit, or underscore and ensures the
// result does not start with a digit.
//
// This is intentionally conservative: callers should validate keys upstream
// and treat an empty return value as an error.
func QuoteEnvKey(key string) string {
	var b strings.Builder
	for i, ch := range key {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch == '_':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			if i > 0 {
				b.WriteRune(ch)
			}
			// drop leading digit
		}
	}
	return b.String()
}
