package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/donovan-yohan/belayer/internal/pipeline"
)

// JiraAdapter polls a Jira project via the REST API v3 and produces SubmitSpecs.
type JiraAdapter struct {
	baseURL       string
	email         string
	token         string
	filter        string
	project       string
	pipelineName  string
	httpClient    *http.Client
}

// NewJiraAdapter constructs a JiraAdapter from an IntakeConfig.
// Required config keys: project, credential_env, jira_url.
// Optional config keys: filter, jira_email.
func NewJiraAdapter(cfg pipeline.IntakeConfig, pipelineName string) (*JiraAdapter, error) {
	project := cfg.Config["project"]
	if project == "" {
		return nil, fmt.Errorf("jira intake: missing required config key \"project\"")
	}

	credEnv := cfg.Config["credential_env"]
	if credEnv == "" {
		return nil, fmt.Errorf("jira intake: missing required config key \"credential_env\"")
	}

	token := os.Getenv(credEnv)
	if token == "" {
		return nil, fmt.Errorf("jira intake: credential env var %q is not set or empty", credEnv)
	}

	baseURL := cfg.Config["jira_url"]
	if baseURL == "" {
		return nil, fmt.Errorf("jira intake: missing required config key \"jira_url\" (e.g., \"https://yourcompany.atlassian.net\")")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	email := cfg.Config["jira_email"]

	filter := cfg.Config["filter"]
	if filter == "" {
		filter = fmt.Sprintf("project = %s ORDER BY created DESC", project)
	}

	return &JiraAdapter{
		baseURL:      baseURL,
		email:        email,
		token:        token,
		filter:       filter,
		project:      project,
		pipelineName: pipelineName,
		httpClient:   &http.Client{},
	}, nil
}

// jiraSearchResponse is the shape of GET /rest/api/3/search.
type jiraSearchResponse struct {
	Issues []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary     string           `json:"summary"`
	Description *jiraDescription `json:"description"`
}

// jiraDescription handles Atlassian Document Format (ADF) or plain string.
// We extract plain text from the top-level "text" nodes when present.
type jiraDescription struct {
	Type    string            `json:"type"`
	Content []jiraDocContent  `json:"content"`
}

type jiraDocContent struct {
	Type    string           `json:"type"`
	Text    string           `json:"text,omitempty"`
	Content []jiraDocContent `json:"content,omitempty"`
}

// extractText recursively extracts plain text from Atlassian Document Format content.
func extractText(contents []jiraDocContent) string {
	var b strings.Builder
	for _, c := range contents {
		if c.Type == "text" {
			b.WriteString(c.Text)
		} else if len(c.Content) > 0 {
			b.WriteString(extractText(c.Content))
		}
		if c.Type == "paragraph" || c.Type == "heading" {
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// Poll fetches issues from Jira matching the configured JQL filter and returns
// them as SubmitSpecs. An empty result is not an error.
func (a *JiraAdapter) Poll(ctx context.Context) ([]SubmitSpec, error) {
	searchURL := fmt.Sprintf("%s/rest/api/3/search?jql=%s", a.baseURL, url.QueryEscape(a.filter))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jira poll: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if a.email != "" {
		req.SetBasicAuth(a.email, a.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira poll: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("jira poll: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira poll: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result jiraSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("jira poll: parse response: %w", err)
	}

	specs := make([]SubmitSpec, 0, len(result.Issues))
	for _, issue := range result.Issues {
		desc := ""
		if issue.Fields.Description != nil {
			desc = extractText(issue.Fields.Description.Content)
		}

		spec := issue.Fields.Summary
		if desc != "" {
			spec = issue.Fields.Summary + "\n\n" + desc
		}

		specs = append(specs, SubmitSpec{
			Spec:         spec,
			Source:       "jira",
			ExternalID:   issue.Key,
			PipelineName: a.pipelineName,
		})
	}

	return specs, nil
}
