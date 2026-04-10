package broker

import "time"

// MessageType classifies the intent of a message.
type MessageType string

const (
	MessageInstruction MessageType = "instruction"
	MessageInputNeeded MessageType = "input-needed"
	MessageStateChange MessageType = "state-change"
)

// Message is a single inter-agent message.
type Message struct {
	ID          string      `json:"id"`
	SessionID   string      `json:"session_id"`
	SenderID    string      `json:"sender_id"`
	RecipientID string      `json:"recipient_id"` // empty for broadcast
	Type        MessageType `json:"type"`
	Content     string      `json:"content"`
	Urgent      bool        `json:"urgent"`
	Timestamp   time.Time   `json:"timestamp"`
}

// Handler is a callback invoked when a message is delivered to a subscriber.
type Handler func(msg Message)

// Broker routes messages between agents within a session.
type Broker interface {
	// Send delivers msg to the specific agent identified by agentID within sessionID.
	// Non-urgent messages are debounced; urgent messages are delivered immediately.
	Send(sessionID, agentID string, msg Message) error

	// Broadcast delivers msg to every subscriber in sessionID except the sender.
	Broadcast(sessionID string, msg Message) error

	// Subscribe registers handler to receive messages addressed to agentID in sessionID.
	Subscribe(sessionID, agentID string, handler Handler) error

	// Unsubscribe removes the handler for agentID in sessionID.
	Unsubscribe(sessionID, agentID string) error

	// Interrupt sends an interrupt signal (Ctrl+C) followed by msg to the agent.
	Interrupt(sessionID, agentID string, msg Message) error
}
