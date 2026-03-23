package outcome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

func makeWorkDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "outcome-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileNode(outputPath string) *pipeline.NodeConfig {
	return &pipeline.NodeConfig{
		Name:   "test-node",
		Output: pipeline.OutputConfig{Type: "file", Path: outputPath},
	}
}

func codeNode() *pipeline.NodeConfig {
	return &pipeline.NodeConfig{
		Name:   "test-node",
		Output: pipeline.OutputConfig{Type: "commit"},
	}
}

// --- verdict.txt tests ---

func TestVerdictPass(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, verdictFile), "PASS\n")

	result := Detect(codeNode(), dir, 1)
	if result.Outcome != model.OutcomePass {
		t.Fatalf("expected PASS, got %s", result.Outcome)
	}
	if result.Attempt != 1 {
		t.Fatalf("expected attempt 1, got %d", result.Attempt)
	}
}

func TestVerdictFail(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, verdictFile), "FAIL\n")

	result := Detect(codeNode(), dir, 2)
	if result.Outcome != model.OutcomeFail {
		t.Fatalf("expected FAIL, got %s", result.Outcome)
	}
}

func TestVerdictRetryWithTarget(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, verdictFile), "RETRY lead\n")

	result := Detect(codeNode(), dir, 1)
	if result.Outcome != model.OutcomeRetry {
		t.Fatalf("expected RETRY, got %s", result.Outcome)
	}
	if result.TargetNode != "lead" {
		t.Fatalf("expected target 'lead', got %q", result.TargetNode)
	}
}

func TestVerdictRetryNoTarget(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, verdictFile), "RETRY\n")

	result := Detect(codeNode(), dir, 1)
	if result.Outcome != model.OutcomeRetry {
		t.Fatalf("expected RETRY, got %s", result.Outcome)
	}
	if result.TargetNode != "" {
		t.Fatalf("expected empty target, got %q", result.TargetNode)
	}
}

// --- output file first-line tests ---

func TestOutputFileFirstLinePass(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, "output.txt"), "PASS\nsome other content\n")

	result := Detect(fileNode("output.txt"), dir, 1)
	if result.Outcome != model.OutcomePass {
		t.Fatalf("expected PASS from output file first line, got %s", result.Outcome)
	}
}

func TestOutputFileFirstLineRetryWithTarget(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, "output.txt"), "RETRY review\n")

	result := Detect(fileNode("output.txt"), dir, 1)
	if result.Outcome != model.OutcomeRetry {
		t.Fatalf("expected RETRY, got %s", result.Outcome)
	}
	if result.TargetNode != "review" {
		t.Fatalf("expected target 'review', got %q", result.TargetNode)
	}
}

// --- type-based defaults ---

func TestFileExistsDefaultPass(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, "out.md"), "not a verdict keyword\n")

	result := Detect(fileNode("out.md"), dir, 1)
	if result.Outcome != model.OutcomePass {
		t.Fatalf("expected PASS (file exists, no verdict keyword), got %s", result.Outcome)
	}
}

func TestFileMissingDefaultFail(t *testing.T) {
	dir := makeWorkDir(t)

	result := Detect(fileNode("missing.md"), dir, 1)
	if result.Outcome != model.OutcomeFail {
		t.Fatalf("expected FAIL (file missing), got %s", result.Outcome)
	}
}

func TestCodeTypeDefaultPass(t *testing.T) {
	dir := makeWorkDir(t)

	result := Detect(codeNode(), dir, 1)
	if result.Outcome != model.OutcomePass {
		t.Fatalf("expected PASS for code type default, got %s", result.Outcome)
	}
}

func TestDetect_GateResultType_DefaultsToPass(t *testing.T) {
	workDir := t.TempDir()
	node := &pipeline.NodeConfig{
		Name:   "review",
		Type:   pipeline.NodeTypeGate,
		Output: pipeline.OutputConfig{Type: "gate_result"},
	}

	result := Detect(node, workDir, 0)
	// Gate result nodes default to PASS — the activity handles scoring.
	if result.Outcome != model.OutcomePass {
		t.Errorf("outcome: got %q, want %q", result.Outcome, model.OutcomePass)
	}
}

// --- pr output type tests ---

func prNode(outputPath string) *pipeline.NodeConfig {
	return &pipeline.NodeConfig{
		Name:   "summit",
		Output: pipeline.OutputConfig{Type: "pr", Path: outputPath},
	}
}

func TestDetect_PRTypeExistsDefaultPass(t *testing.T) {
	dir := makeWorkDir(t)
	writeFile(t, filepath.Join(dir, "pr.json"), `{"url":"https://github.com/test/1","number":1,"branch":"feat"}`)

	result := Detect(prNode("pr.json"), dir, 0)
	if result.Outcome != model.OutcomePass {
		t.Fatalf("expected PASS (pr.json exists), got %s", result.Outcome)
	}
	if result.OutputPath != "pr.json" {
		t.Fatalf("expected output path 'pr.json', got %q", result.OutputPath)
	}
}

func TestDetect_PRTypeMissingDefaultFail(t *testing.T) {
	dir := makeWorkDir(t)

	result := Detect(prNode("pr.json"), dir, 0)
	if result.Outcome != model.OutcomeFail {
		t.Fatalf("expected FAIL (pr.json missing), got %s", result.Outcome)
	}
}

// --- precedence: verdict.txt over output file ---

func TestVerdictTakesPrecedenceOverOutputFile(t *testing.T) {
	dir := makeWorkDir(t)
	// verdict says FAIL, output file says PASS — verdict wins
	writeFile(t, filepath.Join(dir, verdictFile), "FAIL\n")
	writeFile(t, filepath.Join(dir, "output.txt"), "PASS\n")

	result := Detect(fileNode("output.txt"), dir, 1)
	if result.Outcome != model.OutcomeFail {
		t.Fatalf("expected FAIL from verdict (takes precedence), got %s", result.Outcome)
	}
}
