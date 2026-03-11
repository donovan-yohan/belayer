package review

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/model"
)

func prStatus(state, ciStatus string, reviews []model.Review) *model.PRStatus {
	return &model.PRStatus{State: state, CIStatus: ciStatus, Reviews: reviews}
}

func TestClassifyActivity_CIFailure(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("open", "failing", nil)
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventCIFailed {
		t.Errorf("expected EventCIFailed, got %v", events)
	}
}

func TestClassifyActivity_CIPassed(t *testing.T) {
	prev := prStatus("open", "failing", nil)
	curr := prStatus("open", "passing", nil)
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventCIPassed {
		t.Errorf("expected EventCIPassed, got %v", events)
	}
}

func TestClassifyActivity_Approved(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("open", "passing", []model.Review{{Author: "alice", State: "approved"}})
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventApproved {
		t.Errorf("expected EventApproved, got %v", events)
	}
}

func TestClassifyActivity_ChangesRequested(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("open", "passing", []model.Review{{Author: "bob", State: "changes_requested"}})
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventChangesRequested {
		t.Errorf("expected EventChangesRequested, got %v", events)
	}
}

func TestClassifyActivity_Merged(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("merged", "passing", nil)
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventMerged {
		t.Errorf("expected EventMerged, got %v", events)
	}
}

func TestClassifyActivity_Closed(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("closed", "passing", nil)
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 1 || events[0].Type != EventClosed {
		t.Errorf("expected EventClosed, got %v", events)
	}
}

func TestClassifyActivity_NewComments(t *testing.T) {
	prev := prStatus("open", "passing", nil)
	curr := prStatus("open", "passing", nil)
	activity := &model.PRActivity{
		Comments: []model.ReviewComment{
			{ID: 1, Author: "carol", Body: "looks good"},
			{ID: 2, Author: "dave", Body: "fix this"},
		},
	}
	events := ClassifyActivity(prev, curr, activity)
	if len(events) != 2 {
		t.Fatalf("expected 2 comment events, got %d", len(events))
	}
	for _, e := range events {
		if e.Type != EventNewComment {
			t.Errorf("expected EventNewComment, got %v", e.Type)
		}
	}
}

func TestClassifyActivity_NoChanges(t *testing.T) {
	reviews := []model.Review{{Author: "alice", State: "approved"}}
	prev := prStatus("open", "passing", reviews)
	curr := prStatus("open", "passing", reviews)
	events := ClassifyActivity(prev, curr, nil)
	if len(events) != 0 {
		t.Errorf("expected no events, got %v", events)
	}
}

func TestHighestReviewState(t *testing.T) {
	tests := []struct {
		reviews []model.Review
		want    string
	}{
		{nil, ""},
		{[]model.Review{{State: "commented"}}, "commented"},
		{[]model.Review{{State: "approved"}}, "approved"},
		{[]model.Review{{State: "changes_requested"}}, "changes_requested"},
		{[]model.Review{{State: "approved"}, {State: "commented"}}, "approved"},
		{[]model.Review{{State: "approved"}, {State: "changes_requested"}}, "changes_requested"},
		{[]model.Review{{State: "commented"}, {State: "approved"}}, "approved"},
	}
	for _, tt := range tests {
		got := HighestReviewState(tt.reviews)
		if got != tt.want {
			t.Errorf("HighestReviewState(%v) = %q, want %q", tt.reviews, got, tt.want)
		}
	}
}
