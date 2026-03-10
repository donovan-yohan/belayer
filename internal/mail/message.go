// internal/mail/message.go
package mail

import (
	"fmt"
	"strings"
)

// MessageType identifies the kind of mail message.
type MessageType string

const (
	MessageTypeGoalAssignment MessageType = "goal_assignment"
	MessageTypeDone           MessageType = "done"
	MessageTypeSpotResult     MessageType = "spot_result"
	MessageTypeVerdict        MessageType = "verdict"
	MessageTypeFeedback       MessageType = "feedback"
	MessageTypeInstruction    MessageType = "instruction"
)

var validTypes = map[MessageType]bool{
	MessageTypeGoalAssignment: true,
	MessageTypeDone:           true,
	MessageTypeSpotResult:     true,
	MessageTypeVerdict:        true,
	MessageTypeFeedback:       true,
	MessageTypeInstruction:    true,
}

// Valid returns true if the message type is recognized.
func (mt MessageType) Valid() bool {
	return validTypes[mt]
}

// Address identifies a mail recipient. Deterministically maps to a tmux target.
type Address struct {
	Role   string // "setter", "lead", "spotter", "anchor"
	TaskID string // empty for setter
	Repo   string // empty for setter and anchor
	GoalID string // empty for setter and anchor
}

// ParseAddress parses a path-like address string.
// Valid formats:
//   - "setter"
//   - "task/<id>/lead/<repo>/<goal>"
//   - "task/<id>/spotter/<repo>/<goal>"
//   - "task/<id>/anchor"
func ParseAddress(s string) (Address, error) {
	if s == "" {
		return Address{}, fmt.Errorf("empty address")
	}
	if s == "setter" {
		return Address{Role: "setter"}, nil
	}

	parts := strings.Split(s, "/")
	if len(parts) < 3 || parts[0] != "task" {
		return Address{}, fmt.Errorf("invalid address format: %q", s)
	}

	taskID := parts[1]
	role := parts[2]

	switch role {
	case "lead", "spotter":
		if len(parts) != 5 {
			return Address{}, fmt.Errorf("invalid %s address: expected task/<id>/%s/<repo>/<goal>, got %q", role, role, s)
		}
		return Address{Role: role, TaskID: taskID, Repo: parts[3], GoalID: parts[4]}, nil
	case "anchor":
		if len(parts) != 3 {
			return Address{}, fmt.Errorf("invalid anchor address: expected task/<id>/anchor, got %q", s)
		}
		return Address{Role: role, TaskID: taskID}, nil
	default:
		return Address{}, fmt.Errorf("unknown role %q in address %q", role, s)
	}
}

// String returns the canonical address string.
func (a Address) String() string {
	switch a.Role {
	case "setter":
		return "setter"
	case "anchor":
		return fmt.Sprintf("task/%s/anchor", a.TaskID)
	default:
		return fmt.Sprintf("task/%s/%s/%s/%s", a.TaskID, a.Role, a.Repo, a.GoalID)
	}
}

// TmuxTarget returns the tmux session and window name for this address.
func (a Address) TmuxTarget() (session, window string) {
	switch a.Role {
	case "setter":
		return "belayer-setter", "0"
	case "anchor":
		return fmt.Sprintf("belayer-task-%s", a.TaskID), "anchor"
	default:
		return fmt.Sprintf("belayer-task-%s", a.TaskID), fmt.Sprintf("%s-%s", a.Repo, a.GoalID)
	}
}

// Message is the in-memory representation of a mail message.
type Message struct {
	ID      string      `json:"id"`
	From    string      `json:"from"`
	To      string      `json:"to"`
	Type    MessageType `json:"type"`
	Subject string      `json:"subject"`
	Body    string      `json:"body"`
}
