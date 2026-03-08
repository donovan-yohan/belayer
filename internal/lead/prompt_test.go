package lead

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPrompt_ContainsAllFields(t *testing.T) {
	data := PromptData{
		Spec:        "Build a REST API with user authentication",
		GoalID:      "api-1",
		RepoName:    "api",
		Description: "Add /users endpoint with role field",
	}

	prompt, err := BuildPrompt(data)
	require.NoError(t, err)

	assert.Contains(t, prompt, data.Spec)
	assert.Contains(t, prompt, data.GoalID)
	assert.Contains(t, prompt, data.RepoName)
	assert.Contains(t, prompt, data.Description)
	assert.Contains(t, prompt, "DONE.json")
	assert.Contains(t, prompt, `"status": "complete"`)
	assert.Contains(t, prompt, `"status": "failed"`)
	assert.Contains(t, prompt, "files_changed")
}

func TestBuildPrompt_SpecWithSpecialChars(t *testing.T) {
	data := PromptData{
		Spec:        "Use {{braces}} and $variables and `backticks`",
		GoalID:      "test-1",
		RepoName:    "repo",
		Description: "Handle special characters",
	}

	prompt, err := BuildPrompt(data)
	require.NoError(t, err)

	assert.Contains(t, prompt, "{{braces}}")
	assert.Contains(t, prompt, "$variables")
	assert.Contains(t, prompt, "`backticks`")
}

func TestBuildPrompt_WithSpotterFeedback(t *testing.T) {
	prompt, err := BuildPrompt(PromptData{
		Spec:            "build a site",
		GoalID:          "setup",
		RepoName:        "frontend",
		Description:     "scaffold project",
		SpotterFeedback: "FAILED CHECKS:\n- visual_quality: Text not wrapping properly\n- console_errors: 2 errors in console",
	})
	require.NoError(t, err)
	assert.Contains(t, prompt, "Previous Attempt Feedback")
	assert.Contains(t, prompt, "Text not wrapping")
}

func TestBuildPrompt_NoSpotterFeedback(t *testing.T) {
	prompt, err := BuildPrompt(PromptData{
		Spec:        "build a site",
		GoalID:      "setup",
		RepoName:    "frontend",
		Description: "scaffold project",
	})
	require.NoError(t, err)
	assert.NotContains(t, prompt, "Previous Attempt Feedback")
}

func TestBuildPrompt_MultilineSpec(t *testing.T) {
	data := PromptData{
		Spec:        "Line 1\nLine 2\nLine 3",
		GoalID:      "ml-1",
		RepoName:    "repo",
		Description: "Test multiline",
	}

	prompt, err := BuildPrompt(data)
	require.NoError(t, err)

	assert.Contains(t, prompt, "Line 1\nLine 2\nLine 3")
	// Verify the prompt has the expected structure sections
	assert.True(t, strings.Contains(prompt, "## Task Specification"))
	assert.True(t, strings.Contains(prompt, "## Your Goal"))
	assert.True(t, strings.Contains(prompt, "## Instructions"))
	assert.True(t, strings.Contains(prompt, "## DONE.json Format"))
}
