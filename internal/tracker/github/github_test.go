package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGHIssueJSON(t *testing.T) {
	raw := []byte(`{
		"number": 42,
		"title": "Fix the thing",
		"body": "Some body text",
		"labels": [{"name": "bug"}, {"name": "urgent"}],
		"assignees": [{"login": "alice"}, {"login": "bob"}],
		"comments": [
			{"author": {"login": "alice"}, "body": "Looks good", "createdAt": "2024-01-01T00:00:00Z"}
		],
		"url": "https://github.com/owner/repo/issues/42"
	}`)

	issue, err := parseGHIssueJSON(raw)
	require.NoError(t, err)

	assert.Equal(t, "#42", issue.ID)
	assert.Equal(t, "Fix the thing", issue.Title)
	assert.Equal(t, "Some body text", issue.Body)
	assert.Equal(t, []string{"bug", "urgent"}, issue.Labels)
	assert.Equal(t, "alice", issue.Assignee) // first assignee
	assert.Len(t, issue.Comments, 1)
	assert.Equal(t, "alice", issue.Comments[0].Author)
	assert.Equal(t, "Looks good", issue.Comments[0].Body)
	assert.Equal(t, "https://github.com/owner/repo/issues/42", issue.URL)
}

func TestParseGHIssueListJSON(t *testing.T) {
	raw := []byte(`[
		{
			"number": 1,
			"title": "First issue",
			"body": "Body 1",
			"labels": [{"name": "feature"}],
			"assignees": [],
			"comments": [],
			"url": "https://github.com/owner/repo/issues/1"
		},
		{
			"number": 2,
			"title": "Second issue",
			"body": "Body 2",
			"labels": [],
			"assignees": [{"login": "bob"}],
			"comments": [],
			"url": "https://github.com/owner/repo/issues/2"
		}
	]`)

	issues, err := parseGHIssueListJSON(raw)
	require.NoError(t, err)

	assert.Len(t, issues, 2)
	assert.Equal(t, "#1", issues[0].ID)
	assert.Equal(t, "First issue", issues[0].Title)
	assert.Equal(t, []string{"feature"}, issues[0].Labels)
	assert.Equal(t, "", issues[0].Assignee)

	assert.Equal(t, "#2", issues[1].ID)
	assert.Equal(t, "bob", issues[1].Assignee)
}
