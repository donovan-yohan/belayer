package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStartCreatesSessionSpawnsPlannerAndDeliversInitialTask(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := installFakeTmux(t)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	repoDir := filepath.Join(t.TempDir(), "belayer-repo")
	initGitRepo(t, repoDir, "belayer")

	out := runRootCmd(t,
		"run", "start",
		"--socket", socketPath,
		"--name", "nightshift-smoke",
		"--task", "Investigate a single-repo change",
		"--planner-profile", "default",
		"--workdir", repoDir,
	)
	for _, want := range []string{"Run started:", "planner", "belayer roster"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	client := NewClient(socketPath)
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Status != "running" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	runs, err := client.ListAgents(sessions[0].ID)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(runs) != 1 || runs[0].Name != "planner" {
		t.Fatalf("unexpected agent runs: %#v", runs)
	}
	if runs[0].Status != "running" && runs[0].Status != "blocked" {
		t.Fatalf("expected planner status running or blocked in fake-tmux environment, got %#v", runs)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "new-session -d -s belayer-"+sessions[0].ID+"-planner") {
		t.Fatalf("expected planner tmux session in log, got:\n%s", logText)
	}
	if !strings.Contains(logText, "send-keys -t belayer-"+sessions[0].ID+"-planner -l Investigate a single-repo change") {
		t.Fatalf("expected initial task delivery in tmux log, got:\n%s", logText)
	}
}
