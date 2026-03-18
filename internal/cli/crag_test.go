package cli

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donovan-yohan/belayer/internal/crag"
)

func TestCragCreateCommandAllowsLocalPathsWithFlag(t *testing.T) {
	setupCLIHome(t)
	repoPath := createCLIRepo(t, "local-repo")

	cmd := newCragCreateCmd()
	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"local-crag", "--repos", repoPath, "--local-paths"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, _, err := crag.Load("local-crag")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Repos[0].URL; got != repoPath {
		t.Fatalf("stored repo source = %q, want %q", got, repoPath)
	}
}

func TestCragCreateCommandRejectsLocalPathsWithoutFlag(t *testing.T) {
	setupCLIHome(t)
	repoPath := createCLIRepo(t, "local-repo")

	cmd := newCragCreateCmd()
	cmd.SetArgs([]string{"local-crag", "--repos", repoPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--local-paths") {
		t.Fatalf("error = %q, want mention of --local-paths", err)
	}

	crags, listErr := crag.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(crags) != 0 {
		t.Fatalf("crags = %v, want no created crags after validation failure", crags)
	}
}

func TestCragCreateCommandAllowsRemoteURLsWithoutFlag(t *testing.T) {
	setupCLIHome(t)
	repoPath := createCLIRepo(t, "remote-repo")
	repoURL := (&url.URL{Scheme: "file", Path: repoPath}).String()

	cmd := newCragCreateCmd()
	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"remote-crag", "--repos", repoURL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, _, err := crag.Load("remote-crag")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Repos[0].URL; got != repoURL {
		t.Fatalf("stored repo source = %q, want %q", got, repoURL)
	}
}

func setupCLIHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func createCLIRepo(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "config", "user.email", "test@test.com"},
		{"-C", dir, "config", "user.name", "Test"},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	return dir
}
