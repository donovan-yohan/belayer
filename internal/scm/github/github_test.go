package github

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

func TestRunInDir_StderrInExitError(t *testing.T) {
	_, err := runInDir(context.Background(), t.TempDir(), "sh", "-c", "echo 'stderr msg' >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from failing command")
	}
	// stderr should be extractable via stderrFromError
	stderr := stderrFromError(err)
	if !strings.Contains(stderr, "stderr msg") {
		t.Errorf("expected stderr to contain 'stderr msg', got: %q", stderr)
	}
}

func TestRunInDir_StdoutCleanOnSuccess(t *testing.T) {
	out, err := runInDir(context.Background(), t.TempDir(), "sh", "-c", "echo 'stdout only'; echo 'warn' >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// stdout should not contain stderr
	if strings.Contains(string(out), "warn") {
		t.Errorf("stdout should not contain stderr, got: %q", string(out))
	}
	if !strings.Contains(string(out), "stdout only") {
		t.Errorf("stdout should contain 'stdout only', got: %q", string(out))
	}
}

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
		if got.State != model.PRStateOpen {
			t.Errorf("State: got %q, want %q", got.State, model.PRStateOpen)
		}
		if got.CIStatus != model.CIStatusFailing {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, model.CIStatusFailing)
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
		if got.CIStatus != model.CIStatusPassing {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, model.CIStatusPassing)
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
		if got.CIStatus != model.CIStatusPending {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, model.CIStatusPending)
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
		if got.CIStatus != model.CIStatusPending {
			t.Errorf("CIStatus: got %q, want %q", got.CIStatus, model.CIStatusPending)
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
		} else if got.Reviews[0].State != model.ReviewStateChangesRequested {
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
