package runtime

import "testing"

func TestSelect(t *testing.T) {
	if got := Select(false).Mode(); got != ModeLocal {
		t.Fatalf("Select(false).Mode() = %q, want %q", got, ModeLocal)
	}
	if got := Select(true).Mode(); got != ModeDocker {
		t.Fatalf("Select(true).Mode() = %q, want %q", got, ModeDocker)
	}
}
