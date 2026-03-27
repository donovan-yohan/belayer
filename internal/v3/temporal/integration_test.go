package temporal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
)

// fakeSpawner implements session.Spawner by immediately writing completion files,
// simulating a Claude session that finishes instantly.
type fakeSpawner struct {
	workDir string
	results map[string]model.CompletionResult // node name → result
}

func (f *fakeSpawner) Spawn(_ context.Context, opts session.SpawnOpts) (<-chan error, error) {
	result, ok := f.results[opts.NodeName]
	if !ok {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}
	result.Attempt = opts.Attempt
	completionDir := session.CompletionDir(f.workDir)
	os.MkdirAll(completionDir, 0o755)
	path := session.CompletionFilePath(f.workDir, opts.TaskID, opts.NodeName, opts.Attempt)
	data, _ := json.Marshal(result)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return nil, nil
}

// retryThenPassSpawner returns RETRY on the first spotter call, PASS on subsequent calls.
// For gate nodes, it also writes gate output files with appropriate scores.
type retryThenPassSpawner struct {
	workDir      string
	spotterCalls *int
}

func (r *retryThenPassSpawner) Spawn(_ context.Context, opts session.SpawnOpts) (<-chan error, error) {
	var result model.CompletionResult

	if opts.NodeName == "spotter" {
		*r.spotterCalls++
		outputDir := session.OutputDir(r.workDir)
		os.MkdirAll(outputDir, 0o755)
		if *r.spotterCalls == 1 {
			// Write gate files that score in RETRY range (5.0, between retry=4.0 and pass=7.0).
			os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(`{
				"gate": "spotter", "attempt": 0,
				"dimensions": {
					"spec_compliance": {"score": 5, "rationale": "issues", "issues": ["gap"]},
					"test_contracts": {"score": 5, "rationale": "minimal", "issues": []},
					"runtime_correctness": {"score": 5, "rationale": "concerns", "issues": []}
				},
				"weighted_score": 5.0, "outcome": "RETRY", "summary": "Needs work"
			}`), 0o644)
			os.WriteFile(filepath.Join(outputDir, "rationale.md"), []byte("# Review\nFix bugs."), 0o644)
			result = model.CompletionResult{Outcome: model.OutcomeRetry, Attempt: opts.Attempt}
		} else {
			// Write gate files that score in PASS range (8.0 >= pass=7.0).
			os.WriteFile(filepath.Join(outputDir, "gate-result.json"), []byte(`{
				"gate": "spotter", "attempt": 0,
				"dimensions": {
					"spec_compliance": {"score": 8, "rationale": "ok", "issues": []},
					"test_contracts": {"score": 8, "rationale": "ok", "issues": []},
					"runtime_correctness": {"score": 8, "rationale": "ok", "issues": []}
				},
				"weighted_score": 8.0, "outcome": "PASS", "summary": "Good"
			}`), 0o644)
			os.WriteFile(filepath.Join(outputDir, "rationale.md"), []byte("# Review\nLooks good."), 0o644)
			result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
		}
	} else {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}

	completionDir := session.CompletionDir(r.workDir)
	os.MkdirAll(completionDir, 0o755)
	path := session.CompletionFilePath(r.workDir, opts.TaskID, opts.NodeName, opts.Attempt)
	data, _ := json.Marshal(result)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return nil, nil
}

// --- test suite ---

type IntegrationTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *IntegrationTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *IntegrationTestSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

// --- helpers ---

// initGitRepo initializes a git repo with an initial commit so that
// materializeCodeInput (git diff) works in tests.
func initGitRepo(t *testing.T, workDir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

// writeFileAtPath creates parent dirs and writes content to a path inside workDir.
func writeFileAtPath(t *testing.T, workDir, rel, content string) {
	t.Helper()
	full := filepath.Join(workDir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

// integrationPipeline returns a minimal 4-node pipeline YAML for integration testing.
// Output nodes write to known paths so fakeSpawner can pre-seed them.
func integrationPipeline() []byte {
	return []byte(`name: test-pipeline
intake:
  - name: user-session
    type: interactive
nodes:
  - name: setter
    type: node
    command: echo test
    description: plan
    input:
      type: file
      key: design_doc
    output:
      type: file
      path: .belayer/.internal/output/plan.md
    on_pass: next
    on_retry: setter
    on_fail: stop
    max_retries: 2

  - name: lead
    type: node
    command: echo test
    description: implement
    input:
      type: file
      key: setter
    output:
      type: commit
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: spotter
    type: gate
    command: echo test
    description: review
    input:
      type: commit
    dimensions:
      - name: spec_compliance
        description: match
        weight: 0.35
      - name: test_contracts
        description: tests
        weight: 0.3
      - name: runtime_correctness
        description: works
        weight: 0.35
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2

  - name: summit
    type: node
    command: echo test
    description: PR
    input:
      type: gate_result
      key: spotter
    output:
      type: pr
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2

safety:
  max_concurrent_runs: 3
`)
}

// TestEndToEnd_AllPass: fakeSpawner returns PASS for every node → ClimbCompleted.
func (s *IntegrationTestSuite) TestEndToEnd_AllPass() {
	workDir := s.T().TempDir()
	initGitRepo(s.T(), workDir)

	// Pre-seed output files that the workflow checks for file-type nodes.
	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/plan.md", "PASS\nHere is the plan.")
	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/review.md", "PASS\nLooks good.")
	// Pre-seed gate output files for the spotter gate node.
	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/gate-result.json", `{
		"gate": "spotter", "attempt": 0,
		"dimensions": {
			"spec_compliance": {"score": 8, "rationale": "ok", "issues": []},
			"test_contracts": {"score": 8, "rationale": "ok", "issues": []},
			"runtime_correctness": {"score": 8, "rationale": "ok", "issues": []}
		},
		"weighted_score": 8.0, "outcome": "PASS", "summary": "Good"
	}`)
	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/rationale.md", "# Review\nLooks good.")

	spawner := &fakeSpawner{
		workDir: workDir,
		results: map[string]model.CompletionResult{
			"setter":  {Outcome: model.OutcomePass, OutputPath: ".belayer/.internal/output/plan.md"},
			"lead":    {Outcome: model.OutcomePass},
			"spotter": {Outcome: model.OutcomePass, OutputPath: ".belayer/.internal/output/review.md"},
		},
	}

	acts := &Activities{Spawner: spawner}
	s.env.RegisterActivity(acts)

	input := model.ClimbInput{
		PipelineYAML: integrationPipeline(),
		WorkDir:      workDir,
		Branch:       "feature/integration-test",
	}

	s.env.ExecuteWorkflow(ClimbWorkflow, input)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestEndToEnd_RetryLoop: spotter returns RETRY on first call, PASS on second → ClimbCompleted.
func (s *IntegrationTestSuite) TestEndToEnd_RetryLoop() {
	workDir := s.T().TempDir()
	initGitRepo(s.T(), workDir)

	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/plan.md", "PASS\nHere is the plan.")
	writeFileAtPath(s.T(), workDir, ".belayer/.internal/output/review.md", "PASS\nLooks good.")

	calls := 0
	spawner := &retryThenPassSpawner{
		workDir:      workDir,
		spotterCalls: &calls,
	}

	acts := &Activities{Spawner: spawner}
	s.env.RegisterActivity(acts)

	input := model.ClimbInput{
		PipelineYAML: integrationPipeline(),
		WorkDir:      workDir,
		Branch:       "feature/retry-test",
	}

	s.env.ExecuteWorkflow(ClimbWorkflow, input)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
	s.Equal(2, calls, "spotter should have been called twice (retry + pass)")
}

// TestIntegration_GatePipeline verifies gate pipeline parsing and validation end-to-end.
func TestIntegration_GatePipeline(t *testing.T) {
	pipelineYAML := []byte(`
name: gate-test
nodes:
  - name: lead
    type: node
    description: Write code
    output:
      type: commit
    on_pass: review
    on_fail: stop
    max_retries: 3
  - name: review
    type: gate
    description: Review the code
    input:
      type: commit
    dimensions:
      - name: correctness
        description: "Does it work?"
        weight: 0.6
      - name: quality
        description: "Is it clean?"
        weight: 0.4
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
      path: .belayer/.internal/output/gate-result.json
      rationale_path: .belayer/.internal/output/rationale.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`)

	cfg, err := pipeline.ParsePipeline(pipelineYAML)
	if err != nil {
		t.Fatalf("parse pipeline: %v", err)
	}
	if err := pipeline.Validate(cfg); err != nil {
		t.Fatalf("validate pipeline: %v", err)
	}

	// Verify gate node parsed correctly
	gate := cfg.FindNode("review")
	if gate == nil {
		t.Fatal("expected 'review' gate node")
	}
	if !gate.IsGate() {
		t.Fatal("expected review to be a gate")
	}
	if len(gate.Dimensions) != 2 {
		t.Fatalf("dimensions: got %d, want 2", len(gate.Dimensions))
	}
	if gate.Thresholds.Pass != 7.0 {
		t.Errorf("pass threshold: got %f, want 7.0", gate.Thresholds.Pass)
	}
	if gate.Output.Type != "gate_result" {
		t.Errorf("output type: got %q, want gate_result", gate.Output.Type)
	}
}
