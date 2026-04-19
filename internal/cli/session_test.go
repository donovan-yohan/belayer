package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestStatusCmd_IncludesLogColumn verifies that `belayer status` prints a LOG
// column and surfaces the session's log_level value in the row.
//
// Test-plan item: T1.5 — `belayer status` adds LOG column.
func TestStatusCmd_IncludesLogColumn(t *testing.T) {
	sock := startTestDaemon(t)
	c := NewClient(sock)

	// Create a session at log_level=trace so the column value is unambiguous.
	_, err := c.CreateSession("status-log-test", "nightshift", nil, "", "trace")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"status", "--socket", sock})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute status: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "LOG") {
		t.Fatalf("missing LOG column header in status output:\n%s", out)
	}
	if !strings.Contains(out, "trace") {
		t.Fatalf("missing 'trace' value in status row:\n%s", out)
	}
}
