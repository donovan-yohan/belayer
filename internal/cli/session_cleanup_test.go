package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildSessionCleanupPaths(t *testing.T) {
	paths := buildSessionCleanupPaths("/tmp/home", "/tmp/work", "sess-123", "night-shift")

	if got, want := paths.SandboxDir, filepath.Join("/tmp/home", ".belayer", "sandboxes", "sess-123"); got != want {
		t.Fatalf("SandboxDir = %q, want %q", got, want)
	}
	if got, want := paths.LocalWorktreeDir, filepath.Join("/tmp/work", "belayer-worktrees", "night-shift"); got != want {
		t.Fatalf("LocalWorktreeDir = %q, want %q", got, want)
	}
}

func TestCleanupSessionArtifacts_RemovesSandboxAndLocalWorktrees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts and unixy paths")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	repoDir := initGitRepo(t)
	sessionID := "sess-123"
	sessionName := "implement-123"
	sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sessionID)
	localBaseDir := filepath.Join(tmpRoot, "belayer-worktrees", sessionName)
	createGitWorktree(t, repoDir, filepath.Join(sandboxDir, "worktrees", "pilot"))
	createGitWorktree(t, repoDir, filepath.Join(localBaseDir, "worktrees", "pilot"))

	var outBuf, errBuf bytes.Buffer
	if err := cleanupSessionArtifacts(buildSessionCleanupPaths(home, tmpRoot, sessionID, sessionName), &outBuf, &errBuf); err != nil {
		t.Fatalf("cleanupSessionArtifacts: %v", err)
	}

	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("expected sandbox dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(localBaseDir); !os.IsNotExist(err) {
		t.Fatalf("expected local worktree base dir to be removed, stat err=%v", err)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("expected no warnings, got: %s", errBuf.String())
	}
}

func TestCleanupSessionArtifacts_WarnsButSucceedsForNonGitWorktrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	sessionID := "sess-warn"
	sessionName := "warn-session"
	sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sessionID, "worktrees", "pilot")
	localDir := filepath.Join(tmpRoot, "belayer-worktrees", sessionName, "worktrees", "reviewer")
	if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
		t.Fatalf("mkdir sandbox worktree: %v", err)
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("mkdir local worktree: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	if err := cleanupSessionArtifacts(buildSessionCleanupPaths(home, tmpRoot, sessionID, sessionName), &outBuf, &errBuf); err != nil {
		t.Fatalf("cleanupSessionArtifacts: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".belayer", "sandboxes", sessionID)); !os.IsNotExist(err) {
		t.Fatalf("expected sandbox dir to be removed even for non-git worktrees, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpRoot, "belayer-worktrees", sessionName)); !os.IsNotExist(err) {
		t.Fatalf("expected local worktree dir to be removed even for non-git worktrees, stat err=%v", err)
	}

	warnings := errBuf.String()
	if warnings == "" {
		t.Fatal("expected stderr warning output for non-git worktrees, got none")
	}
	if !strings.Contains(warnings, sandboxDir) {
		t.Fatalf("expected stderr warning to mention sandbox worktree %q, got: %q", sandboxDir, warnings)
	}
	if !strings.Contains(warnings, localDir) {
		t.Fatalf("expected stderr warning to mention local worktree %q, got: %q", localDir, warnings)
	}
}

func TestCleanupSessionArtifacts_StopsDockerSandboxWhenComposeExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts and unixy paths")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "docker.log")
	writeExecutable(t, filepath.Join(binDir, "docker"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+logPath+"\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sessionID := "sess-docker"
	sessionName := "docker-session"
	sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sessionID)
	if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
		t.Fatalf("mkdir sandbox: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sandboxDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	if err := cleanupSessionArtifacts(buildSessionCleanupPaths(home, tmpRoot, sessionID, sessionName), &outBuf, &errBuf); err != nil {
		t.Fatalf("cleanupSessionArtifacts: %v", err)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("expected no stderr warnings, got: %s", errBuf.String())
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	if got := string(logBytes); !strings.Contains(got, "compose -f "+filepath.Join(sandboxDir, "docker-compose.yml")+" down") {
		t.Fatalf("expected docker compose down invocation, got %q", got)
	}
	if !strings.Contains(outBuf.String(), "Stopping Docker sandbox...") || !strings.Contains(outBuf.String(), "Docker sandbox stopped.") {
		t.Fatalf("expected cleanup output to mention docker sandbox stop, got: %q", outBuf.String())
	}
}

func TestNewSessionCmd_RegistersCleanSubcommand(t *testing.T) {
	cmd := newSessionCmd()
	if _, _, err := cmd.Find([]string{"clean"}); err != nil {
		t.Fatalf("expected session clean subcommand to be registered: %v", err)
	}
}

func TestSessionStopCommand_CleansArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses unix socket and shell scripts")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeExecutable(t, filepath.Join(binDir, "tmux"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "docker"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+logPath+"\"\n")
	writeExecutable(t, filepath.Join(binDir, "git"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+logPath+"\"\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	const sessionID = "sess-stop"
	const sessionName = "night-shift"
	socketPath := shortSocketPath(t)
	startFakeSessionDaemon(t, socketPath, sessionResponse{
		ID:       sessionID,
		Name:     sessionName,
		Status:   "running",
		Template: "implement",
	})

	sandboxDir := filepath.Join(home, ".belayer", "sandboxes", sessionID)
	sandboxWorktree := filepath.Join(sandboxDir, "worktrees", "pilot")
	if err := os.MkdirAll(sandboxWorktree, 0o755); err != nil {
		t.Fatalf("mkdir sandbox worktree: %v", err)
	}
	composePath := filepath.Join(sandboxDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	localBase := filepath.Join(tmpRoot, "belayer-worktrees", sessionName)
	localWorktree := filepath.Join(localBase, "worktrees", "reviewer")
	if err := os.MkdirAll(localWorktree, 0o755); err != nil {
		t.Fatalf("mkdir local worktree: %v", err)
	}

	cmd := newSessionStopCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--socket", socketPath, "--force", sessionID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("session stop failed: %v\nstderr:\n%s", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "Stopped session "+sessionID) {
		t.Fatalf("expected stop confirmation in stdout, got:\n%s", got)
	}
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("expected sandbox dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(localBase); !os.IsNotExist(err) {
		t.Fatalf("expected local worktree dir to be removed, stat err=%v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	logs := string(logBytes)
	if !strings.Contains(logs, "compose -f "+composePath+" down") {
		t.Fatalf("expected docker compose down invocation, got:\n%s", logs)
	}
	if !strings.Contains(logs, "-C "+sandboxWorktree+" rev-parse --git-common-dir") {
		t.Fatalf("expected sandbox worktree git cleanup attempt, got:\n%s", logs)
	}
	if !strings.Contains(logs, "-C "+localWorktree+" rev-parse --git-common-dir") {
		t.Fatalf("expected local worktree git cleanup attempt, got:\n%s", logs)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	runCmd(t, repoDir, "git", "init")
	runCmd(t, repoDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoDir, "git", "config", "user.name", "Belayer Tests")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runCmd(t, repoDir, "git", "add", "README.md")
	runCmd(t, repoDir, "git", "commit", "-m", "seed")
	return repoDir
}

func createGitWorktree(t *testing.T, repoDir, worktreePath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("mkdir worktree parent: %v", err)
	}
	runCmd(t, repoDir, "git", "worktree", "add", worktreePath, "--detach", "HEAD")
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func startFakeSessionDaemon(t *testing.T, socketPath string, session sessionResponse) {
	t.Helper()

	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on socket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(t, w, []sessionResponse{session})
	})
	mux.HandleFunc("GET /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(t, w, session)
	})
	mux.HandleFunc("PATCH /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		stopped := session
		stopped.Status = "stopped"
		writeJSONResponse(t, w, stopped)
	})
	mux.HandleFunc("POST /sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"logged"}`))
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(ln)
	}()

	t.Cleanup(func() {
		_ = server.Close()
		_ = ln.Close()
		_ = os.Remove(socketPath)
	})
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join("/tmp", fmt.Sprintf("belayer-%d-%d.sock", os.Getpid(), os.Getppid()))
	_ = os.Remove(path)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	return path
}
