package intake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jiraSearchPayload builds a minimal Jira search API response body.
func jiraSearchPayload(t *testing.T, issues []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"issues": issues})
	require.NoError(t, err)
	return body
}

func TestJiraAdapter_Poll_Success(t *testing.T) {
	issues := []map[string]any{
		{
			"key": "FOX-1234",
			"fields": map[string]any{
				"summary": "Fix the login bug",
				"description": map[string]any{
					"type": "doc",
					"content": []map[string]any{
						{
							"type": "paragraph",
							"content": []map[string]any{
								{"type": "text", "text": "Users cannot log in after password reset."},
							},
						},
					},
				},
			},
		},
		{
			"key": "FOX-5678",
			"fields": map[string]any{
				"summary":     "Add dark mode",
				"description": nil,
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/search", r.URL.Path)
		assert.NotEmpty(t, r.URL.Query().Get("jql"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jiraSearchPayload(t, issues))
	}))
	defer srv.Close()

	t.Setenv("JIRA_TEST_TOKEN", "secret-token")

	cfg := pipeline.IntakeConfig{
		Type: "jira",
		Config: map[string]string{
			"project":        "FOX",
			"credential_env": "JIRA_TEST_TOKEN",
			"jira_url":       srv.URL,
			"jira_email":     "user@example.com",
		},
	}

	adapter, err := NewJiraAdapter(cfg, "my-pipeline")
	require.NoError(t, err)

	specs, err := adapter.Poll(t.Context())
	require.NoError(t, err)
	require.Len(t, specs, 2)

	assert.Equal(t, "FOX-1234", specs[0].ExternalID)
	assert.Equal(t, "jira", specs[0].Source)
	assert.Equal(t, "my-pipeline", specs[0].PipelineName)
	assert.Contains(t, specs[0].Spec, "Fix the login bug")
	assert.Contains(t, specs[0].Spec, "Users cannot log in after password reset.")

	assert.Equal(t, "FOX-5678", specs[1].ExternalID)
	assert.Equal(t, "jira", specs[1].Source)
	assert.Equal(t, "my-pipeline", specs[1].PipelineName)
	assert.Equal(t, "Add dark mode", specs[1].Spec)
}

func TestJiraAdapter_Poll_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jiraSearchPayload(t, []map[string]any{}))
	}))
	defer srv.Close()

	t.Setenv("JIRA_TEST_TOKEN", "secret-token")

	cfg := pipeline.IntakeConfig{
		Type: "jira",
		Config: map[string]string{
			"project":        "FOX",
			"credential_env": "JIRA_TEST_TOKEN",
			"jira_url":       srv.URL,
			"jira_email":     "user@example.com",
		},
	}

	adapter, err := NewJiraAdapter(cfg, "my-pipeline")
	require.NoError(t, err)

	specs, err := adapter.Poll(t.Context())
	require.NoError(t, err)
	assert.Empty(t, specs)
}

func TestJiraAdapter_Poll_AuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()

	t.Setenv("JIRA_TEST_TOKEN", "bad-token")

	cfg := pipeline.IntakeConfig{
		Type: "jira",
		Config: map[string]string{
			"project":        "FOX",
			"credential_env": "JIRA_TEST_TOKEN",
			"jira_url":       srv.URL,
			"jira_email":     "user@example.com",
		},
	}

	adapter, err := NewJiraAdapter(cfg, "my-pipeline")
	require.NoError(t, err)

	specs, err := adapter.Poll(t.Context())
	require.Error(t, err)
	assert.Nil(t, specs)
	assert.Contains(t, err.Error(), "401")
}

func TestJiraAdapter_Poll_MissingConfig(t *testing.T) {
	t.Setenv("JIRA_TEST_TOKEN", "some-token")

	cfg := pipeline.IntakeConfig{
		Type: "jira",
		Config: map[string]string{
			// "project" intentionally omitted
			"credential_env": "JIRA_TEST_TOKEN",
		},
	}

	adapter, err := NewJiraAdapter(cfg, "my-pipeline")
	require.Error(t, err)
	assert.Nil(t, adapter)
	assert.Contains(t, err.Error(), "project")
}
