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

func TestShouldDispatchCIFix(t *testing.T) {
	tests := []struct {
		name     string
		fixCount int
		maxFixes int
		want     bool
	}{
		{"first attempt", 0, 2, true},
		{"second attempt", 1, 2, true},
		{"exhausted", 2, 2, false},
		{"over cap", 3, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldDispatchCIFix(tt.fixCount, tt.maxFixes)
			if got != tt.want {
				t.Errorf("ShouldDispatchCIFix(%d, %d) = %v, want %v", tt.fixCount, tt.maxFixes, got, tt.want)
			}
		})
	}
}

func pr(ciFixCount int) *model.PullRequest {
	return &model.PullRequest{CIFixCount: ciFixCount}
}

func TestDecideReaction(t *testing.T) {
	tests := []struct {
		name           string
		event          ReactionEvent
		pr             *model.PullRequest
		maxFixAttempts int
		autoMerge      bool
		want           string
	}{
		{"ci_failure under cap", ReactionEvent{Type: EventCIFailed}, pr(0), 2, false, "lead_dispatched"},
		{"ci_failure at cap", ReactionEvent{Type: EventCIFailed}, pr(2), 2, false, "marked_stuck"},
		{"ci_passed", ReactionEvent{Type: EventCIPassed}, pr(0), 2, false, "recorded"},
		{"new_comment", ReactionEvent{Type: EventNewComment}, pr(0), 2, false, "comment_replied"},
		{"changes_requested", ReactionEvent{Type: EventChangesRequested}, pr(0), 2, false, "lead_dispatched"},
		{"approved with auto_merge", ReactionEvent{Type: EventApproved}, pr(0), 2, true, "merge_attempted"},
		{"approved without auto_merge", ReactionEvent{Type: EventApproved}, pr(0), 2, false, "recorded"},
		{"merged", ReactionEvent{Type: EventMerged}, pr(0), 2, false, "status_merged"},
		{"closed", ReactionEvent{Type: EventClosed}, pr(0), 2, false, "status_closed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideReaction(tt.event, tt.pr, tt.maxFixAttempts, tt.autoMerge)
			if got != tt.want {
				t.Errorf("DecideReaction() = %q, want %q", got, tt.want)
			}
		})
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
