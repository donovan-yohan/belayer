package shell

import "strings"

// Quote returns a shell-safe single-quoted string. This is the safest
// quoting method for bash — single quotes prevent all interpretation
// except for the single quote character itself, which is handled by
// ending the quote, adding an escaped single quote, and reopening.
//
// Example: "hello 'world'" → "'hello '\\''world'\\'''"
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	// Replace each ' with '\\'' (end quote, escaped literal quote, reopen quote)
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// QuoteEnvKey validates and returns an environment variable key.
// Only allows alphanumeric characters and underscores.
// Returns empty string if the key contains invalid characters.
func QuoteEnvKey(s string) string {
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return ""
		}
	}
	return s
}
