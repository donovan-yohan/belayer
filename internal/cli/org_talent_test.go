package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTalentListShowsDevelopmentAndStory(t *testing.T) {
	cmd := newTalentCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("talent list: %v", err)
	}

	got := out.String()
	for _, want := range []string{"development", "backend-dev", "story", "storyteller", "continuity-editor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("talent list missing %q:\n%s", want, got)
		}
	}
}

func TestTalentInstallStoryCategorySkipsAndForceOverwrites(t *testing.T) {
	dir := t.TempDir()

	first := newTalentCmd()
	firstOut := &bytes.Buffer{}
	first.SetOut(firstOut)
	first.SetErr(firstOut)
	first.SetArgs([]string{"install", "story", "--target", dir})
	if err := first.Execute(); err != nil {
		t.Fatalf("first install: %v", err)
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

	second := newTalentCmd()
	secondOut := &bytes.Buffer{}
	second.SetOut(secondOut)
	second.SetErr(secondOut)
	second.SetArgs([]string{"install", "story", "--target", dir})
	if err := second.Execute(); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(secondOut.String(), "skipped=") {
		t.Fatalf("expected skipped count, got: %s", secondOut.String())
	}
	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if string(got) != "local edit" {
		t.Fatalf("install without --force overwrote local edit: %q", string(got))
	}

	forced := newTalentCmd()
	forced.SetOut(&bytes.Buffer{})
	forced.SetErr(&bytes.Buffer{})
	forced.SetArgs([]string{"install", "story", "--target", dir, "--force"})
	if err := forced.Execute(); err != nil {
		t.Fatalf("forced install: %v", err)
	}
	got, err = os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read forced prompt: %v", err)
	}
	if string(got) == "local edit" {
		t.Fatalf("install --force did not overwrite local edit")
	}
}

func TestTalentInstallSingleDevelopmentTalent(t *testing.T) {
	dir := t.TempDir()
	cmd := newTalentCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"install", "development/backend-dev", "--target", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install development/backend-dev: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "backend-dev", "agent.yaml")); err != nil {
		t.Fatalf("expected backend-dev agent.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".belayer", "agents", "web-dev", "agent.yaml")); !os.IsNotExist(err) {
		t.Fatalf("single install should not install web-dev, stat err=%v", err)
	}
}

func TestTalentInstallRejectsTraversal(t *testing.T) {
	cmd := newTalentCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"install", "../story"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected traversal reference to fail")
	}
}

func TestOrgInitListAndLink(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newOrgCmd()
	initOut := &bytes.Buffer{}
	initCmd.SetOut(initOut)
	initCmd.SetErr(initOut)
	initCmd.SetArgs([]string{"init", "software-company", "--kind", "development", "--description", "Software org"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("org init: %v", err)
	}
	orgYAML := filepath.Join(home, "orgs", "software-company", "org.yaml")
	raw, err := os.ReadFile(orgYAML)
	if err != nil {
		t.Fatalf("read org.yaml: %v", err)
	}
	if !strings.Contains(string(raw), `schema_version: "belayer-org/v1"`) || !strings.Contains(string(raw), "kind: development") {
		t.Fatalf("unexpected org.yaml:\n%s", string(raw))
	}

	listCmd := newOrgCmd()
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(listOut)
	listCmd.SetArgs([]string{"list"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("org list: %v", err)
	}
	if !strings.Contains(listOut.String(), "software-company") {
		t.Fatalf("org list missing software-company: %s", listOut.String())
	}

	configPath := filepath.Join(project, ".belayer", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("log_level: standard\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	linkCmd := newOrgCmd()
	linkCmd.SetOut(&bytes.Buffer{})
	linkCmd.SetErr(&bytes.Buffer{})
	linkCmd.SetArgs([]string{"link", "software-company", "--target", project})
	if err := linkCmd.Execute(); err != nil {
		t.Fatalf("org link: %v", err)
	}
	linked, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read linked config: %v", err)
	}
	got := string(linked)
	if !strings.Contains(got, "log_level: standard") || !strings.Contains(got, "org:\n  name: software-company\n") {
		t.Fatalf("unexpected linked config:\n%s", got)
	}
}

func TestSetOrgLinkBlockReplacesExistingBlock(t *testing.T) {
	raw := []byte("log_level: standard\norg:\n  name: old\nruntime:\n  max_concurrent_mains: 8\n")
	got := string(setOrgLinkBlock(raw, "new-org", ""))
	if strings.Contains(got, "old") {
		t.Fatalf("old org remained:\n%s", got)
	}
	for _, want := range []string{"log_level: standard", "org:\n  name: new-org\n", "runtime:\n  max_concurrent_mains: 8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
