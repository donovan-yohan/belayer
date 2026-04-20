package daemon

import (
	"net/http"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/store"
)

func TestValidateAgentName_Valid(t *testing.T) {
	for _, name := range []string{
		"supervisor",
		"reviewer-1",
		"pm",
		"impl_a",
		"a.b.c",
		"reviewer.7",
	} {
		if err := validateAgentName(name); err != nil {
			t.Errorf("validateAgentName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateAgentName_Rejects(t *testing.T) {
	cases := []struct {
		name       string
		wantSubstr string
	}{
		{"", "empty"},
		{".", "reserved"},
		{"..", "reserved"},
		{".hidden", "start with"},
		{"a/b", "separator"},
		{`a\b`, "separator"},
		{"../escape", "start with"},
		{"foo/../bar", "separator"},
		{"foo..bar", "'..'"},
		{"has\x00nul", "NUL"},
	}
	for _, tc := range cases {
		err := validateAgentName(tc.name)
		if err == nil {
			t.Errorf("validateAgentName(%q) = nil, want error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Errorf("validateAgentName(%q) error = %q, want substring %q", tc.name, err.Error(), tc.wantSubstr)
		}
	}
}

// TestSpawnAgent_RejectsIdentityTraversal verifies the HTTP handler rejects
// a spawn request whose Identity field contains path traversal. Identity is
// resolved by agentIdentityPaths to pick .belayer/agents/<identity>/ and
// agents/<identity>/ — an unvalidated "../etc" would escape that tree and
// read arbitrary prompt/config files from outside the identity root.
//
// Addresses CodeRabbit critical: req.Identity was not validated like req.Name.
func TestSpawnAgent_RejectsIdentityTraversal(t *testing.T) {
	d := testDaemon(t)
	// testDaemon already registers POST /sessions/{id}/agents.

	sess, err := d.store.CreateSession(store.Session{Name: "identity-traversal-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, bad := range []string{"../escape", "a/b", ".hidden", ".."} {
		rr := doRequest(t, d, "POST", "/sessions/"+sess+"/agents", agentSpawnRequest{
			Name:     "worker",
			Role:     "worker",
			Profile:  "default",
			Identity: bad,
		})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("Identity=%q: got %d, want 400: %s", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "identity") {
			t.Errorf("Identity=%q: error body %q missing 'identity'", bad, rr.Body.String())
		}
	}
}

func TestTranslateHostPathToContainer(t *testing.T) {
	cases := []struct {
		hostPath      string
		hostWorkspace string
		want          string
	}{
		{"", "/host/ws", ""},
		{"/host/ws/.belayer/runs/s/transcripts/a.jsonl", "/host/ws", "/workspace/.belayer/runs/s/transcripts/a.jsonl"},
		{"/host/ws", "/host/ws", "/workspace"},
		{"/other/path", "/host/ws", "/other/path"},
		{"relative/path", "/host/ws", "relative/path"},
		{"/host/ws/file", "", "/host/ws/file"},
	}
	for _, tc := range cases {
		got := translateHostPathToContainer(tc.hostPath, tc.hostWorkspace)
		if got != tc.want {
			t.Errorf("translateHostPathToContainer(%q, %q) = %q, want %q", tc.hostPath, tc.hostWorkspace, got, tc.want)
		}
	}
}
