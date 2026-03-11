package model

type Issue struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Comments     []Comment      `json:"comments"`
	Labels       []string       `json:"labels"`
	Priority     string         `json:"priority"`
	Assignee     string         `json:"assignee"`
	LinkedIssues []LinkedIssue  `json:"linked_issues"`
	URL          string         `json:"url"`
	Raw          map[string]any `json:"raw"`
}

type Comment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Date   string `json:"date"`
}

type LinkedIssue struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type IssueFilter struct {
	Labels   []string `json:"labels"`
	Assignee string   `json:"assignee"`
	Sprint   string   `json:"sprint"`
	Status   []string `json:"status"`
}
