package main

import (
	"testing"
)

// TestParseWriteRoots exercises the env-parse helper: split on colon, skip
// empty entries. The Landlock syscall path is intentionally not exercised here
// because tests run on darwin CI where the kernel feature is absent.
func TestParseWriteRoots(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want []string
	}{
		{
			name: "empty env",
			env:  "",
			want: nil,
		},
		{
			name: "single path",
			env:  "/workspace",
			want: []string{"/workspace"},
		},
		{
			name: "two paths",
			env:  "/workspace:/tmp",
			want: []string{"/workspace", "/tmp"},
		},
		{
			name: "trailing colon skipped",
			env:  "/workspace:/tmp:",
			want: []string{"/workspace", "/tmp"},
		},
		{
			name: "leading colon skipped",
			env:  ":/workspace:/tmp",
			want: []string{"/workspace", "/tmp"},
		},
		{
			name: "interior empty segment skipped",
			env:  "/a::/b",
			want: []string{"/a", "/b"},
		},
		{
			name: "multiple paths with run dir",
			env:  "/workspace/.belayer/worktrees/sess/agent:/workspace/.belayer/runs/sess/agent:/tmp",
			want: []string{
				"/workspace/.belayer/worktrees/sess/agent",
				"/workspace/.belayer/runs/sess/agent",
				"/tmp",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWriteRoots(tc.env)
			if len(got) != len(tc.want) {
				t.Fatalf("parseWriteRoots(%q) = %v (len %d), want %v (len %d)",
					tc.env, got, len(got), tc.want, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("parseWriteRoots(%q)[%d] = %q, want %q", tc.env, i, got[i], tc.want[i])
				}
			}
		})
	}
}
