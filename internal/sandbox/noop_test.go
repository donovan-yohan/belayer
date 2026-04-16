package sandbox_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/sandbox"
)

func TestNoopCreate(t *testing.T) {
	var d sandbox.Noop
	h, err := d.Create(context.Background(), sandbox.Config{Name: "test-session"})
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if h.ID != "test-session" {
		t.Errorf("expected handle ID %q, got %q", "test-session", h.ID)
	}
}

func TestNoopStop(t *testing.T) {
	var d sandbox.Noop
	h := sandbox.Handle{ID: "test-session"}
	if err := d.Stop(context.Background(), h); err != nil {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}

func TestNoopExecSimpleCommand(t *testing.T) {
	var d sandbox.Noop
	h := sandbox.Handle{ID: "test-session"}

	var stdout bytes.Buffer
	proc, err := d.Exec(context.Background(), h, []string{"echo", "hello"}, sandbox.ExecOpts{
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}

	if err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned unexpected error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "hello" {
		t.Errorf("expected stdout %q, got %q", "hello", got)
	}
}

func TestNoopExecEnvVars(t *testing.T) {
	var d sandbox.Noop
	h := sandbox.Handle{ID: "test-session"}

	var stdout bytes.Buffer
	proc, err := d.Exec(context.Background(), h, []string{"env"}, sandbox.ExecOpts{
		Env:    []string{"BELAYER_TEST_VAR=sandwich"},
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}

	if err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "BELAYER_TEST_VAR=sandwich") {
		t.Errorf("expected env output to contain BELAYER_TEST_VAR=sandwich, got:\n%s", stdout.String())
	}
}

func TestNoopExecWorkingDirectory(t *testing.T) {
	var d sandbox.Noop
	h := sandbox.Handle{ID: "test-session"}

	dir := t.TempDir()

	var stdout bytes.Buffer
	proc, err := d.Exec(context.Background(), h, []string{"pwd"}, sandbox.ExecOpts{
		Dir:    dir,
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("Exec returned unexpected error: %v", err)
	}

	if err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned unexpected error: %v", err)
	}

	// pwd may differ from dir due to symlinks (e.g. macOS /var -> /private/var),
	// so compare via os.SameFile rather than string equality.
	got := strings.TrimSpace(stdout.String())
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat pwd output %q: %v", got, err)
	}
	wantInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat expected dir %q: %v", dir, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("expected working directory %q, got %q", dir, got)
	}
}

func TestNoopExecEmptyCommandReturnsError(t *testing.T) {
	var d sandbox.Noop
	h := sandbox.Handle{ID: "test-session"}

	_, err := d.Exec(context.Background(), h, []string{}, sandbox.ExecOpts{})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}
