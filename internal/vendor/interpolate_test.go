package vendor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		vars   map[string]string
		want   string
	}{
		{
			name:   "single variable",
			prompt: "Implement %{INPUT}",
			vars:   map[string]string{"INPUT": "/path/to/design.md"},
			want:   "Implement /path/to/design.md",
		},
		{
			name:   "multiple variables",
			prompt: "Work in %{WORK_DIR} on %{INPUT}",
			vars:   map[string]string{"WORK_DIR": "/tmp/work", "INPUT": "design.md"},
			want:   "Work in /tmp/work on design.md",
		},
		{
			name:   "shell variables pass through",
			prompt: "$review the code at %{INPUT}",
			vars:   map[string]string{"INPUT": "/path/to/code"},
			want:   "$review the code at /path/to/code",
		},
		{
			name:   "skill invocations pass through",
			prompt: "/ship",
			vars:   map[string]string{"INPUT": "unused"},
			want:   "/ship",
		},
		{
			name:   "unknown belayer variables left untouched",
			prompt: "Use %{UNKNOWN} and %{INPUT}",
			vars:   map[string]string{"INPUT": "design.md"},
			want:   "Use %{UNKNOWN} and design.md",
		},
		{
			name:   "empty vars map",
			prompt: "Just a prompt with %{INPUT}",
			vars:   map[string]string{},
			want:   "Just a prompt with %{INPUT}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Interpolate(tt.prompt, tt.vars)
			if got != tt.want {
				t.Errorf("Interpolate(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

func TestResolvePromptRefs_Valid(t *testing.T) {
	workDir := t.TempDir()
	promptsDir := filepath.Join(workDir, ".belayer", "prompts")
	os.MkdirAll(promptsDir, 0o755)
	os.WriteFile(filepath.Join(promptsDir, "review.md"), []byte("You are a reviewer.\n\n%{INPUT}"), 0o644)

	got, err := ResolvePromptRefs("$review", workDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "You are a reviewer.\n\n%{INPUT}" {
		t.Errorf("got %q, want resolved content", got)
	}
}

func TestResolvePromptRefs_Missing(t *testing.T) {
	workDir := t.TempDir()

	got, err := ResolvePromptRefs("$unknown", workDir)
	if err == nil {
		t.Fatal("expected error for missing prompt ref")
	}
	if got != "$unknown" {
		t.Errorf("unresolvable ref should be left as-is, got %q", got)
	}
}

func TestResolvePromptRefs_NoRefs(t *testing.T) {
	got, err := ResolvePromptRefs("plain text prompt", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain text prompt" {
		t.Errorf("got %q, want pass-through", got)
	}
}
