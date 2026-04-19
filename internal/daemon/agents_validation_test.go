package daemon

import (
	"strings"
	"testing"
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
