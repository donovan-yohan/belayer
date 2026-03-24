package belayerassets

import (
	"strings"
	"testing"
)

func TestPluginVersion(t *testing.T) {
	if got := MustPluginVersion("harness"); got != "3.4.0" {
		t.Fatalf("unexpected harness version: %s", got)
	}
	if got := MustPluginVersion("pr"); got != "1.2.0" {
		t.Fatalf("unexpected pr version: %s", got)
	}
	if got := MustPluginVersion("explorer"); got != "0.2.0" {
		t.Fatalf("unexpected explorer version: %s", got)
	}
}

func TestCodexSkillFiles_GeneratesCommandSkillsAndCopiesStaticSkills(t *testing.T) {
	files, err := CodexSkillFiles()
	if err != nil {
		t.Fatalf("CodexSkillFiles returned error: %v", err)
	}

	required := []string{
		"harness-plan/SKILL.md",
		"harness-orchestrate/SKILL.md",
		"pr-author/SKILL.md",
		"strangler-fig/SKILL.md",
		"strangler-fig/references/steps-by-context.md",
		"explorer-send/SKILL.md",
	}
	for _, path := range required {
		if _, ok := files[path]; !ok {
			t.Fatalf("missing generated file %s", path)
		}
	}
}

func TestCodexSkillFiles_RewritesRuntimeReferences(t *testing.T) {
	files, err := CodexSkillFiles()
	if err != nil {
		t.Fatalf("CodexSkillFiles returned error: %v", err)
	}

	plan := string(files["harness-plan/SKILL.md"])
	if strings.Contains(plan, "/harness:orchestrate") {
		t.Fatalf("expected Claude slash commands to be rewritten in harness-plan skill")
	}
	if !strings.Contains(plan, "harness-orchestrate") {
		t.Fatalf("expected Codex skill reference in harness-plan skill")
	}
	if strings.Contains(plan, "superpowers:writing-plans") {
		t.Fatalf("expected superpowers namespace to be rewritten for Codex")
	}
	if !strings.Contains(plan, "writing-plans") {
		t.Fatalf("expected Codex superpowers skill reference in harness-plan skill")
	}

	refactor := string(files["strangler-fig/SKILL.md"])
	if strings.Contains(refactor, "plugins/harness/skills/strangler-fig/") {
		t.Fatalf("expected static skill repo-relative paths to be rewritten")
	}
	if !strings.Contains(refactor, "references/steps-by-context.md") {
		t.Fatalf("expected static skill reference path to be preserved as local reference")
	}

	send := string(files["explorer-send/SKILL.md"])
	// The "Claude alias:" header line intentionally preserves /explorer:send.
	// Check that body content (Usage section) is rewritten.
	if !strings.Contains(send, "explorer-send") {
		t.Fatalf("expected explorer-send Codex skill reference in explorer send skill")
	}
}
