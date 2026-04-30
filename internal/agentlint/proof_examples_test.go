package agentlint_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestOrgProofExamplesAreValidArtifacts(t *testing.T) {
	root := repoRoot(t)
	proofRoot := filepath.Join(root, "examples", "org-proofs")

	expectedFiles := map[string]string{
		"evaluation-report.md": "",

		filepath.Join("relay-ide-software-company", "README.md"):                          "",
		filepath.Join("relay-ide-software-company", "org-plan.json"):                      "belayer-org-plan/v1",
		filepath.Join("relay-ide-software-company", "gate-result-code-review.json"):       "belayer-gate-result/v1",
		filepath.Join("relay-ide-software-company", "gate-result-runtime-qa.json"):        "belayer-gate-result/v1",
		filepath.Join("relay-ide-software-company", "talent-evaluation-backend-dev.json"): "belayer-talent-evaluation/v1",
		filepath.Join("relay-ide-software-company", "org-retro.json"):                     "belayer-org-retro/v1",

		filepath.Join("athenaeum-tavern-story", "README.md"):                                           "",
		filepath.Join("athenaeum-tavern-story", "org-plan.json"):                                       "belayer-org-plan/v1",
		filepath.Join("athenaeum-tavern-story", "three-act-story.md"):                                  "",
		filepath.Join("athenaeum-tavern-story", "world-state.json"):                                    "belayer-world-state/v1",
		filepath.Join("athenaeum-tavern-story", "continuity-report.json"):                              "belayer-continuity-report/v1",
		filepath.Join("athenaeum-tavern-story", "talent-evaluation-storyteller.json"):                  "belayer-talent-evaluation/v1",
		filepath.Join("athenaeum-tavern-story", "talent-evaluation-tavernkeep-mara.json"):              "belayer-talent-evaluation/v1",
		filepath.Join("athenaeum-tavern-story", "org-retro.json"):                                      "belayer-org-retro/v1",
		filepath.Join("athenaeum-tavern-story", "generated-talents", "tavernkeep-mara", "talent.yaml"): "",
		filepath.Join("athenaeum-tavern-story", "generated-talents", "tavernkeep-mara", "notes.md"):    "",
	}

	for rel, wantSchema := range expectedFiles {
		path := filepath.Join(proofRoot, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if wantSchema == "" {
			continue
		}

		var artifact map[string]any
		if err := json.Unmarshal(raw, &artifact); err != nil {
			t.Fatalf("%s: invalid JSON: %v", path, err)
		}
		if got := artifact["schema_version"]; got != wantSchema {
			t.Errorf("%s: schema_version = %v, want %s", path, got, wantSchema)
		}
	}
}

func TestOrgProofExamplesDoNotUseLocalAbsolutePaths(t *testing.T) {
	root := repoRoot(t)
	proofRoot := filepath.Join(root, "examples", "org-proofs")

	err := filepath.WalkDir(proofRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "/Users/") {
			t.Errorf("%s contains local absolute path", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk proof examples: %v", err)
	}
}

func TestOrgProofExamplesKeepStoryAndSoftwareContractsSeparate(t *testing.T) {
	root := repoRoot(t)
	proofRoot := filepath.Join(root, "examples", "org-proofs")

	storyPlanPath := filepath.Join(proofRoot, "athenaeum-tavern-story", "org-plan.json")
	storyPlan := readJSONMap(t, storyPlanPath)
	storyGates := storyPlan["gates"].([]any)
	if len(storyGates) != 1 {
		t.Fatalf("%s: got %d gates, want 1 continuity gate", storyPlanPath, len(storyGates))
	}
	gate := storyGates[0].(map[string]any)
	if gate["id"] != "continuity" || gate["output_artifact"] != "continuity-report" {
		t.Fatalf("%s: story gate = %#v, want continuity-report gate", storyPlanPath, gate)
	}

	relayPlanPath := filepath.Join(proofRoot, "relay-ide-software-company", "org-plan.json")
	relayPlan := readJSONMap(t, relayPlanPath)
	relayGates := relayPlan["gates"].([]any)
	for _, want := range []string{"code-review", "runtime-qa", "acceptance"} {
		if !jsonObjectArrayContains(relayGates, "id", want) {
			t.Errorf("%s: missing software-company gate %q", relayPlanPath, want)
		}
	}
}

func TestOrgProofExamplesPersistGeneratedStoryTalent(t *testing.T) {
	root := repoRoot(t)
	proofRoot := filepath.Join(root, "examples", "org-proofs")
	path := filepath.Join(proofRoot, "athenaeum-tavern-story", "generated-talents", "tavernkeep-mara", "talent.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var talent map[string]any
	if err := yaml.Unmarshal(raw, &talent); err != nil {
		t.Fatalf("%s: invalid YAML: %v", path, err)
	}

	if got := talent["schema_version"]; got != "belayer-generated-talent/v1" {
		t.Errorf("%s: schema_version = %v, want belayer-generated-talent/v1", path, got)
	}
	if got := talent["name"]; got != "mara-underbough" {
		t.Errorf("%s: name = %v, want mara-underbough", path, got)
	}
	origin, ok := talent["origin"].(map[string]any)
	if !ok {
		t.Fatalf("%s: missing origin object", path)
	}
	if got := origin["session_id"]; got != "example-athenaeum-tavern" {
		t.Errorf("%s: origin.session_id = %v, want example-athenaeum-tavern", path, got)
	}
	firstArtifact, ok := origin["first_artifact"].(string)
	if !ok || !strings.Contains(firstArtifact, "athenaeum-tavern-story/three-act-story.md") {
		t.Errorf("%s: origin.first_artifact = %v, want tavern story artifact", path, origin["first_artifact"])
	}
	if _, ok := talent["continuity_constraints"]; !ok {
		t.Errorf("%s: missing continuity_constraints", path)
	}
}

func TestOrgProofExamplesAllJSONHasSchemaVersion(t *testing.T) {
	root := repoRoot(t)
	proofRoot := filepath.Join(root, "examples", "org-proofs")

	err := filepath.WalkDir(proofRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		artifact := readJSONMap(t, path)
		if artifact["schema_version"] == "" {
			t.Errorf("%s: missing schema_version", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", proofRoot, err)
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatalf("%s: invalid JSON: %v", path, err)
	}
	return artifact
}

func jsonObjectArrayContains(items []any, key, want string) bool {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && object[key] == want {
			return true
		}
	}
	return false
}
