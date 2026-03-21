package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func (f *fakeSpawner) Spawn(_ context.Context, opts session.SpawnOpts) error {
	result, ok := f.results[opts.NodeName]
	if !ok {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}
	result.Attempt = opts.Attempt
	completionDir := filepath.Join(f.workDir, ".belayer", "completion")
	os.MkdirAll(completionDir, 0o755)
	path := filepath.Join(completionDir, fmt.Sprintf("%s-%s-attempt-%d.json", opts.TaskID, opts.NodeName, opts.Attempt))
	data, _ := json.Marshal(result)
	return os.WriteFile(path, data, 0o644)
}

// retryThenPassSpawner returns RETRY on the first spotter call, PASS on subsequent calls.
type retryThenPassSpawner struct {
	workDir      string
	spotterCalls *int
}

func (r *retryThenPassSpawner) Spawn(_ context.Context, opts session.SpawnOpts) error {
	var result model.CompletionResult

	if opts.NodeName == "spotter" {
		*r.spotterCalls++
		if *r.spotterCalls == 1 {
			result = model.CompletionResult{
				Outcome:    model.OutcomeRetry,
				TargetNode: "lead",
				Feedback:   "needs more work",
				Attempt:    opts.Attempt,
			}
		} else {
			result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
		}
	} else {
		result = model.CompletionResult{Outcome: model.OutcomePass, Attempt: opts.Attempt}
	}

	completionDir := filepath.Join(r.workDir, ".belayer", "completion")
	os.MkdirAll(completionDir, 0o755)
	path := filepath.Join(completionDir, fmt.Sprintf("%s-%s-attempt-%d.json", opts.TaskID, opts.NodeName, opts.Attempt))
	data, _ := json.Marshal(result)
	return os.WriteFile(path, data, 0o644)
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

// writeFileAtPath creates parent dirs and writes content to a path inside workDir.
func writeFileAtPath(t *testing.T, workDir, rel, content string) {
	t.Helper()
	full := filepath.Join(workDir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

// integrationPipeline returns a minimal 3-node pipeline YAML for integration testing.
// Output nodes write to known paths so fakeSpawner can pre-seed them.
func integrationPipeline() []byte {
	return []byte(pipeline.DefaultPipelineYAML)
}

// TestEndToEnd_AllPass: fakeSpawner returns PASS for every node → ClimbCompleted.
func (s *IntegrationTestSuite) TestEndToEnd_AllPass() {
	workDir := s.T().TempDir()

	// Pre-seed output files that the workflow checks for file-type nodes.
	writeFileAtPath(s.T(), workDir, ".belayer/output/plan.md", "PASS\nHere is the plan.")
	writeFileAtPath(s.T(), workDir, ".belayer/output/review.md", "PASS\nLooks good.")

	spawner := &fakeSpawner{
		workDir: workDir,
		results: map[string]model.CompletionResult{
			"setter":  {Outcome: model.OutcomePass, OutputPath: filepath.Join(workDir, ".belayer/output/plan.md")},
			"lead":    {Outcome: model.OutcomePass},
			"spotter": {Outcome: model.OutcomePass, OutputPath: filepath.Join(workDir, ".belayer/output/review.md")},
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

	writeFileAtPath(s.T(), workDir, ".belayer/output/plan.md", "PASS\nHere is the plan.")
	writeFileAtPath(s.T(), workDir, ".belayer/output/review.md", "PASS\nLooks good.")

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
