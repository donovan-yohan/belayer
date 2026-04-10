package runtime

import "testing"

func TestSelect(t *testing.T) {
	if got := Select(false, false).Mode(); got != ModeLocal {
		t.Fatalf("Select(false, false).Mode() = %q, want %q", got, ModeLocal)
	}
	if got := Select(true, false).Mode(); got != ModeDocker {
		t.Fatalf("Select(true, false).Mode() = %q, want %q", got, ModeDocker)
	}
	if got := Select(true, true).Mode(); got != ModeClamshell {
		t.Fatalf("Select(true, true).Mode() = %q, want %q", got, ModeClamshell)
	}
}
