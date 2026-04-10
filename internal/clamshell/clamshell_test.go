package clamshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCreateArgs(t *testing.T) {
	args, err := BuildCreateArgs(SandboxConfig{
		Name:       "belayer-test-pilot",
		PolicyPath: "/tmp/policy.yaml",
		Workspaces: []WorkspaceMount{{HostPath: "/host/session", Target: "/belayer/session"}, {HostPath: "/host/repo", Target: "/workspace"}},
		Command:    []string{"sh", "-lc", "echo hi"},
	})
	if err != nil {
		t.Fatalf("BuildCreateArgs returned error: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"sandbox create", "--name belayer-test-pilot", "--policy /tmp/policy.yaml", "--workspace /host/session:/belayer/session", "--workspace /host/repo:/workspace", "-- sh -lc echo hi"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected args to contain %q, got %q", want, joined)
		}
	}
}

func TestBuildCreateArgs_RequiresPolicy(t *testing.T) {
	if _, err := BuildCreateArgs(SandboxConfig{Name: "x"}); err == nil {
		t.Fatal("expected missing policy error")
	}
}

func TestConnectSandbox_UsesClamshellBinary(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "clamshell.log")
	script := filepath.Join(binDir, "clamshell")
	body := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake clamshell: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	if err := ConnectSandbox("belayer-test-pilot"); err != nil {
		t.Fatalf("ConnectSandbox returned error: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "sandbox connect --name belayer-test-pilot") {
		t.Fatalf("unexpected clamshell invocation: %s", string(data))
	}
}
