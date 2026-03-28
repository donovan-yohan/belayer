package intake

import (
	"testing"
	"time"
)

func TestParsePollInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"30s", 30 * time.Second},
		{"", 5 * time.Minute},          // default
		{"invalid", 5 * time.Minute},   // default on error
		{"168h", 168 * time.Hour},      // weekly
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parsePollInterval(tt.input)
			if got != tt.expected {
				t.Errorf("parsePollInterval(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
