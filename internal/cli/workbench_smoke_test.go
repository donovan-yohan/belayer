package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/docker"
)

func installFakeDockerForCLIWorkbenchTests(t *testing.T, statusesJSON string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	body := `#!/bin/sh
set -eu
if [ "$1" = "compose" ] && [ "$4" = "up" ]; then
  exit 0
fi
if [ "$1" = "compose" ] && [ "$4" = "ps" ]; then
  printf '%s' '` + statusesJSON + `'
  exit 0
fi
if [ "$1" = "compose" ] && [ "$4" = "down" ]; then
  exit 0
fi
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func requireUnixSocketSupport(t *testing.T) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "probe.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("unix sockets unavailable in this environment: %v", err)
		return
	}
	_ = ln.Close()
	_ = os.Remove(socketPath)
}

func TestWorkbenchLifecycleCommands(t *testing.T) {
	requireUnixSocketSupport(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeDockerForCLIWorkbenchTests(t, `[{"name":"extend-api","state":"running","health":"healthy"}]`)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	client := NewClient(socketPath)
	sess, err := client.CreateSession("workbench-smoke", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sandboxPath := filepath.Join(home, ".belayer", "sandboxes", sess.ID)
	if err := os.MkdirAll(sandboxPath, 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandboxPath, "docker-compose.yml"), []byte("version: '3.9'"), 0o600); err != nil {
		t.Fatalf("write sandbox compose: %v", err)
	}
	if err := docker.WriteRuntimeMetadata(sandboxPath, docker.RuntimeMetadata{
		SessionID: sess.ID,
		Workbench: &docker.WorkbenchConfigSpec{
			Timeout: "1s",
			Services: []docker.ServiceDecl{
				{Name: "extend-api", Image: "example/api:latest", Ports: []string{"8080"}},
			},
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeMetadata: %v", err)
	}

	upOut := runRootCmd(t, "workbench", "up", "--socket", socketPath, "--session", sess.ID)
	for _, want := range []string{
		"Status: ready",
		"Endpoints:",
		"extend-api: http://extend-api:8080",
		"Services:",
		"extend-api: running/healthy",
	} {
		if !strings.Contains(upOut, want) {
			t.Fatalf("expected up output to contain %q, got:\n%s", want, upOut)
		}
	}

	statusOut := runRootCmd(t, "workbench", "status", "--socket", socketPath, "--session", sess.ID)
	if !strings.Contains(statusOut, "extend-api: http://extend-api:8080") {
		t.Fatalf("expected status output to include endpoint, got:\n%s", statusOut)
	}

	downOut := runRootCmd(t, "workbench", "down", "--socket", socketPath, "--session", sess.ID)
	if !strings.Contains(downOut, "Workbench torn down for session") {
		t.Fatalf("unexpected down output:\n%s", downOut)
	}
}

func TestWorkbenchUpUsesBELAYERSessionID(t *testing.T) {
	requireUnixSocketSupport(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeDockerForCLIWorkbenchTests(t, `[{"name":"extend-api","state":"running","health":"healthy"}]`)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	client := NewClient(socketPath)
	sess, err := client.CreateSession("workbench-env", "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sandboxPath := filepath.Join(home, ".belayer", "sandboxes", sess.ID)
	if err := os.MkdirAll(sandboxPath, 0o700); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandboxPath, "docker-compose.yml"), []byte("version: '3.9'"), 0o600); err != nil {
		t.Fatalf("write sandbox compose: %v", err)
	}
	if err := docker.WriteRuntimeMetadata(sandboxPath, docker.RuntimeMetadata{
		SessionID: sess.ID,
		Workbench: &docker.WorkbenchConfigSpec{
			Timeout: "1s",
			Services: []docker.ServiceDecl{
				{Name: "extend-api", Image: "example/api:latest", Ports: []string{"8080"}},
			},
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeMetadata: %v", err)
	}

	t.Setenv("BELAYER_SESSION_ID", sess.ID)
	upOut := runRootCmd(t, "workbench", "up", "--socket", socketPath)
	if !strings.Contains(upOut, "Status: ready") {
		t.Fatalf("expected env-based workbench up to succeed, got:\n%s", upOut)
	}
}
