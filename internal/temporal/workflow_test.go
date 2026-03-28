package temporal

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/model"
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
		PipelineYAML: []byte(`name: test-pipeline
nodes:
  - name: plan
    type: node
    command: echo test
    description: create plan
    input: { type: file, key: design_doc }
    output: { type: file, path: .belayer/.internal/output/plan.md }
    on_pass: next
    on_retry: plan
    on_fail: stop
    max_retries: 2
  - name: implement
    type: node
    command: echo test
    description: write code
    input: { type: file, key: plan }
    output: { type: commit }
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3
  - name: review
    type: gate
    command: echo test
    description: review code
    input: { type: commit }
    dimensions:
      - { name: spec_compliance, weight: 0.35, description: match }
      - { name: test_contracts, weight: 0.3, description: tests }
      - { name: runtime_correctness, weight: 0.35, description: works }
    thresholds: { pass: 7.0, retry: 4.0 }
    output: { type: gate_result }
    on_pass: next
    on_retry: implement
    on_fail: stop
    max_retries: 2
  - name: pr-author
    type: node
    command: echo test
    description: create PR
    input: { type: gate_result, key: review }
    output: { type: pr }
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2
`),
		WorkDir: "/tmp/test-workdir",
		Branch:  "feature/test",
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

// TestClimb_AllNodesPass: plan, implement, review all PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_AllNodesPass() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "plan"
	})).Return(passOutput("plan"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(passOutput("review"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "pr-author"
	})).Return(passOutput("pr-author"), nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_ReviewRetriesToImplement: review first returns RETRY targeting implement, then PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_ReviewRetriesToImplement() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "plan"
	})).Return(passOutput("plan"), nil)

	// Implement called twice: once initially, once after review retry.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil).Times(2)

	// Review: first call returns RETRY targeting implement.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 0
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomeRetry,
			TargetNode: "implement",
			Feedback:   "needs more work",
		},
	}, nil).Once()

	// Review: second call returns PASS.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 1
	})).Return(passOutput("review"), nil).Once()

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "pr-author"
	})).Return(passOutput("pr-author"), nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, defaultInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_NodeFails: plan FAILs → ClimbFailed with message containing "plan".
func (s *ClimbWorkflowTestSuite) TestClimb_NodeFails() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "plan"
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
	s.Contains(result.Message, "plan")
}

func gatePipelineYAML() []byte {
	return []byte(`
name: gate-test
nodes:
  - name: implement
    type: node
    command: echo test
    description: Write code
    output:
      type: commit
    on_pass: review
    on_fail: stop
    max_retries: 3
  - name: review
    type: gate
    command: echo test
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
    on_retry: implement
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

// TestClimb_GatePass: implement PASS → gate PASS → ClimbCompleted.
func (s *ClimbWorkflowTestSuite) TestClimb_GatePass() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/.internal/output/gate-result.json",
		},
	}, nil)

	s.env.ExecuteWorkflow(ClimbWorkflow, gateInput())

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.ClimbOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.ClimbCompleted, result.Status)
}

// TestClimb_GateRetryThenPass: gate RETRY → implement retries → gate PASS.
func (s *ClimbWorkflowTestSuite) TestClimb_GateRetryThenPass() {
	a := &Activities{}

	// Implement called twice: initial + after gate retry.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil).Times(2)

	// Gate: first call RETRY, second call PASS.
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 0
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomeRetry,
			TargetNode: "implement",
			Feedback:   "Fix the bugs in auth module",
		},
	}, nil).Once()

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review" && in.Attempt == 1
	})).Return(&NodeActivityOutput{
		Result: model.CompletionResult{
			Outcome:    model.OutcomePass,
			OutputPath: ".belayer/.internal/output/gate-result.json",
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
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil)

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

// TestClimb_MaxRetriesExhausted: review always RETRY → eventually ClimbFailed with "max retries".
func (s *ClimbWorkflowTestSuite) TestClimb_MaxRetriesExhausted() {
	a := &Activities{}

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "plan"
	})).Return(passOutput("plan"), nil)

	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "implement"
	})).Return(passOutput("implement"), nil)

	// Review always retries self (review.max_retries == 2 in default pipeline).
	s.env.OnActivity(a.NodeActivity, mock.Anything, mock.MatchedBy(func(in NodeActivityInput) bool {
		return in.Node.Name == "review"
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
