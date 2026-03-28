package pipeline

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDocExamples_ParseYAML reads pipeline YAML examples from the three-phase
// architecture design doc and verifies they parse correctly. This catches drift
// between documentation and the pipeline parser.
func TestDocExamples_ParseYAML(t *testing.T) {
	designDoc := "../../docs/design-docs/2026-03-25-three-phase-architecture-design.md"
	data, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("design doc not found at %s: %v", designDoc, err)
	}

	// Extract YAML code blocks from the markdown.
	yamlBlockRe := regexp.MustCompile("(?s)```yaml\\s*\\n(.*?)```")
	matches := yamlBlockRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no YAML blocks found in design doc")
	}

	var pipelineBlocks []string
	for _, m := range matches {
		block := m[1]
		// Only test blocks that look like pipeline configs (have nodes: key).
		if strings.Contains(block, "nodes:") {
			pipelineBlocks = append(pipelineBlocks, block)
		}
	}

	if len(pipelineBlocks) == 0 {
		t.Fatal("no pipeline YAML blocks found in design doc (expected blocks with 'nodes:' key)")
	}

	for i, block := range pipelineBlocks {
		// Use ParsePipelineNoValidate because:
		// 1. Some examples use future fields (top-level setter:/spotter:) that
		//    yaml.v3 silently ignores but the validator doesn't know about.
		// 2. Some examples may reference node names in on_retry/on_pass that
		//    only make sense in the context of the full pipeline.
		cfg, err := ParsePipelineNoValidate([]byte(block))
		if err != nil {
			t.Errorf("pipeline YAML block %d failed to parse: %v\n--- block ---\n%s", i+1, err, block)
			continue
		}
		if len(cfg.Nodes) == 0 {
			t.Errorf("pipeline YAML block %d parsed but has no nodes", i+1)
		}
		t.Logf("block %d: parsed pipeline %q with %d nodes", i+1, cfg.Name, len(cfg.Nodes))
	}

	t.Logf("successfully parsed %d pipeline YAML blocks from design doc", len(pipelineBlocks))
}
