package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	daemonpkg "github.com/donovan-yohan/belayer/internal/daemon"
)

func installFakeTmux(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	body := `#!/bin/sh
set -eu
log_dir="${BELAYER_FAKE_TMUX_DIR:-/tmp}"
mkdir -p "$log_dir"
printf '%s\n' "$*" >> "$log_dir/tmux.log"
case "$1" in
  new-session|kill-session|send-keys|pipe-pane)
    exit 0
    ;;
  list-sessions)
    exit 0
    ;;
  has-session)
    exit 1
    ;;
  display-message)
    printf '%s\n' "$PWD"
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("BELAYER_FAKE_TMUX_DIR", logDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(logDir, "tmux.log")
}

func installFakeClamshell(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "clamshell.log")
	script := filepath.Join(binDir, "clamshell")
	body := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "` + logPath + `"
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake clamshell: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func startTestDaemon(t *testing.T) (context.CancelFunc, string) {
	t.Helper()
	baseDir, err := os.MkdirTemp("/tmp", "belayer-cli-daemon-")
	if err != nil {
		t.Fatalf("mktemp daemon dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(baseDir) })
	socketPath := filepath.Join(baseDir, "daemon.sock")
	dbPath := filepath.Join(baseDir, "belayer.db")
	d, err := daemonpkg.New(daemonpkg.Config{SocketPath: socketPath, DBPath: dbPath})
	if err != nil {
		t.Fatalf("new daemon: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()

	client := NewClient(socketPath)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := client.Health(); err == nil {
			return cancel, socketPath
		}
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("daemon exited before becoming healthy: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("daemon did not become healthy in time")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func runRootCmd(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

func initGitRepo(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	mustRun := func(args ...string) {
		t.Helper()
		if out, err := execCombined(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustRun("git", "init", "-q")
	mustRun("git", "config", "user.email", "smoke@example.com")
	mustRun("git", "config", "user.name", "smoke")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun("git", "add", "README.md")
	mustRun("git", "commit", "-qm", "init")
}

func execCombined(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", ".."))
}

func TestSessionCreateClimbAliasStartsImplementRoster(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := installFakeTmux(t)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	repoDir := filepath.Join(t.TempDir(), "extend-api")
	initGitRepo(t, repoDir, "extend-api")

	out := runRootCmd(t,
		"session", "create",
		"--socket", socketPath,
		"--template", "climb",
		"--input", "Add a test workflow",
		"--name", "smoke-climb",
		"--repo", repoDir,
	)
	for _, want := range []string{"pilot:", "implementer:", "reviewer:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	client := NewClient(socketPath)
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Template != "climb" || sessions[0].Status != "running" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	for _, want := range []string{"belayer-smoke-climb-pilot", "belayer-smoke-climb-implementer", "belayer-smoke-climb-reviewer"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("expected tmux log to contain %q, got:\n%s", want, string(logData))
		}
	}
}

func TestSessionCreateClimbFullstackStartsEnvironmentRoster(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := installFakeTmux(t)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	root := repoRoot(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "environment.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "environment.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "workbench.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "workbench.yaml"))
	for _, name := range []string{"pilot", "api-implementer", "app-implementer", "reviewer", "sprite"} {
		copyFile(t, filepath.Join(root, ".belayer", "templates", name, "agent.yaml"), filepath.Join(workspace, ".belayer", "templates", name, "agent.yaml"))
		copyFile(t, filepath.Join(root, ".belayer", "templates", name, "system-prompt.md"), filepath.Join(workspace, ".belayer", "templates", name, "system-prompt.md"))
	}

	apiRepo := filepath.Join(t.TempDir(), "extend-api")
	appRepo := filepath.Join(t.TempDir(), "extend-app")
	initGitRepo(t, apiRepo, "extend-api")
	initGitRepo(t, appRepo, "extend-app")

	envPath := filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "environment.yaml")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read environment: %v", err)
	}
	rewritten := strings.ReplaceAll(string(envData), "~/Documents/Programs/work/extend-api", apiRepo)
	rewritten = strings.ReplaceAll(rewritten, "~/Documents/Programs/work/extend-app", appRepo)
	if err := os.WriteFile(envPath, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}

	out := runRootCmd(t,
		"session", "create",
		"--socket", socketPath,
		"--template", "climb-fullstack",
		"--environment", "extend-fullstack",
		"--input", "Ship the fullstack workflow",
		"--name", "smoke-fullstack",
	)
	for _, want := range []string{"pilot:", "api-implementer:", "app-implementer:", "reviewer:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	client := NewClient(socketPath)
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Template != "climb-fullstack" || sessions[0].Status != "running" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	for _, want := range []string{"belayer-smoke-fullstack-pilot", "belayer-smoke-fullstack-api-implementer", "belayer-smoke-fullstack-app-implementer", "belayer-smoke-fullstack-reviewer"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("expected tmux log to contain %q, got:\n%s", want, string(logData))
		}
	}

	metaPath := filepath.Join(home, ".belayer", "sandboxes", sessions[0].ID, "runtime.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read runtime metadata: %v", err)
	}
	for _, want := range []string{"\"environment\": \"extend-fullstack\"", apiRepo, appRepo} {
		if !strings.Contains(string(metaData), want) {
			t.Fatalf("expected runtime metadata to contain %q, got:\n%s", want, string(metaData))
		}
	}
}

func TestSessionCreateEpicStartsPilotOnlyRoster(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := installFakeTmux(t)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	root := repoRoot(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "environment.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "environment.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "workbench.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "workbench.yaml"))
	for _, name := range []string{"pilot"} {
		copyFile(t, filepath.Join(root, ".belayer", "templates", name, "agent.yaml"), filepath.Join(workspace, ".belayer", "templates", name, "agent.yaml"))
		copyFile(t, filepath.Join(root, ".belayer", "templates", name, "system-prompt.md"), filepath.Join(workspace, ".belayer", "templates", name, "system-prompt.md"))
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}

	out := runRootCmd(t,
		"session", "create",
		"--socket", socketPath,
		"--template", "epic",
		"--environment", "extend-fullstack",
		"--input", "JIRA-1234",
		"--name", "smoke-epic",
	)
	if !strings.Contains(out, "pilot:") {
		t.Fatalf("expected epic output to contain pilot roster, got:\n%s", out)
	}
	if strings.Contains(out, "reviewer:") || strings.Contains(out, "implementer:") {
		t.Fatalf("expected epic output to be pilot-only, got:\n%s", out)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	if !strings.Contains(string(logData), "belayer-smoke-epic-pilot") {
		t.Fatalf("expected pilot tmux launch, got:\n%s", string(logData))
	}
}

func TestSessionCreateEpicClamshellLaunchesPilotSandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := installFakeClamshell(t)
	cancel, socketPath := startTestDaemon(t)
	defer cancel()

	root := repoRoot(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "environment.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "environment.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "environments", "extend-fullstack", "workbench.yaml"), filepath.Join(workspace, ".belayer", "environments", "extend-fullstack", "workbench.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "policies", "extend-fullstack.yaml"), filepath.Join(workspace, ".belayer", "policies", "extend-fullstack.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "templates", "pilot", "agent.yaml"), filepath.Join(workspace, ".belayer", "templates", "pilot", "agent.yaml"))
	copyFile(t, filepath.Join(root, ".belayer", "templates", "pilot", "system-prompt.md"), filepath.Join(workspace, ".belayer", "templates", "pilot", "system-prompt.md"))

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}

	out := runRootCmd(t,
		"session", "create",
		"--socket", socketPath,
		"--template", "epic",
		"--environment", "extend-fullstack",
		"--clamshell",
		"--input", "JIRA-9999",
		"--name", "smoke-epic-clamshell",
	)
	if !strings.Contains(out, "pilot:") {
		t.Fatalf("expected clamshell epic output to contain pilot roster, got:\n%s", out)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read clamshell log: %v", err)
	}
	if !strings.Contains(string(logData), "sandbox create --name belayer-smoke-epic-clamshell-pilot") {
		t.Fatalf("expected clamshell sandbox create invocation, got:\n%s", string(logData))
	}
	metaClient := NewClient(socketPath)
	sessions, err := metaClient.ListSessions()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("unexpected sessions: %#v err=%v", sessions, err)
	}
	metaPath := filepath.Join(home, ".belayer", "sandboxes", sessions[0].ID, "runtime.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read runtime metadata: %v", err)
	}
	for _, want := range []string{`"runtime": "clamshell"`, `"sandbox_name": "belayer-smoke-epic-clamshell-pilot"`} {
		if !strings.Contains(string(metaData), want) {
			t.Fatalf("expected runtime metadata to contain %q, got:\n%s", want, string(metaData))
		}
	}
}
