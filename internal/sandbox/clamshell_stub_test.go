//go:build !clamshell

package sandbox_test

import (
	"context"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/sandbox"
)

// TestClamshellStubReportsUnavailable verifies the default-build stub returns
// a clear "not built with -tags clamshell" error from every Driver method.
// Under -tags clamshell this file is not compiled in — the real driver's
// behavior is exercised by clamshell_test.go instead.
func TestClamshellStubReportsUnavailable(t *testing.T) {
	d, err := sandbox.Default.Get("clamshell")
	if err != nil {
		t.Fatalf("Default.Get(\"clamshell\"): %v", err)
	}

	_, createErr := d.Create(context.Background(), sandbox.Config{Name: "sess"})
	if createErr == nil {
		t.Fatal("stub Create returned nil error; expected unavailable error")
	}
	if !strings.Contains(createErr.Error(), "-tags clamshell") {
		t.Errorf("stub Create error %q does not mention -tags clamshell", createErr.Error())
	}

	_, execErr := d.Exec(context.Background(), sandbox.Handle{}, []string{"echo"}, sandbox.ExecOpts{})
	if execErr == nil || !strings.Contains(execErr.Error(), "-tags clamshell") {
		t.Errorf("stub Exec error %v does not mention -tags clamshell", execErr)
	}

	stopErr := d.Stop(context.Background(), sandbox.Handle{})
	if stopErr == nil || !strings.Contains(stopErr.Error(), "-tags clamshell") {
		t.Errorf("stub Stop error %v does not mention -tags clamshell", stopErr)
	}
}
