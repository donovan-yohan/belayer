package github

import (
	"testing"
	"time"
)

func TestParseGHPRStatusJSON(t *testing.T) {
	t.Run("failing check dominates", func(t *testing.T) {
		data := []byte(`{
			"number": 42,
			"state": "OPEN",
			"url": "https://github.com/owner/repo/pull/42",
			"mergeable": "MERGEABLE",
			"statusCheckRollup": [
				{"name": "ci/build", "status": "COMPLETED", "conclusion": "SUCCESS"},
				{"name": "ci/lint",  "status": "COMPLETED", "conclusion": "FAILURE"}
			],
			"reviews": [
				{"author": {"login": "alice"}, "state": "APPROVED", "body": "lgtm"}
			]
		}`)

		got, err := parseGHPRStatusJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Number != 42 {
			t.Errorf("Number: got %d, want 42", got.Number)
		}
		if got.State != "open" {
			t.Errorf("State: got %q, want %q", got.State, "open")
		}
		if got.CIStatus != "failing" {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "failing")
		}
		if !got.Mergeable {
			t.Error("expected Mergeable=true")
		}
		if len(got.Reviews) != 1 || got.Reviews[0].Author != "alice" {
			t.Errorf("unexpected reviews: %+v", got.Reviews)
		}
	})

	t.Run("all passing", func(t *testing.T) {
		data := []byte(`{
			"number": 7,
			"state": "OPEN",
			"url": "https://github.com/owner/repo/pull/7",
			"mergeable": "MERGEABLE",
			"statusCheckRollup": [
				{"name": "ci/build", "status": "COMPLETED", "conclusion": "SUCCESS"},
				{"name": "ci/test",  "status": "COMPLETED", "conclusion": "SUCCESS"}
			],
			"reviews": []
		}`)

		got, err := parseGHPRStatusJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CIStatus != "passing" {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "passing")
		}
	})

	t.Run("in progress check gives pending", func(t *testing.T) {
		data := []byte(`{
			"number": 3,
			"state": "OPEN",
			"url": "https://github.com/owner/repo/pull/3",
			"mergeable": "CONFLICTING",
			"statusCheckRollup": [
				{"name": "ci/build", "status": "IN_PROGRESS", "conclusion": ""}
			],
			"reviews": []
		}`)

		got, err := parseGHPRStatusJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CIStatus != "pending" {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "pending")
		}
		if got.Mergeable {
			t.Error("expected Mergeable=false for CONFLICTING")
		}
	})

	t.Run("no checks gives pending", func(t *testing.T) {
		data := []byte(`{
			"number": 1,
			"state": "OPEN",
			"url": "https://github.com/owner/repo/pull/1",
			"mergeable": "MERGEABLE",
			"statusCheckRollup": [],
			"reviews": []
		}`)

		got, err := parseGHPRStatusJSON(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CIStatus != "pending" {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, "pending")
		}
	})
}

func TestParseGHPRActivityJSON(t *testing.T) {
	since := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	t.Run("filters comments by since, includes all reviews", func(t *testing.T) {
		commentsData := []byte(`[
			{"id": 1, "user": {"login": "bob"}, "body": "old comment", "created_at": "2024-01-09T12:00:00Z"},
			{"id": 2, "user": {"login": "alice"}, "body": "new comment", "created_at": "2024-01-11T08:00:00Z"}
		]`)
		reviewsData := []byte(`[
			{"author": {"login": "carol"}, "state": "CHANGES_REQUESTED", "body": "needs work"}
		]`)

		got, err := parseGHPRActivityJSON(commentsData, reviewsData, since)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Comments) != 1 {
			t.Errorf("Comments: got %d, want 1", len(got.Comments))
		} else {
			if got.Comments[0].ID != 2 || got.Comments[0].Author != "alice" {
				t.Errorf("unexpected comment: %+v", got.Comments[0])
			}
		}
		if len(got.Reviews) != 1 {
			t.Errorf("Reviews: got %d, want 1", len(got.Reviews))
		} else if got.Reviews[0].State != "CHANGES_REQUESTED" {
			t.Errorf("Review state: got %q", got.Reviews[0].State)
		}
	})

	t.Run("empty comments and reviews", func(t *testing.T) {
		got, err := parseGHPRActivityJSON([]byte(`[]`), []byte(`[]`), since)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Comments) != 0 || len(got.Reviews) != 0 {
			t.Errorf("expected empty activity, got %+v", got)
		}
	})
}
