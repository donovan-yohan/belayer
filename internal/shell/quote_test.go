package shell_test

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/shell"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "'hello'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "string with spaces",
			input: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "string with single quote",
			input: "it's fine",
			want:  "'it'\"'\"'s fine'",
		},
		{
			name:  "string with multiple single quotes",
			input: "it's a cat's life",
			want:  "'it'\"'\"'s a cat'\"'\"'s life'",
		},
		{
			name:  "SQL injection attempt",
			input: "'; DROP TABLE users; --",
			want:  "''\"'\"'; DROP TABLE users; --'",
		},
		{
			name:  "command injection attempt",
			input: "$(rm -rf /)",
			want:  "'$(rm -rf /)'",
		},
		{
			name:  "backtick injection",
			input: "`id`",
			want:  "'`id`'",
		},
		{
			name:  "newline in input",
			input: "foo\nbar",
			want:  "'foo\nbar'",
		},
		{
			name:  "only single quotes",
			input: "'''",
			want:  "''" + `"'"` + "''" + `"'"` + "''" + `"'"` + "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shell.Quote(tt.input)
			if got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteEnvKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple key", input: "MY_VAR", want: "MY_VAR"},
		{name: "lowercase", input: "my_var", want: "my_var"},
		{name: "with digits", input: "VAR_1", want: "VAR_1"},
		{name: "leading digit stripped", input: "1VAR", want: "VAR"},
		{name: "special chars stripped", input: "MY-VAR", want: "MYVAR"},
		{name: "empty", input: "", want: ""},
		{name: "only digits", input: "123", want: "23"},
		{name: "injection attempt", input: "VAR;rm -rf /", want: "VARrmrf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shell.QuoteEnvKey(tt.input)
			if got != tt.want {
				t.Errorf("QuoteEnvKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
