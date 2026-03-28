package session

import "testing"

func TestInternalDir(t *testing.T) {
	got := InternalDir("/tmp/work")
	want := "/tmp/work/.belayer/.internal"
	if got != want {
		t.Errorf("InternalDir = %q, want %q", got, want)
	}
}

func TestCompletionFilePath(t *testing.T) {
	got := CompletionFilePath("/tmp/work", "climb-123", "lead", 2)
	want := "/tmp/work/.belayer/.internal/completion/climb-123-lead-attempt-2.json"
	if got != want {
		t.Errorf("CompletionFilePath = %q, want %q", got, want)
	}
}

func TestInputDir(t *testing.T) {
	got := InputDir("/tmp/work")
	want := "/tmp/work/.belayer/.internal/input"
	if got != want {
		t.Errorf("InputDir = %q, want %q", got, want)
	}
}

func TestOutputDir(t *testing.T) {
	got := OutputDir("/tmp/work")
	want := "/tmp/work/.belayer/.internal/output"
	if got != want {
		t.Errorf("OutputDir = %q, want %q", got, want)
	}
}
