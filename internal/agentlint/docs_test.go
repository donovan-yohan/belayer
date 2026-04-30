package agentlint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactSchemasAreValidJSON(t *testing.T) {
	root := repoRoot(t)
	schemaDir := filepath.Join(root, "docs", "artifact-schemas")

	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}

	seen := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		seen++
		path := filepath.Join(schemaDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Errorf("%s: invalid JSON schema: %v", path, err)
			continue
		}
		if schema["$schema"] == "" {
			t.Errorf("%s: missing $schema", path)
		}
		if schema["title"] == "" {
			t.Errorf("%s: missing title", path)
		}
		if schema["type"] != "object" {
			t.Errorf("%s: root type = %v, want object", path, schema["type"])
		}
		if _, ok := schema["required"].([]any); !ok {
			t.Errorf("%s: missing required array", path)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Errorf("%s: missing properties object", path)
			continue
		}
		if _, ok := props["schema_version"]; !ok {
			t.Errorf("%s: missing schema_version property", path)
		}
	}

	if seen < 6 {
		t.Fatalf("found %d artifact schemas, want at least 6", seen)
	}
}

func TestArtifactSchemaDocsReferenceEverySchema(t *testing.T) {
	root := repoRoot(t)
	docPath := filepath.Join(root, "docs", "ARTIFACT_SCHEMAS.md")
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	docText := string(doc)

	schemaDir := filepath.Join(root, "docs", "artifact-schemas")
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		if !strings.Contains(docText, entry.Name()) {
			t.Errorf("%s does not reference %s", docPath, entry.Name())
		}
	}
}

func TestDesignDocsIndexMarksHistoricalMaterial(t *testing.T) {
	root := repoRoot(t)
	indexPath := filepath.Join(root, "docs", "design-docs", "index.md")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read %s: %v", indexPath, err)
	}
	text := string(raw)

	required := []string{
		"Current Or Recently Implemented Designs",
		"Historical Or Superseded Designs",
		"Forward-Looking, Not Implemented",
		"start with `docs/README.md`",
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Errorf("%s missing deprecation/audit marker %q", indexPath, needle)
		}
	}
}
