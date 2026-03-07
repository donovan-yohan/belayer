package tmux

import (
	"fmt"
	"os/exec"
	"testing"
	"time"
)

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

func testSessionName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("belayer-test-%d", time.Now().UnixNano())
}

func TestRealTmux_SessionLifecycle(t *testing.T) {
	skipIfNoTmux(t)

	tm := NewRealTmux()
	session := testSessionName(t)

	// Ensure cleanup even if test fails.
	t.Cleanup(func() {
		_ = tm.KillSession(session)
	})

	// Session should not exist yet.
	if tm.HasSession(session) {
		t.Fatalf("session %q should not exist before creation", session)
	}

	// Create session.
	if err := tm.NewSession(session); err != nil {
		t.Fatalf("NewSession(%q): %v", session, err)
	}

	// Session should now exist.
	if !tm.HasSession(session) {
		t.Fatalf("session %q should exist after creation", session)
	}

	// Create a new window.
	windowName := "test-win"
	if err := tm.NewWindow(session, windowName); err != nil {
		t.Fatalf("NewWindow(%q, %q): %v", session, windowName, err)
	}

	// ListWindows should contain the new window.
	windows, err := tm.ListWindows(session)
	if err != nil {
		t.Fatalf("ListWindows(%q): %v", session, err)
	}
	found := false
	for _, w := range windows {
		if w == windowName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListWindows(%q) = %v, want it to contain %q", session, windows, windowName)
	}

	// SendKeys to the window (just echo something harmless).
	if err := tm.SendKeys(session, windowName, "echo belayer-tmux-test"); err != nil {
		t.Fatalf("SendKeys(%q, %q, ...): %v", session, windowName, err)
	}

	// PipePane to a temp log file.
	logPath := t.TempDir() + "/tmux-test.log"
	if err := tm.PipePane(session, windowName, logPath); err != nil {
		t.Fatalf("PipePane(%q, %q, %q): %v", session, windowName, logPath, err)
	}

	// Kill the window.
	if err := tm.KillWindow(session, windowName); err != nil {
		t.Fatalf("KillWindow(%q, %q): %v", session, windowName, err)
	}

	// Kill the session.
	if err := tm.KillSession(session); err != nil {
		t.Fatalf("KillSession(%q): %v", session, err)
	}

	// Session should no longer exist.
	if tm.HasSession(session) {
		t.Fatalf("session %q should not exist after kill", session)
	}
}

func TestRealTmux_NewSession_Duplicate(t *testing.T) {
	skipIfNoTmux(t)

	tm := NewRealTmux()
	session := testSessionName(t)

	t.Cleanup(func() {
		_ = tm.KillSession(session)
	})

	if err := tm.NewSession(session); err != nil {
		t.Fatalf("first NewSession(%q): %v", session, err)
	}

	// Creating a duplicate session should fail.
	if err := tm.NewSession(session); err == nil {
		t.Errorf("expected error when creating duplicate session %q", session)
	}
}

func TestRealTmux_KillSession_NonExistent(t *testing.T) {
	skipIfNoTmux(t)

	tm := NewRealTmux()

	// Killing a non-existent session should return an error.
	if err := tm.KillSession("belayer-nonexistent-session"); err == nil {
		t.Error("expected error when killing non-existent session")
	}
}
