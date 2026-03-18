package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/manage"
)

func TestExplorerCommandPreparesNamedWorkspaceWithAbsolutePRD(t *testing.T) {
	setupCLIHome(t)

	projectDir := t.TempDir()
	prdPath := filepath.Join(projectDir, "doc.md")
	if err := os.WriteFile(prdPath, []byte("# PRD"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	restoreDir := chdirForTest(t, projectDir)
	defer restoreDir()

	var launchedDir string
	var launchedSkipPermissions bool
	restoreLauncher := stubClaudeLauncher(t, func(dir string, envOverrides map[string]string, skipPermissions bool) error {
		launchedDir = dir
		launchedSkipPermissions = skipPermissions
		if len(envOverrides) != 0 {
			t.Fatalf("envOverrides = %v, want empty for explorer", envOverrides)
		}
		return nil
	})
	defer restoreLauncher()

	cmd := newExplorerSessionCmd()
	cmd.SetArgs([]string{"--name", "myproject", "--prd", "doc.md"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	expectedDir := filepath.Join(os.Getenv("HOME"), ".belayer", "explorer", "myproject")
	if launchedDir != expectedDir {
		t.Fatalf("launchedDir = %q, want %q", launchedDir, expectedDir)
	}
	if launchedSkipPermissions {
		t.Fatal("expected --yolo to default to false")
	}

	claudeMD, err := os.ReadFile(filepath.Join(expectedDir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(claudeMD)
	if !strings.Contains(content, prdPath) {
		t.Fatalf("CLAUDE.md should contain absolute PRD path %q", prdPath)
	}
	if !strings.Contains(content, "**Project Name:** myproject") {
		t.Fatal("CLAUDE.md should contain the project name")
	}
}

func TestExplorerCommandPreparesUnnamedWorkspaceAndPassesYolo(t *testing.T) {
	setupCLIHome(t)

	var launchedDir string
	var launchedSkipPermissions bool
	restoreLauncher := stubClaudeLauncher(t, func(dir string, envOverrides map[string]string, skipPermissions bool) error {
		launchedDir = dir
		launchedSkipPermissions = skipPermissions
		return nil
	})
	defer restoreLauncher()

	cmd := newExplorerSessionCmd()
	cmd.SetArgs([]string{"--yolo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.HasPrefix(filepath.Base(launchedDir), "_unnamed-") {
		t.Fatalf("launchedDir = %q, want unnamed explorer workspace", launchedDir)
	}
	if !launchedSkipPermissions {
		t.Fatal("expected --yolo to be forwarded to the Claude launcher")
	}

	claudeMD, err := os.ReadFile(filepath.Join(launchedDir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(claudeMD), "Not chosen yet") {
		t.Fatal("CLAUDE.md should prompt for a project name when none is provided")
	}
}

func TestExplorerCommandRejectsDirectoryPRD(t *testing.T) {
	setupCLIHome(t)

	projectDir := t.TempDir()
	restoreDir := chdirForTest(t, projectDir)
	defer restoreDir()

	prdDir := filepath.Join(projectDir, "docs")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cmd := newExplorerSessionCmd()
	cmd.SetArgs([]string{"--prd", "docs"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("error = %q, want mention that the PRD path is a directory", err)
	}
}

func TestExplorerCommandResumesExistingWorkspaceWhenChosen(t *testing.T) {
	setupCLIHome(t)

	projectDir := t.TempDir()
	restoreDir := chdirForTest(t, projectDir)
	defer restoreDir()

	workspaceDir := filepath.Join(os.Getenv("HOME"), ".belayer", "explorer", "myproject")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", workspaceDir, err)
	}
	stalePath := filepath.Join(workspaceDir, "research-notes.md")
	if err := os.WriteFile(stalePath, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", stalePath, err)
	}

	var launchedDir string
	restoreLauncher := stubClaudeLauncher(t, func(dir string, envOverrides map[string]string, skipPermissions bool) error {
		launchedDir = dir
		if len(envOverrides) != 0 {
			t.Fatalf("envOverrides = %v, want empty for explorer", envOverrides)
		}
		if skipPermissions {
			t.Fatal("expected --yolo to default to false")
		}
		return nil
	})
	defer restoreLauncher()

	cmd := newExplorerSessionCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetIn(strings.NewReader("resume\n"))
	cmd.SetArgs([]string{"--name", "myproject"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if launchedDir != workspaceDir {
		t.Fatalf("launchedDir = %q, want %q", launchedDir, workspaceDir)
	}
	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("resume should preserve stale workspace contents: %v", err)
	}
	gotOutput := strings.ToLower(output.String())
	if !strings.Contains(gotOutput, "resume") || !strings.Contains(gotOutput, "start fresh") {
		t.Fatalf("prompt output = %q, want mention of resume and start fresh", output.String())
	}
}

func TestExplorerCommandStartsFreshWhenChosen(t *testing.T) {
	setupCLIHome(t)

	projectDir := t.TempDir()
	restoreDir := chdirForTest(t, projectDir)
	defer restoreDir()

	workspaceDir := filepath.Join(os.Getenv("HOME"), ".belayer", "explorer", "myproject")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", workspaceDir, err)
	}
	stalePath := filepath.Join(workspaceDir, "research-notes.md")
	if err := os.WriteFile(stalePath, []byte("remove me"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", stalePath, err)
	}

	var launchedDir string
	restoreLauncher := stubClaudeLauncher(t, func(dir string, envOverrides map[string]string, skipPermissions bool) error {
		launchedDir = dir
		if len(envOverrides) != 0 {
			t.Fatalf("envOverrides = %v, want empty for explorer", envOverrides)
		}
		return nil
	})
	defer restoreLauncher()

	cmd := newExplorerSessionCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetIn(strings.NewReader("start fresh\n"))
	cmd.SetArgs([]string{"--name", "myproject"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if launchedDir != workspaceDir {
		t.Fatalf("launchedDir = %q, want %q", launchedDir, workspaceDir)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("start fresh should remove stale workspace contents, stat err = %v", err)
	}
	gotOutput := strings.ToLower(output.String())
	if !strings.Contains(gotOutput, "resume") || !strings.Contains(gotOutput, "start fresh") {
		t.Fatalf("prompt output = %q, want mention of resume and start fresh", output.String())
	}
}

func TestExplorerCommandRestoresWorkspaceWhenFreshPrepareFails(t *testing.T) {
	setupCLIHome(t)

	projectDir := t.TempDir()
	restoreDir := chdirForTest(t, projectDir)
	defer restoreDir()

	workspaceDir := filepath.Join(os.Getenv("HOME"), ".belayer", "explorer", "myproject")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", workspaceDir, err)
	}
	stalePath := filepath.Join(workspaceDir, "research-notes.md")
	if err := os.WriteFile(stalePath, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", stalePath, err)
	}

	restorePrepare := stubPrepareExplorerDir(t, func(rootDir string, data manage.ExplorerPromptData) (string, error) {
		return "", errors.New("boom")
	})
	defer restorePrepare()

	cmd := newExplorerSessionCmd()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetIn(strings.NewReader("fresh\n"))
	cmd.SetArgs([]string{"--name", "myproject"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "preparing explorer workspace") {
		t.Fatalf("error = %q, want prepare failure", err)
	}
	if _, statErr := os.Stat(stalePath); statErr != nil {
		t.Fatalf("fresh prepare failure should restore the original workspace: %v", statErr)
	}
}

func TestReadExplorerWorkspaceActionRetriesAfterInvalidInput(t *testing.T) {
	output := &bytes.Buffer{}

	action, err := readExplorerWorkspaceAction(strings.NewReader("later\nfresh\n"), output, "/tmp/workspace")
	if err != nil {
		t.Fatalf("readExplorerWorkspaceAction() error: %v", err)
	}
	if action != explorerWorkspaceFresh {
		t.Fatalf("action = %q, want %q", action, explorerWorkspaceFresh)
	}
	if !strings.Contains(output.String(), "Enter 'resume' or 'fresh': ") {
		t.Fatalf("prompt output = %q, want retry prompt", output.String())
	}
}

func TestReadExplorerWorkspaceActionRejectsUnexpectedEOF(t *testing.T) {
	_, err := readExplorerWorkspaceAction(strings.NewReader("later"), io.Discard, "/tmp/workspace")
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestRootCommandIncludesExplorer(t *testing.T) {
	root := NewRootCmd()

	found, _, err := root.Find([]string{"explorer"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found == nil || found.Use != "explorer" {
		t.Fatalf("found = %#v, want explorer command", found)
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	return func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
	}
}

func stubClaudeLauncher(t *testing.T, fn func(dir string, envOverrides map[string]string, skipPermissions bool) error) func() {
	t.Helper()
	original := execClaudeSession
	execClaudeSession = fn
	return func() {
		execClaudeSession = original
	}
}

func stubPrepareExplorerDir(t *testing.T, fn func(rootDir string, data manage.ExplorerPromptData) (string, error)) func() {
	t.Helper()
	original := prepareExplorerDir
	prepareExplorerDir = fn
	return func() {
		prepareExplorerDir = original
	}
}
