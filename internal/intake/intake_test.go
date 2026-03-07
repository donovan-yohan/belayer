package intake

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutor simulates agentic node execution for testing.
type mockExecutor struct {
	responses map[string]string // keyword -> JSON response
	calls     []string          // recorded prompts
}

func (m *mockExecutor) Execute(ctx context.Context, prompt string) (string, error) {
	m.calls = append(m.calls, prompt)
	for keyword, response := range m.responses {
		if strings.Contains(strings.ToLower(prompt), keyword) {
			return response, nil
		}
	}
	return `{"result": "mock"}`, nil
}

func newSufficientExecutor() *mockExecutor {
	return &mockExecutor{
		responses: map[string]string{
			"sufficiency": `{"sufficient": true, "gaps": []}`,
		},
	}
}

func newInsufficientExecutor() *mockExecutor {
	return &mockExecutor{
		responses: map[string]string{
			"sufficiency": `{"sufficient": false, "gaps": ["What API endpoints are needed?", "Which database schema changes?"]}`,
		},
	}
}

func TestParseJiraTickets(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"PROJ-123", []string{"PROJ-123"}},
		{"PROJ-123,PROJ-456", []string{"PROJ-123", "PROJ-456"}},
		{"PROJ-123, PROJ-456, PROJ-789", []string{"PROJ-123", "PROJ-456", "PROJ-789"}},
		{" PROJ-123 , PROJ-456 ", []string{"PROJ-123", "PROJ-456"}},
		{",,,", nil},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			result := ParseJiraTickets(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPipeline_TextInput_Sufficient(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description: "Add user authentication to the API",
		RepoNames:   []string{"api", "frontend"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	assert.Equal(t, "Add user authentication to the API", result.Description)
	assert.Equal(t, "text", result.Source)
	assert.Equal(t, "", result.SourceRef)
	assert.True(t, result.SufficiencyChecked)

	// Should have called sufficiency check
	require.Len(t, exec.calls, 1)
	assert.Contains(t, exec.calls[0], "sufficiency")
	assert.Contains(t, exec.calls[0], "api, frontend")
}

func TestPipeline_JiraInput_SingleTicket(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		JiraTickets: []string{"PROJ-123"},
		RepoNames:   []string{"api"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	assert.Equal(t, "Jira tickets: PROJ-123", result.Description)
	assert.Equal(t, "jira", result.Source)
	assert.Equal(t, "PROJ-123", result.SourceRef)
	assert.True(t, result.SufficiencyChecked)
}

func TestPipeline_JiraInput_MultipleTickets(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		JiraTickets: []string{"PROJ-123", "PROJ-456", "PROJ-789"},
		RepoNames:   []string{"api", "frontend"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	assert.Equal(t, "Jira tickets: PROJ-123, PROJ-456, PROJ-789", result.Description)
	assert.Equal(t, "jira", result.Source)
	assert.Equal(t, "PROJ-123,PROJ-456,PROJ-789", result.SourceRef)
}

func TestPipeline_JiraInput_WithAdditionalDescription(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description: "Focus on the backend changes",
		JiraTickets: []string{"PROJ-123"},
		RepoNames:   []string{"api"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	assert.Contains(t, result.Description, "Jira tickets: PROJ-123")
	assert.Contains(t, result.Description, "Focus on the backend changes")
}

func TestPipeline_Insufficient_Brainstorm(t *testing.T) {
	exec := newInsufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	userInput := "REST endpoints for CRUD\nAdd users table with email column\n"
	cfg := PipelineConfig{
		Description: "Add user management",
		RepoNames:   []string{"api"},
		Stdin:       strings.NewReader(userInput),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	assert.Contains(t, result.Description, "Add user management")
	assert.Contains(t, result.Description, "REST endpoints for CRUD")
	assert.Contains(t, result.Description, "Add users table with email column")
	assert.Contains(t, result.Description, "Additional context from brainstorm")
	assert.True(t, result.SufficiencyChecked)

	// Stdout should show the Q&A prompts
	assert.Contains(t, stdout.String(), "Q1:")
	assert.Contains(t, stdout.String(), "Q2:")
}

func TestPipeline_Insufficient_NoBrainstorm(t *testing.T) {
	exec := newInsufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description:  "Add user management",
		RepoNames:    []string{"api"},
		NoBrainstorm: true,
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	// Description should not be enriched
	assert.Equal(t, "Add user management", result.Description)
	assert.True(t, result.SufficiencyChecked)

	// Should show gap listing
	assert.Contains(t, stdout.String(), "brainstorm skipped")
}

func TestPipeline_SufficiencyCheckFails(t *testing.T) {
	pipeline := NewPipeline(&failingExecutor{})

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description: "Add feature",
		RepoNames:   []string{"api"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err) // Pipeline should not fail — just skip sufficiency

	assert.Equal(t, "Add feature", result.Description)
	assert.False(t, result.SufficiencyChecked)
	assert.Contains(t, stdout.String(), "Warning: sufficiency check failed")
}

// failingExecutor always returns an error.
type failingExecutor struct{}

func (f *failingExecutor) Execute(ctx context.Context, prompt string) (string, error) {
	return "", fmt.Errorf("claude not available")
}

func TestPipeline_Brainstorm_EOFHandling(t *testing.T) {
	exec := newInsufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	// Empty stdin — simulates EOF immediately
	cfg := PipelineConfig{
		Description: "Add user management",
		RepoNames:   []string{"api"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	result, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	// Description should remain unchanged when no answers provided
	assert.Equal(t, "Add user management", result.Description)
}

func TestPipeline_RepoNamesInSufficiencyPrompt(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description: "Add feature",
		RepoNames:   []string{"api", "frontend", "shared-lib"},
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	_, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	require.Len(t, exec.calls, 1)
	assert.Contains(t, exec.calls[0], "api, frontend, shared-lib")
}

func TestPipeline_NoRepoNames(t *testing.T) {
	exec := newSufficientExecutor()
	pipeline := NewPipeline(exec)

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		Description: "Add feature",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
	}

	_, err := pipeline.Run(context.Background(), cfg)
	require.NoError(t, err)

	require.Len(t, exec.calls, 1)
	assert.Contains(t, exec.calls[0], "none specified")
}
