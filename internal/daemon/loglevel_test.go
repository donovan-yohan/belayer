package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty returns standard", input: "", want: "standard"},
		{name: "standard is valid", input: "standard", want: "standard"},
		{name: "verbose is valid", input: "verbose", want: "verbose"},
		{name: "unknown returns error", input: "debug", wantErr: true},
		{name: "uppercase invalid", input: "Standard", wantErr: true},
		{name: "mixed case invalid", input: "VERBOSE", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateLogLevel(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateLogLevel(%q): expected error, got %q", tc.input, got)
				}
				if !strings.Contains(err.Error(), "standard") || !strings.Contains(err.Error(), "verbose") {
					t.Errorf("error should name allowed set, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateLogLevel(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ValidateLogLevel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDaemonNew_RejectsInvalidDefaultLogLevel(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Config{
		SocketPath:      filepath.Join(dir, "belayer.sock"),
		DBPath:          filepath.Join(dir, "belayer.db"),
		DefaultLogLevel: "debug",
	})
	if err == nil {
		t.Fatal("New with invalid DefaultLogLevel should fail fast")
	}
	if !strings.Contains(err.Error(), "DefaultLogLevel") {
		t.Errorf("error should mention DefaultLogLevel, got: %v", err)
	}
}

func TestDaemonNew_AcceptsEmptyDefaultLogLevel(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{
		SocketPath:      filepath.Join(dir, "belayer.sock"),
		DBPath:          filepath.Join(dir, "belayer.db"),
		DefaultLogLevel: "",
	})
	if err != nil {
		t.Fatalf("New with empty DefaultLogLevel failed: %v", err)
	}
	_ = d
}

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name          string
		explicit      string
		configDefault string
		want          string
		wantErr       bool
	}{
		{name: "all empty → standard", explicit: "", configDefault: "", want: "standard"},
		{name: "explicit wins over config", explicit: "verbose", configDefault: "standard", want: "verbose"},
		{name: "explicit wins when config empty", explicit: "standard", configDefault: "", want: "standard"},
		{name: "config default used when explicit empty", explicit: "", configDefault: "verbose", want: "verbose"},
		{name: "config default standard used when explicit empty", explicit: "", configDefault: "standard", want: "standard"},
		{name: "invalid explicit returns error", explicit: "debug", configDefault: "standard", wantErr: true},
		{name: "invalid configDefault returns error", explicit: "", configDefault: "debug", wantErr: true},
		{name: "explicit verbose overrides invalid config (explicit evaluated first)", explicit: "verbose", configDefault: "debug", want: "verbose"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveLogLevel(tc.explicit, tc.configDefault)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveLogLevel(%q, %q): expected error, got %q", tc.explicit, tc.configDefault, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveLogLevel(%q, %q): unexpected error: %v", tc.explicit, tc.configDefault, err)
			}
			if got != tc.want {
				t.Errorf("ResolveLogLevel(%q, %q) = %q, want %q", tc.explicit, tc.configDefault, got, tc.want)
			}
		})
	}
}
