package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
