package daemon

import (
	"strings"
	"testing"
)

func TestResolveProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cragSlug   string
		talentName string
		instanceID string
		want       string
		wantErrSub string // non-empty → expect error containing this substring
	}{
		{
			name:       "singleton supervisor in software-co crag",
			cragSlug:   "software-co",
			talentName: "supervisor",
			instanceID: "",
			want:       "belayer-software-co-supervisor",
		},
		{
			name:       "parallel main with hash suffix",
			cragSlug:   "foo",
			talentName: "backend-dev",
			instanceID: "a3f9c2d1",
			want:       "belayer-foo-backend-dev-a3f9c2d1",
		},
		{
			name:       "generated talent instanceID ignored",
			cragSlug:   "foo",
			talentName: "generated-reviewer-1",
			instanceID: "ignored-hash",
			want:       "belayer-foo-generated-reviewer-1",
		},
		{
			name:       "generated talent no instanceID",
			cragSlug:   "foo",
			talentName: "generated-reviewer-1",
			instanceID: "",
			want:       "belayer-foo-generated-reviewer-1",
		},
		{
			name:       "singleton web-dev",
			cragSlug:   "myproject",
			talentName: "web-dev",
			instanceID: "",
			want:       "belayer-myproject-web-dev",
		},
		{
			name:       "empty cragSlug returns error",
			cragSlug:   "",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "cragSlug must not be empty",
		},
		{
			name:       "empty talentName returns error",
			cragSlug:   "foo",
			talentName: "",
			instanceID: "",
			wantErrSub: "talentName must not be empty",
		},
		{
			name:       "oversize result (long crag + long talent + hash) returns error mentioning total length",
			cragSlug:   "twenty-char-cragslug",          // 20 chars
			talentName: "thirty-five-char-talent-name-xx", // 32 chars to push over limit
			instanceID: "a3f9c2d1",                       // 8 chars → total: 8+20+1+32+1+8 = 70 > 64
			wantErrSub: "characters (max 64)",
		},
		{
			name:       "crag with uppercase rejected",
			cragSlug:   "Software-Co",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "invalid",
		},
		{
			name:       "crag with dot rejected",
			cragSlug:   "my.crag",
			talentName: "supervisor",
			instanceID: "",
			wantErrSub: "invalid",
		},
		{
			name:       "generated talent with regex-incompatible chars returns error",
			cragSlug:   "foo",
			talentName: "generated-Reviewer.1",
			instanceID: "",
			wantErrSub: "invalid",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveProfileName(tc.cragSlug, tc.talentName, tc.instanceID)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("ResolveProfileName(%q, %q, %q) = %q, want error containing %q",
						tc.cragSlug, tc.talentName, tc.instanceID, got, tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProfileName(%q, %q, %q) unexpected error: %v",
					tc.cragSlug, tc.talentName, tc.instanceID, err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantErrSub string // non-empty → expect error containing this substring
	}{
		// Valid cases.
		{name: "bare belayer", input: "belayer"},
		{name: "scoped profile", input: "belayer-foo-bar"},
		{name: "two alnum chars", input: "a1"},
		{name: "hyphens and underscores", input: "a-b_c-d"},
		{name: "all lowercase digits", input: "0abc123"},
		{name: "exactly 64 chars", input: strings.Repeat("a", 64)},

		// Invalid cases.
		{
			name:       "empty string",
			input:      "",
			wantErrSub: "must not be empty",
		},
		{
			name:       "starts with hyphen",
			input:      "-belayer",
			wantErrSub: "must start with",
		},
		{
			name:       "starts with underscore",
			input:      "_belayer",
			wantErrSub: "must start with",
		},
		{
			name:       "has uppercase",
			input:      "Belayer",
			wantErrSub: "invalid",
		},
		{
			name:       "has dot",
			input:      "bel.ayer",
			wantErrSub: "invalid",
		},
		{
			name:       "has slash",
			input:      "bel/ayer",
			wantErrSub: "invalid",
		},
		{
			name:       "exceeds 64 chars",
			input:      strings.Repeat("a", 65),
			wantErrSub: "exceeds 64 characters",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProfileName(tc.input)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("ValidateProfileName(%q) = nil, want error containing %q", tc.input, tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateProfileName(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestDeriveInstanceID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seed string
	}{
		{name: "uuid-style seed", seed: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "short seed", seed: "abc"},
		{name: "another seed", seed: "xyz123"},
	}

	t.Run("empty seed returns empty string", func(t *testing.T) {
		t.Parallel()
		if got := DeriveInstanceID(""); got != "" {
			t.Errorf("DeriveInstanceID(\"\") = %q, want \"\"", got)
		}
	})

	t.Run("output is exactly 8 lowercase hex chars", func(t *testing.T) {
		t.Parallel()
		for _, tc := range tests {
			got := DeriveInstanceID(tc.seed)
			if len(got) != 8 {
				t.Errorf("DeriveInstanceID(%q) = %q, want 8 chars", tc.seed, got)
			}
			for _, ch := range got {
				if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
					t.Errorf("DeriveInstanceID(%q) = %q contains non-lowercase-hex char %q", tc.seed, got, ch)
				}
			}
		}
	})

	t.Run("deterministic: same seed same output", func(t *testing.T) {
		t.Parallel()
		for _, tc := range tests {
			a := DeriveInstanceID(tc.seed)
			b := DeriveInstanceID(tc.seed)
			if a != b {
				t.Errorf("DeriveInstanceID(%q) not deterministic: %q != %q", tc.seed, a, b)
			}
		}
	})

	t.Run("different seeds produce different outputs", func(t *testing.T) {
		t.Parallel()
		seen := make(map[string]string)
		for _, tc := range tests {
			got := DeriveInstanceID(tc.seed)
			for prevSeed, prevID := range seen {
				if got == prevID {
					t.Errorf("DeriveInstanceID(%q) == DeriveInstanceID(%q) = %q (collision)", tc.seed, prevSeed, got)
				}
			}
			seen[tc.seed] = got
		}
	})
}
