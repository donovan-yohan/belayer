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

func TestTalentGeneratedPersistAndList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern", "--kind", "story"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}

	persistCmd := newTeamCmd()
	persistOut := &bytes.Buffer{}
	persistCmd.SetOut(persistOut)
	persistCmd.SetErr(persistOut)
	persistCmd.SetArgs([]string{
		"generated", "persist", "last-lantern", "mara-underbough",
		"--domain", "story",
		"--role", "tavernkeep",
		"--lifecycle", "resumable",
		"--status", "generated",
		"--source-request", "turn-0002",
		"--reason", "scene needs a local authority who remembers rumors",
		"--metadata", "voice=warm and watchful",
		"--metadata", "constraint=does not know who sent the sealed letter",
		"--promotion-evidence", "artifacts/talent-evaluation-mara.json",
		"--note", "First appeared when the player asked about old roads.",
	})
	if err := persistCmd.Execute(); err != nil {
		t.Fatalf("generated persist: %v\n%s", err, persistOut.String())
	}
	if !strings.Contains(persistOut.String(), "Persisted generated talent mara-underbough") {
		t.Fatalf("unexpected persist output: %s", persistOut.String())
	}

	talentPath := filepath.Join(home, "crags", "last-lantern", "generated-talents", "mara-underbough", "talent.yaml")
	raw, err := os.ReadFile(talentPath)
	if err != nil {
		t.Fatalf("read generated talent: %v", err)
	}
	for _, want := range []string{
		`schema_version: belayer-generated-talent/v1`,
		`id: mara-underbough`,
		`domain: story`,
		`role: tavernkeep`,
		`lifecycle: resumable`,
		`status: generated`,
		`source_request: turn-0002`,
		`voice: warm and watchful`,
		`constraint: does not know who sent the sealed letter`,
		`artifacts/talent-evaluation-mara.json`,
		`created_at:`,
		`updated_at:`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("generated talent missing %q:\n%s", want, string(raw))
		}
	}
	notesPath := filepath.Join(home, "crags", "last-lantern", "generated-talents", "mara-underbough", "notes.md")
	notes, err := os.ReadFile(notesPath)
	if err != nil {
		t.Fatalf("read generated talent notes: %v", err)
	}
	if !strings.Contains(string(notes), "First appeared") {
		t.Fatalf("notes missing first appearance:\n%s", string(notes))
	}

	listCmd := newTeamCmd()
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(listOut)
	listCmd.SetArgs([]string{"generated", "list", "last-lantern"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("generated list: %v", err)
	}
	if got := strings.Fields(listOut.String()); len(got) < 5 ||
		got[0] != "mara-underbough" ||
		got[1] != "story" ||
		got[2] != "tavernkeep" ||
		got[3] != "resumable" ||
		got[4] != "generated" {
		t.Fatalf("generated list missing persisted talent fields:\n%s", listOut.String())
	}
}

func TestTalentGeneratedPersistRejectsPromotedWithoutEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}

	cmd := newTeamCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"generated", "persist", "last-lantern", "mara-underbough",
		"--domain", "story",
		"--role", "tavernkeep",
		"--status", "promoted",
		"--source-request", "turn-0002",
		"--reason", "scene needs a local authority",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected promoted status without evidence to fail")
	}
}

func TestTalentGeneratedPersistRejectsEmptyPromotionEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}

	cmd := newTeamCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"generated", "persist", "last-lantern", "mara-underbough",
		"--domain", "story",
		"--role", "tavernkeep",
		"--source-request", "turn-0002",
		"--reason", "scene needs a local authority",
		"--promotion-evidence", " ",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected empty promotion evidence to fail")
	}
}

func TestTalentGeneratedListSkipsDanglingDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "crags", "last-lantern", "generated-talents", "dangling"), 0o755); err != nil {
		t.Fatalf("mkdir dangling generated talent: %v", err)
	}

	cmd := newTeamCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"generated", "list", "last-lantern"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("generated list: %v\n%s", err, out.String())
	}
	if out.String() != "" {
		t.Fatalf("expected dangling dir to be skipped, got:\n%s", out.String())
	}
}

func TestTalentGeneratedPersistRejectsInvalidLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}

	cmd := newTeamCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"generated", "persist", "last-lantern", "mara-underbough",
		"--domain", "story",
		"--role", "tavernkeep",
		"--lifecycle", "sidequest",
		"--source-request", "turn-0002",
		"--reason", "scene needs a local authority",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid lifecycle to fail")
	}
}

func TestTalentGeneratedScaffoldCreatesRunnableIdentity(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("BELAYER_HOME", home)

	initCmd := newCragCmd()
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.SetErr(&bytes.Buffer{})
	initCmd.SetArgs([]string{"init", "last-lantern", "--kind", "story"})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("crag init: %v", err)
	}

	persistCmd := newTeamCmd()
	persistCmd.SetOut(&bytes.Buffer{})
	persistCmd.SetErr(&bytes.Buffer{})
	persistCmd.SetArgs([]string{
		"generated", "persist", "last-lantern", "mara-underbough",
		"--domain", "story",
		"--role", "tavernkeep",
		"--lifecycle", "resumable",
		"--source-request", "turn-0002",
		"--reason", "scene needs a reusable local authority",
		"--metadata", "voice=warm and watchful",
	})
	if err := persistCmd.Execute(); err != nil {
		t.Fatalf("generated persist: %v", err)
	}

	scaffoldCmd := newTeamCmd()
	scaffoldOut := &bytes.Buffer{}
	scaffoldCmd.SetOut(scaffoldOut)
	scaffoldCmd.SetErr(scaffoldOut)
	scaffoldCmd.SetArgs([]string{"generated", "scaffold", "last-lantern", "mara-underbough", "--target", project})
	if err := scaffoldCmd.Execute(); err != nil {
		t.Fatalf("generated scaffold: %v\n%s", err, scaffoldOut.String())
	}
	if !strings.Contains(scaffoldOut.String(), "Scaffolded generated talent mara-underbough") {
		t.Fatalf("unexpected scaffold output: %s", scaffoldOut.String())
	}

	identityDir := filepath.Join(project, ".belayer", "agents", "mara-underbough")
	for _, rel := range []string{"agent.yaml", "system-prompt.md", "agents.md", "talent.yaml"} {
		if _, err := os.Stat(filepath.Join(identityDir, rel)); err != nil {
			t.Fatalf("expected scaffolded %s: %v", rel, err)
		}
	}
	agentYAML, err := os.ReadFile(filepath.Join(identityDir, "agent.yaml"))
	if err != nil {
		t.Fatalf("read scaffolded agent.yaml: %v", err)
	}
	for _, want := range []string{
		`kind: side`,
		`ephemeral: false`,
		`model: gpt-5.4`,
	} {
		if !strings.Contains(string(agentYAML), want) {
			t.Fatalf("agent.yaml missing %q:\n%s", want, string(agentYAML))
		}
	}
	systemPrompt, err := os.ReadFile(filepath.Join(identityDir, "system-prompt.md"))
	if err != nil {
		t.Fatalf("read scaffolded system prompt: %v", err)
	}
	for _, want := range []string{"domain: story", "role: tavernkeep", "source request: turn-0002", "voice: warm and watchful"} {
		if !strings.Contains(string(systemPrompt), want) {
			t.Fatalf("system-prompt.md missing %q:\n%s", want, string(systemPrompt))
		}
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
