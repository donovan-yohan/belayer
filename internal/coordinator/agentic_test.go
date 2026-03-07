package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgenticTestDB sets up an in-memory database with a task record for agentic tests.
// It reuses setupTestDB (from store_test.go) and adds a task prerequisite.
func setupAgenticTestDB(t *testing.T) *Store {
	t.Helper()
	store := setupTestDB(t)

	task := &model.Task{
		ID:          "task-1",
		InstanceID:  "test-instance",
		Description: "test task for agentic nodes",
		Source:      "text",
		SourceRef:   "",
		Status:      model.TaskStatusPending,
	}
	require.NoError(t, store.InsertTask(task))

	return store
}

// setupMockClaude creates a mock claude script in a temp directory and prepends
// it to PATH so that exec.Command("claude", ...) resolves to the mock.
func setupMockClaude(t *testing.T) {
	t.Helper()
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
# Find the prompt in arguments (last positional arg after flags)
PROMPT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -p) shift ;; # skip -p flag
        --model|--output-format) shift; shift ;; # skip flag and value
        *) PROMPT="$1"; shift ;;
    esac
done

# Return appropriate JSON based on prompt content
if echo "$PROMPT" | grep -qi "sufficiency"; then
    echo '{"sufficient": true, "gaps": []}'
elif echo "$PROMPT" | grep -qi "decompos"; then
    echo '{"repos": [{"name": "api", "spec": "implement the feature"}]}'
elif echo "$PROMPT" | grep -qi "alignment"; then
    echo '{"pass": true, "feedback": "all repos aligned", "criteria": [{"name": "api_contract", "pass": true, "details": "consistent"}, {"name": "shared_types", "pass": true, "details": "compatible"}, {"name": "feature_parity", "pass": true, "details": "complete"}, {"name": "integration_points", "pass": true, "details": "aligned"}], "misaligned_repos": []}'
elif echo "$PROMPT" | grep -qi "stuck"; then
    echo '{"diagnosis": "test", "recovery": "retry", "should_retry": true}'
else
    echo '{"result": "mock response"}'
fi
`
	err := os.WriteFile(mockScript, []byte(script), 0755)
	require.NoError(t, err)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))
}

// setupFailingMockClaude creates a mock claude that exits with a non-zero code
// and writes to stderr.
func setupFailingMockClaude(t *testing.T) {
	t.Helper()
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
echo "partial output" >&1
echo "something went wrong" >&2
exit 1
`
	err := os.WriteFile(mockScript, []byte(script), 0755)
	require.NoError(t, err)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))
}

func TestNewAgenticNode(t *testing.T) {
	store := setupAgenticTestDB(t)

	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")

	assert.NotNil(t, node)
	assert.Equal(t, model.AgenticSufficiency, node.nodeType)
	assert.Equal(t, "claude-sonnet-4-6", node.model)
	assert.Equal(t, store, node.store)
}

func TestExecuteSufficiency(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")

	ctx := context.Background()
	result, err := node.Execute(ctx, "task-1", "Check sufficiency of this work item")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Raw, `"sufficient": true`)
	assert.Contains(t, result.Raw, `"gaps": []`)
	assert.Greater(t, result.Duration, time.Duration(0))

	// Verify decision was stored in the database.
	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)

	d := decisions[0]
	assert.Equal(t, "task-1", d.TaskID)
	assert.Equal(t, model.AgenticSufficiency, d.NodeType)
	assert.Equal(t, "Check sufficiency of this work item", d.Input)
	assert.Contains(t, d.Output, `"sufficient": true`)
	assert.Equal(t, "claude-sonnet-4-6", d.Model)
	assert.GreaterOrEqual(t, d.DurationMs, int64(0))
}

func TestExecuteDecomposition(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	node := NewAgenticNode(store, model.AgenticDecomposition, "claude-sonnet-4-6")

	ctx := context.Background()
	result, err := node.Execute(ctx, "task-1", "Decompose this task into repos")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Raw, `"repos"`)
	assert.Contains(t, result.Raw, `"api"`)
	assert.Contains(t, result.Raw, `"implement the feature"`)

	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Equal(t, model.AgenticDecomposition, decisions[0].NodeType)
}

func TestExecuteAlignment(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	node := NewAgenticNode(store, model.AgenticAlignment, "claude-sonnet-4-6")

	ctx := context.Background()
	result, err := node.Execute(ctx, "task-1", "Check alignment across repos")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Raw, `"pass": true`)
	assert.Contains(t, result.Raw, `"api_contract"`)

	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Equal(t, model.AgenticAlignment, decisions[0].NodeType)
}

func TestExecuteStuckAnalysis(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	node := NewAgenticNode(store, model.AgenticStuckAnalysis, "claude-sonnet-4-6")

	ctx := context.Background()
	result, err := node.Execute(ctx, "task-1", "Analyze stuck lead situation")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Raw, `"diagnosis": "test"`)
	assert.Contains(t, result.Raw, `"recovery": "retry"`)
	assert.Contains(t, result.Raw, `"should_retry": true`)

	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Equal(t, model.AgenticStuckAnalysis, decisions[0].NodeType)
}

func TestExecuteStoresDecisionOnFailure(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupFailingMockClaude(t)

	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")

	ctx := context.Background()
	result, err := node.Execute(ctx, "task-1", "Check sufficiency")

	// Command failed, so we expect an error.
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "command failed")

	// Even on failure, the decision should be stored with error info.
	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)

	d := decisions[0]
	assert.Equal(t, model.AgenticSufficiency, d.NodeType)
	assert.Contains(t, d.Output, "error:")
	assert.Contains(t, d.Output, "partial output")
	assert.Contains(t, d.Output, "something went wrong")
}

func TestExecuteContextCancellation(t *testing.T) {
	store := setupAgenticTestDB(t)

	// Create a mock claude that sleeps for a long time.
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
sleep 30
echo '{"result": "should not reach"}'
`
	err := os.WriteFile(mockScript, []byte(script), 0755)
	require.NoError(t, err)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := node.Execute(ctx, "task-1", "Check sufficiency")

	require.Error(t, err)
	assert.Nil(t, result)

	// Decision should still be recorded (with error info).
	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Contains(t, decisions[0].Output, "error:")
}

func TestExecuteMultipleDecisionsForSameTask(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	ctx := context.Background()

	// Run sufficiency check.
	suffNode := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")
	result1, err := suffNode.Execute(ctx, "task-1", "Check sufficiency of requirement")
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Run decomposition.
	decompNode := NewAgenticNode(store, model.AgenticDecomposition, "claude-sonnet-4-6")
	result2, err := decompNode.Execute(ctx, "task-1", "Decompose this task")
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Both decisions should be stored.
	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 2)

	// Verify they have different node types.
	nodeTypes := map[model.AgenticNodeType]bool{}
	for _, d := range decisions {
		nodeTypes[d.NodeType] = true
	}
	assert.True(t, nodeTypes[model.AgenticSufficiency])
	assert.True(t, nodeTypes[model.AgenticDecomposition])
}

func TestExecuteDecisionIDUniqueness(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	ctx := context.Background()
	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")

	_, err := node.Execute(ctx, "task-1", "First sufficiency check")
	require.NoError(t, err)

	_, err = node.Execute(ctx, "task-1", "Second sufficiency check")
	require.NoError(t, err)

	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 2)

	// IDs should be unique.
	assert.NotEqual(t, decisions[0].ID, decisions[1].ID)
}

func TestExecuteRecordsDuration(t *testing.T) {
	store := setupAgenticTestDB(t)

	// Create a mock that takes a measurable amount of time.
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
sleep 0.1
echo '{"result": "delayed"}'
`
	err := os.WriteFile(mockScript, []byte(script), 0755)
	require.NoError(t, err)
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")
	ctx := context.Background()

	result, err := node.Execute(ctx, "task-1", "Check sufficiency")
	require.NoError(t, err)

	// Duration should be at least ~100ms.
	assert.GreaterOrEqual(t, result.Duration.Milliseconds(), int64(50))

	// Same for the stored decision.
	decisions, err := store.GetAgenticDecisionsForTask("task-1")
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.GreaterOrEqual(t, decisions[0].DurationMs, int64(50))
}

func TestExecuteDefaultPromptFallback(t *testing.T) {
	store := setupAgenticTestDB(t)
	setupMockClaude(t)

	// Use a prompt that doesn't match any keyword — should get the default response.
	node := NewAgenticNode(store, model.AgenticSufficiency, "claude-sonnet-4-6")
	ctx := context.Background()

	result, err := node.Execute(ctx, "task-1", "do something generic")
	require.NoError(t, err)
	assert.Contains(t, result.Raw, `"result": "mock response"`)
}
