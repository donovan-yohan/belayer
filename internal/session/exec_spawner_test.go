package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecSpawner_SpawnSuccess(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "true",
		WorkDir:  os.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	if exitCh == nil {
		t.Fatal("expected non-nil exit channel")
	}
	select {
	case result := <-exitCh:
		if result.Error != nil {
			t.Fatalf("expected nil error from exit channel, got: %v", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_EmptyCommand(t *testing.T) {
	spawner := &ExecSpawner{}
	_, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "",
		WorkDir:  os.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecSpawner_NonZeroExit(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "exit 1",
		WorkDir:  os.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case result := <-exitCh:
		if result.Error == nil {
			t.Fatal("expected error from exit channel for exit 1")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_EnvVars(t *testing.T) {
	dir := t.TempDir()
	outFile := dir + "/env.txt"
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "mynode",
		TaskID:   "task-42",
		Attempt:  3,
		Command:  "env > " + outFile,
		WorkDir:  dir,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	<-exitCh
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read env output: %v", err)
	}
	env := string(data)
	for _, want := range []string{"BELAYER_TASK_ID=task-42", "BELAYER_NODE=mynode", "BELAYER_ATTEMPT=3", "BELAYER_WORK_DIR=" + dir} {
		if !strings.Contains(env, want) {
			t.Errorf("missing env var %q in output", want)
		}
	}
}

func TestExecSpawner_CaptureStdout(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName:      "test",
		TaskID:        "t1",
		Command:       "echo hello",
		WorkDir:       os.TempDir(),
		CaptureStdout: true,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case result := <-exitCh:
		if result.Error != nil {
			t.Fatalf("expected nil error, got: %v", result.Error)
		}
		if !strings.Contains(string(result.Stdout), "hello") {
			t.Errorf("expected stdout to contain 'hello', got: %q", result.Stdout)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_NoCaptureStdout(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName:      "test",
		TaskID:        "t1",
		Command:       "echo hello",
		WorkDir:       os.TempDir(),
		CaptureStdout: false,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case result := <-exitCh:
		if result.Stdout != nil {
			t.Errorf("expected nil stdout when CaptureStdout=false, got %d bytes", len(result.Stdout))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}

func TestExecSpawner_StderrAlwaysCaptured(t *testing.T) {
	spawner := &ExecSpawner{}
	exitCh, err := spawner.Spawn(context.Background(), SpawnOpts{
		NodeName: "test",
		TaskID:   "t1",
		Command:  "echo errout >&2",
		WorkDir:  os.TempDir(),
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	select {
	case result := <-exitCh:
		if !strings.Contains(string(result.Stderr), "errout") {
			t.Errorf("expected stderr to contain 'errout', got: %q", result.Stderr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process exit")
	}
}
