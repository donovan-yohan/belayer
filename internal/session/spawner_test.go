package session

import "testing"

func TestSpawnOpts_Fields(t *testing.T) {
	opts := SpawnOpts{
		NodeName: "reviewer",
		TaskID:   "task-123",
		Command:  "echo test",
		WorkDir:  "/tmp",
	}
	if opts.NodeName != "reviewer" {
		t.Errorf("NodeName = %q, want %q", opts.NodeName, "reviewer")
	}
	if opts.Command != "echo test" {
		t.Errorf("Command = %q, want %q", opts.Command, "echo test")
	}
}
