package temporal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/donovan-yohan/belayer/internal/v2/model"
)

type WorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *WorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	// Register activity struct for mocking via method references.
	s.env.RegisterActivity(&Activities{})
}

func (s *WorkflowTestSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

func TestWorkflowSuite(t *testing.T) {
	suite.Run(t, new(WorkflowTestSuite))
}

func (s *WorkflowTestSuite) TestRouteWorkflow_SetterThenLead_BothFinish() {
	var a *Activities

	// Mock the spawn activities — they return immediately.
	s.env.OnActivity(a.TypeBSpawnActivity, mock.Anything, mock.MatchedBy(func(input TypeBSpawnInput) bool {
		return input.Role.Name == "setter"
	})).Return(&TypeBSpawnOutput{SessionID: "setter-session", Spawned: true}, nil)

	s.env.OnActivity(a.TypeBSpawnActivity, mock.Anything, mock.MatchedBy(func(input TypeBSpawnInput) bool {
		return input.Role.Name == "lead"
	})).Return(&TypeBSpawnOutput{SessionID: "lead-session", Spawned: true}, nil)

	// Simulate CLI callbacks: setter finishes, then lead finishes.
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChannelName, model.RoleSignal{
			TaskID: "test-run",
			Role:   "setter",
			Action: model.SignalFinish,
			Output: json.RawMessage(`{"spec":"build auth system"}`),
		})
	}, time.Millisecond*100)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChannelName, model.RoleSignal{
			TaskID: "test-run",
			Role:   "lead",
			Action: model.SignalFinish,
			Output: json.RawMessage(`{"files_changed":["auth.go"]}`),
		})
	}, time.Millisecond*200)

	s.env.ExecuteWorkflow(RouteWorkflow, model.RouteInput{
		Description: "build user authentication",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.RouteOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.RunStatusCompleted, result.Status)
	s.Contains(result.RoleOutputs, "setter")
	s.Contains(result.RoleOutputs, "lead")
}

func (s *WorkflowTestSuite) TestRouteWorkflow_SetterFlares() {
	var a *Activities
	s.env.OnActivity(a.TypeBSpawnActivity, mock.Anything, mock.Anything).
		Return(&TypeBSpawnOutput{SessionID: "setter-session", Spawned: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChannelName, model.RoleSignal{
			TaskID:  "test-run",
			Role:    "setter",
			Action:  model.SignalFlare,
			Message: "stuck on requirements",
		})
	}, time.Millisecond*100)

	s.env.ExecuteWorkflow(RouteWorkflow, model.RouteInput{
		Description: "build auth",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.RouteOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.RunStatusFlared, result.Status)
	s.Contains(result.Message, "flared")
}

func (s *WorkflowTestSuite) TestRouteWorkflow_LeadFails() {
	var a *Activities

	s.env.OnActivity(a.TypeBSpawnActivity, mock.Anything, mock.MatchedBy(func(input TypeBSpawnInput) bool {
		return input.Role.Name == "setter"
	})).Return(&TypeBSpawnOutput{SessionID: "setter-session", Spawned: true}, nil)

	s.env.OnActivity(a.TypeBSpawnActivity, mock.Anything, mock.MatchedBy(func(input TypeBSpawnInput) bool {
		return input.Role.Name == "lead"
	})).Return(&TypeBSpawnOutput{SessionID: "lead-session", Spawned: true}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChannelName, model.RoleSignal{
			TaskID: "test-run",
			Role:   "setter",
			Action: model.SignalFinish,
			Output: json.RawMessage(`{"spec":"build auth"}`),
		})
	}, time.Millisecond*100)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalChannelName, model.RoleSignal{
			TaskID:  "test-run",
			Role:    "lead",
			Action:  model.SignalFail,
			Message: "cannot access repository",
		})
	}, time.Millisecond*200)

	s.env.ExecuteWorkflow(RouteWorkflow, model.RouteInput{
		Description: "build auth",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.RouteOutput
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.RunStatusFailed, result.Status)
	s.Contains(result.Message, "failed")
}

func TestHandleRoleSignal(t *testing.T) {
	tests := []struct {
		name     string
		signal   model.RoleSignal
		expected string
	}{
		{
			name: "finish",
			signal: model.RoleSignal{
				Role:   "setter",
				Action: model.SignalFinish,
				Output: json.RawMessage(`{"done":true}`),
			},
			expected: "completed",
		},
		{
			name: "flare",
			signal: model.RoleSignal{
				Role:    "lead",
				Action:  model.SignalFlare,
				Message: "help",
			},
			expected: "flared",
		},
		{
			name: "fail",
			signal: model.RoleSignal{
				Role:    "lead",
				Action:  model.SignalFail,
				Message: "broken",
			},
			expected: "failed",
		},
		{
			name: "unknown action",
			signal: model.RoleSignal{
				Role:   "lead",
				Action: "bogus",
			},
			expected: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HandleRoleSignal(tt.signal)
			if result.Status != tt.expected {
				t.Errorf("got status %q, want %q", result.Status, tt.expected)
			}
		})
	}
}
