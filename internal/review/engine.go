package review

import "github.com/donovan-yohan/belayer/internal/model"

type ReactionEventType string

const (
	EventCIFailed         ReactionEventType = "ci_failed"
	EventCIPassed         ReactionEventType = "ci_passed"
	EventNewComment       ReactionEventType = "new_comment"
	EventChangesRequested ReactionEventType = "changes_requested"
	EventApproved         ReactionEventType = "approved"
	EventMerged           ReactionEventType = "merged"
	EventClosed           ReactionEventType = "closed"
)

type ReactionEvent struct {
	Type    ReactionEventType
	Details any
}

func ClassifyActivity(prev, curr *model.PRStatus, activity *model.PRActivity) []ReactionEvent {
	var events []ReactionEvent

	// CI transitions
	if prev.CIStatus == "passing" && curr.CIStatus == "failing" {
		events = append(events, ReactionEvent{Type: EventCIFailed})
	}
	if prev.CIStatus == "failing" && curr.CIStatus == "passing" {
		events = append(events, ReactionEvent{Type: EventCIPassed})
	}

	// State transitions
	if curr.State == "merged" && prev.State != "merged" {
		events = append(events, ReactionEvent{Type: EventMerged})
	}
	if curr.State == "closed" && prev.State != "closed" {
		events = append(events, ReactionEvent{Type: EventClosed})
	}

	// Review state changes
	prevReviewState := HighestReviewState(prev.Reviews)
	currReviewState := HighestReviewState(curr.Reviews)
	if currReviewState == "approved" && prevReviewState != "approved" {
		events = append(events, ReactionEvent{Type: EventApproved})
	}
	if currReviewState == "changes_requested" && prevReviewState != "changes_requested" {
		events = append(events, ReactionEvent{Type: EventChangesRequested})
	}

	// New comments
	if activity != nil {
		for _, c := range activity.Comments {
			events = append(events, ReactionEvent{Type: EventNewComment, Details: c})
		}
	}

	return events
}

// HighestReviewState returns the most significant review state.
// Priority: changes_requested > approved > commented > ""
func HighestReviewState(reviews []model.Review) string {
	state := ""
	for _, r := range reviews {
		switch r.State {
		case "changes_requested":
			return "changes_requested"
		case "approved":
			state = "approved"
		case "commented":
			if state == "" {
				state = "commented"
			}
		}
	}
	return state
}
