package cli

import (
	"testing"
)

// resolveLogLevelForRun replicates the precedence logic used by run start:
// flag wins, then BELAYER_LOG_LEVEL, then empty (let daemon pick).
func TestResolveRunLogLevel_Precedence(t *testing.T) {
	cases := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{"flag wins over env", "trace", "verbose", "trace"},
		{"env when flag empty", "", "verbose", "verbose"},
		{"empty when neither set", "", "", ""},
		{"flag alone", "trace", "", "trace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BELAYER_LOG_LEVEL", tc.env)
			got := resolveRunLogLevel(tc.flag)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
