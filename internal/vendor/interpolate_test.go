package vendor

import "testing"

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
