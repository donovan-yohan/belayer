package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamListShowsDevelopmentAndStory(t *testing.T) {
	cmd := newTeamCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("team list: %v", err)
	}

	got := out.String()
	for _, want := range []string{"development", "backend-dev", "story", "storyteller", "continuity-editor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("team list missing %q:\n%s", want, got)
		}
	}
}

func TestTeamAddStoryCategorySkipsAndForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	first := newTeamCmd()
	firstOut := &bytes.Buffer{}
	first.SetOut(firstOut)
	first.SetErr(firstOut)
	first.SetArgs([]string{"add", "story", "--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if !strings.Contains(firstOut.String(), "written=") {
		t.Fatalf("expected written count, got: %s", firstOut.String())
	}

	promptPath := filepath.Join(dir, ".belayer", "agents", "storyteller", "system-prompt.md")
	if _, err := os.Stat(promptPath); err != nil {
		t.Fatalf("expected storyteller prompt: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("local edit"), 0o644); err != nil {
		t.Fatalf("write local edit: %v", err)
	}

	second := newTeamCmd()
	secondOut := &bytes.Buffer{}
	second.SetOut(secondOut)
	second.SetErr(secondOut)
	second.SetArgs([]string{"add", "story", "--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second add: %v", err)
	}
	if !strings.Contains(secondOut.String(), "skipped=") {
		t.Fatalf("expected skipped count, got: %s", secondOut.String())
	}
	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if string(got) != "local edit" {
		t.Fatalf("add without --force overwrote local edit: %q", string(got))
	}

	forced := newTeamCmd()
	forced.SetOut(&bytes.Buffer{})
	forced.SetErr(&bytes.Buffer{})
	forced.SetArgs([]string{"add", "story", "--target", dir, "--force"})
	if err := forced.Execute(); err != nil {
		t.Fatalf("forced add: %v", err)
	}
	got, err = os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read forced prompt: %v", err)
	}
	if string(got) == "local edit" {
		t.Fatalf("add --force did not overwrite local edit")
	}
}

func TestTeamAddSingleDevelopmentTeam(t *testing.T) {
	dir := t.TempDir()
	cmd := newTeamCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"add", "development/backend-dev", "--target", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add development/backend-dev: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "backend-dev", "agent.yaml")); err != nil {
		t.Fatalf("expected backend-dev agent.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "web-dev", "agent.yaml")); !os.IsNotExist(err) {
		t.Fatalf("single add should not install web-dev, stat err=%v", err)
	}
}

func TestTeamAddRejectsTraversal(t *testing.T) {
	cmd := newTeamCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"add", "../story"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected traversal reference to fail")
	}
}

func TestTeamRemoveSingleDevelopmentTeam(t *testing.T) {
	dir := t.TempDir()

	addCmd := newTeamCmd()
	addCmd.SetOut(&bytes.Buffer{})
	addCmd.SetErr(&bytes.Buffer{})
	addCmd.SetArgs([]string{"add", "development/backend-dev", "--target", dir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add development/backend-dev: %v", err)
	}

	removeCmd := newTeamCmd()
	removeOut := &bytes.Buffer{}
	removeCmd.SetOut(removeOut)
	removeCmd.SetErr(removeOut)
	removeCmd.SetArgs([]string{"remove", "development/backend-dev", "--target", dir})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("remove development/backend-dev: %v", err)
	}
	if !strings.Contains(removeOut.String(), "removed=1") {
		t.Fatalf("expected remove count, got: %s", removeOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "backend-dev")); !os.IsNotExist(err) {
		t.Fatalf("expected backend-dev dir removed, stat err=%v", err)
	}
}

func TestCragInitListAndLink(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initOut := &bytes.Buffer{}
	initCmd.SetOut(initOut)
	initCmd.SetErr(initOut)
	initCmd.SetArgs([]string{"init", "software-company", "--kind", "development", "--description", "Software org"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}
	cragYAML := filepath.Join(home, "crags", "software-company", "crag.yaml")
	raw, err := os.ReadFile(cragYAML)
	if err != nil {
		t.Fatalf("read crag.yaml: %v", err)
	}
	if !strings.Contains(string(raw), `schema_version: "belayer-crag/v1"`) || !strings.Contains(string(raw), `kind: "development"`) {
		t.Fatalf("unexpected crag.yaml:\n%s", string(raw))
	}

	listCmd := newCragCmd()
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(listOut)
	listCmd.SetArgs([]string{"list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("crag list: %v", err)
	}
	if !strings.Contains(listOut.String(), "software-company") {
		t.Fatalf("crag list missing software-company: %s", listOut.String())
	}

	configPath := filepath.Join(project, ".belayer", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("log_level: standard\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	linkCmd := newCragCmd()
	linkCmd.SetOut(&bytes.Buffer{})
	linkCmd.SetErr(&bytes.Buffer{})
	linkCmd.SetArgs([]string{"link", "software-company", "--target", project})
	if err := linkCmd.Execute(); err != nil {
		t.Fatalf("crag link: %v", err)
	}
	linked, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read linked config: %v", err)
	}
	got := string(linked)
	if !strings.Contains(got, "log_level: standard") || !strings.Contains(got, "crag:\n  name: \"software-company\"\n") {
		t.Fatalf("unexpected linked config:\n%s", got)
	}
}

func TestCragInitRejectsInvalidKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	cmd := newCragCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"init", "software-company", "--kind", "invalid"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid crag kind to fail")
	}
}

func TestSetCragLinkBlockReplacesExistingBlocks(t *testing.T) {
	raw := []byte("log_level: standard\norg:\n  name: old\nruntime:\n  max_concurrent_mains: 8\n")
	got := string(setCragLinkBlock(raw, "new-crag", ""))
	if strings.Contains(got, "old") {
		t.Fatalf("old org remained:\n%s", got)
	}
	for _, want := range []string{"log_level: standard", "crag:\n  name: \"new-crag\"\n", "runtime:\n  max_concurrent_mains: 8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestSetCragLinkBlockPreservesFollowingTopLevelComment(t *testing.T) {
	raw := []byte("log_level: standard\ncrag:\n  name: old\n# runtime settings\nruntime:\n  max_concurrent_mains: 8\n")
	got := string(setCragLinkBlock(raw, "new-crag", "local/crag"))
	for _, want := range []string{
		"crag:\n  name: \"new-crag\"\n  path: \"local/crag\"\n",
		"# runtime settings",
		"runtime:\n  max_concurrent_mains: 8",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRootAliasesForCragClimbAndTeam(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	for _, args := range [][]string{
		{"crag", "list"},
		{"space", "list"},
		{"org", "list"},
		{"team", "list"},
		{"teams", "list"},
		{"talent", "list"},
		{"climb", "--help"},
	} {
		cmd := NewRootCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("belayer %s: %v\n%s", strings.Join(args, " "), err, out.String())
		}
	}
}
