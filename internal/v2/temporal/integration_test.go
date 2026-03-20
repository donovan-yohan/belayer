package temporal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// mockSessionSpawner simulates spawning an interactive session.
type mockSessionSpawner struct {
	spawned []string
}

func (m *mockSessionSpawner) Spawn(_ context.Context, roleName, taskID, workDir string, input json.RawMessage) (string, error) {
	m.spawned = append(m.spawned, roleName)
	return roleName + "-session", nil
}

// mockExecProvider simulates a Type A role execution.
type mockExecProvider struct{}

func (m *mockExecProvider) Execute(_ context.Context, roleDef role.RoleDef, input json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"result":"ok","role":"` + roleDef.Name + `"}`), nil
}

// TestIntegration_WorkerRegistration verifies workflow and activity registration.
func TestIntegration_WorkerRegistration(t *testing.T) {
	server, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{})
	if err != nil {
		t.Skip("Temporal dev server not available, skipping integration test")
	}
	defer server.Stop()

	c := server.Client()
	defer c.Close()

	w := worker.New(c, TaskQueueName, worker.Options{})
	activities := &Activities{
		SessionSpawner: &mockSessionSpawner{},
		ExecProvider:   &mockExecProvider{},
	}
	w.RegisterWorkflow(RouteWorkflow)
	w.RegisterActivity(activities)

	err = w.Start()
	require.NoError(t, err)
	w.Stop()
}

// TestIntegration_FullPipeline_SetterToLead runs the full MVP pipeline
// with mock providers and CLI-callback signals against a real Temporal server.
func TestIntegration_FullPipeline_SetterToLead(t *testing.T) {
	server, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{})
	if err != nil {
		t.Skip("Temporal dev server not available, skipping integration test")
	}
	defer server.Stop()

	c := server.Client()
	defer c.Close()

	spawner := &mockSessionSpawner{}
	activities := &Activities{
		SessionSpawner: spawner,
		ExecProvider:   &mockExecProvider{},
	}

	w := worker.New(c, TaskQueueName, worker.Options{})
	w.RegisterWorkflow(RouteWorkflow)
	w.RegisterActivity(activities)
	require.NoError(t, w.Start())
	defer w.Stop()

	ctx := context.Background()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        "integration-test-setter-lead",
		TaskQueue: TaskQueueName,
	}, RouteWorkflow, model.RouteInput{
		Description: "integration test: build auth",
	})
	require.NoError(t, err)

	// Simulate setter finishing via CLI callback.
	err = c.SignalWorkflow(ctx, run.GetID(), run.GetRunID(), SignalChannelName, model.RoleSignal{
		TaskID: run.GetID(),
		Role:   "setter",
		Action: model.SignalFinish,
		Output: json.RawMessage(`{"spec":"build auth system"}`),
	})
	require.NoError(t, err)

	// Simulate lead finishing via CLI callback.
	err = c.SignalWorkflow(ctx, run.GetID(), run.GetRunID(), SignalChannelName, model.RoleSignal{
		TaskID: run.GetID(),
		Role:   "lead",
		Action: model.SignalFinish,
		Output: json.RawMessage(`{"files_changed":["auth.go","auth_test.go"]}`),
	})
	require.NoError(t, err)

	// Wait for workflow completion.
	var result model.RouteOutput
	require.NoError(t, run.Get(ctx, &result))

	assert.Equal(t, model.RunStatusCompleted, result.Status)
	assert.Contains(t, result.RoleOutputs, "setter")
	assert.Contains(t, result.RoleOutputs, "lead")
	assert.Len(t, spawner.spawned, 2)
	assert.Equal(t, "setter", spawner.spawned[0])
	assert.Equal(t, "lead", spawner.spawned[1])
}
