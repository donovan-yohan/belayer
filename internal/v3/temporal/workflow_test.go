package temporal

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

type ClimbWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *ClimbWorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *ClimbWorkflowTestSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

func TestClimbWorkflowSuite(t *testing.T) {
	suite.Run(t, new(ClimbWorkflowTestSuite))
}

func defaultInput() model.ClimbInput {
	return model.ClimbInput{
		PipelineYAML: []byte(pipeline.DefaultPipelineYAML),
		WorkDir:      "/tmp/test-workdir",
		Branch:       "feature/test",
	}
}

func passOutput(nodeName string) *NodeActivityOutput {
	return &NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: "/tmp/" + nodeName + "-output.md",
		},
	}
}

// TestClimb_AllNodesPass: setter, lead, spotter all PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_AllNodesPass() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "setter"
	})).Return(passOutput("setter"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "spotter"
	})).Return(passOutput("spotter"), nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_SpotterRetriesToLead: spotter first returns RETRY targeting lead, then PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_SpotterRetriesToLead() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "setter"
	})).Return(passOutput("setter"), nil)

	// Lead called twice: once initially, once after spotter retry.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil).Times(2)

	// Spotter: first call returns RETRY targeting lead.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "spotter" && in.Attempt == 0
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomeRetry,
			TargetNode: "lead",
			Feedback:   "needs more work",
		},
	}, nil).Once()

	// Spotter: second call returns PASS.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "spotter" && in.Attempt == 1
	})).Return(passOutput("spotter"), nil).Once()

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_NodeFails: setter FAILs → ClimbFailed with message containing "setter".
func (s *ClimbWorkflowTestSuite) TestClimb_NodeFails() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "setter"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome: model.OutcomeFail,
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
	s.Contains(result.Message, "setter")
}

func gatePipelineYAML() []byte {
	return []byte(`
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
      path: .belayer/output/gate-result.json
      rationale_path: .belayer/output/rationale.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`)
}

func gateInput() model.ClimbInput {
	return model.ClimbInput{
		PipelineYAML: gatePipelineYAML(),
		WorkDir:      "/tmp/test-gate",
		Branch:       "feature/gate-test",
	}
}

// TestClimb_GatePass: lead PASS → gate PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_GatePass() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/output/gate-result.json",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_GateRetryThenPass: gate RETRY → lead retries → gate PASS.
func (s *ClimbWorkflowTestSuite) TestClimb_GateRetryThenPass() {
	a := &Activities{}

	// Lead called twice: initial + after gate retry.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil).Times(2)

	// Gate: first call RETRY, second call PASS.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 0
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomeRetry,
			TargetNode: "lead",
			Feedback:   "Fix the bugs in auth module",
		},
	}, nil).Once()

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 1
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/output/gate-result.json",
		},
	}, nil).Once()

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_GateFail: gate FAIL → ClimbFailed.
func (s *ClimbWorkflowTestSuite) TestClimb_GateFail() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:  model.OutcomeFail,
			Feedback: "Fundamentally broken",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
}

// TestClimb_MaxRetriesExhausted: spotter always RETRY → eventually ClimbFailed with "max retries".
func (s *ClimbWorkflowTestSuite) TestClimb_MaxRetriesExhausted() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "setter"
	})).Return(passOutput("setter"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "lead"
	})).Return(passOutput("lead"), nil)

	// Spotter always retries self (spotter.max_retries == 2 in default pipeline).
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "spotter"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:  model.OutcomeRetry,
			Feedback: "not good enough",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbFailed, result.Status)
	s.Contains(result.Message, "max retries")
}
