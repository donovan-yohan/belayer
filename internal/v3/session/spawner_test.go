package session

import "testing"

func TestWindowName_TruncatesTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "reviewer", TaskID: "abcdef1234567890"}
	got := opts.WindowName()
	want := "reviewer-abcdef12"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}

func TestWindowName_ShortTaskID(t *testing.T) {
	opts := SpawnOpts{NodeName: "planner", TaskID: "abc"}
	got := opts.WindowName()
	want := "planner-abc"
	if got != want {
		t.Errorf("WindowName = %q, want %q", got, want)
	}
}
