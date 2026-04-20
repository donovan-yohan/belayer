package broker

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/donovan-yohan/belayer/internal/store"
)

// agentKey returns the map key used to index a session+agent pair.
func agentKey(sessionID, agentID string) string {
	return sessionID + "/" + agentID
}

// MemoryBroker is an in-process Broker implementation with optional persistence.
type MemoryBroker struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]Handler // sessionID -> agentID -> handler
	st          *store.Store                  // may be nil
	debounce    map[string]*debouncer         // agentKey -> debouncer
	window      time.Duration                // debounce window, defaults to defaultDebounceWindow
}

// NewMemoryBroker creates a MemoryBroker. st may be nil (disables persistence).
func NewMemoryBroker(st *store.Store) *MemoryBroker {
	return &MemoryBroker{
		subscribers: make(map[string]map[string]Handler),
		st:          st,
		debounce:    make(map[string]*debouncer),
		window:      defaultDebounceWindow,
	}
}

// newMemoryBrokerWithWindow creates a MemoryBroker with a custom debounce window (for tests).
func newMemoryBrokerWithWindow(st *store.Store, window time.Duration) *MemoryBroker {
	b := NewMemoryBroker(st)
	b.window = window
	return b
}

// Subscribe registers handler to receive messages for agentID in sessionID.
func (b *MemoryBroker) Subscribe(sessionID, agentID string, handler Handler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[sessionID]; !ok {
		b.subscribers[sessionID] = make(map[string]Handler)
	}
	b.subscribers[sessionID][agentID] = handler
	return nil
}

// Unsubscribe removes the handler for agentID in sessionID.
func (b *MemoryBroker) Unsubscribe(sessionID, agentID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if agents, ok := b.subscribers[sessionID]; ok {
		delete(agents, agentID)
		if len(agents) == 0 {
			delete(b.subscribers, sessionID)
		}
	}

	// Clean up any pending debouncer for this agent.
	key := agentKey(sessionID, agentID)
	if d, ok := b.debounce[key]; ok {
		d.flushNow()
		delete(b.debounce, key)
	}
	return nil
}

// Send delivers msg to a specific agent. Non-urgent messages are debounced;
// urgent messages flush the buffer and deliver immediately.
func (b *MemoryBroker) Send(sessionID, agentID string, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("broker: refusing to send message with empty content (sender=%q recipient=%q)", msg.SenderID, agentID)
	}

	handler := b.lookupHandler(sessionID, agentID)
	if handler == nil {
		return fmt.Errorf("broker: no subscriber for agent %q in session %q", agentID, sessionID)
	}

	key := agentKey(sessionID, agentID)

	if msg.Urgent {
		// Flush any buffered non-urgent content first, then deliver immediately.
		b.mu.Lock()
		d := b.debounce[key]
		b.mu.Unlock()
		if d != nil {
			d.flushNow()
		}
		handler(msg)
		b.logEvent(msg)
		return nil
	}

	// Non-urgent: feed through the debouncer.
	// We need to get-or-create the debouncer under the write lock.
	b.mu.Lock()
	d, ok := b.debounce[key]
	if !ok {
		capturedSessionID := sessionID
		capturedAgentID := agentID
		// Snapshot metadata from this first message; content replaced on flush.
		baseMeta := msg
		d = newDebouncer(b.window, func(coalesced string) {
			h := b.lookupHandler(capturedSessionID, capturedAgentID)
			if h == nil {
				return
			}
			out := baseMeta
			out.Content = coalesced
			h(out)
			b.logEvent(out)
		})
		b.debounce[key] = d
	}
	b.mu.Unlock()

	d.add(msg.Content)
	return nil
}

// Broadcast delivers msg to all subscribers in sessionID except the sender.
func (b *MemoryBroker) Broadcast(sessionID string, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("broker: refusing to broadcast message with empty content (sender=%q recipient=%q)", msg.SenderID, "<broadcast>")
	}

	b.mu.RLock()
	agents := b.subscribers[sessionID]
	// Copy handlers to avoid holding the lock during delivery.
	targets := make(map[string]Handler, len(agents))
	for id, h := range agents {
		if id != msg.SenderID {
			targets[id] = h
		}
	}
	b.mu.RUnlock()

	for agentID, handler := range targets {
		out := msg
		out.RecipientID = agentID
		handler(out)
		b.logEvent(out)
	}
	return nil
}

// Interrupt flushes any pending debounced content, then delivers msg immediately —
// bypassing debounce entirely. The Ctrl+C convention was tmux-specific and is not
// used for bridge agents, which receive interrupts via stdin directly.
func (b *MemoryBroker) Interrupt(sessionID, agentID string, msg Message) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("broker: refusing to interrupt with empty content (sender=%q recipient=%q)", msg.SenderID, agentID)
	}

	handler := b.lookupHandler(sessionID, agentID)
	if handler == nil {
		return fmt.Errorf("broker: no subscriber for agent %q in session %q", agentID, sessionID)
	}

	// Flush any pending debounced content first.
	key := agentKey(sessionID, agentID)
	b.mu.Lock()
	d := b.debounce[key]
	b.mu.Unlock()
	if d != nil {
		d.flushNow()
	}

	// Deliver the message directly.
	msg.RecipientID = agentID
	handler(msg)
	b.logEvent(msg)
	return nil
}

// lookupHandler returns the handler for agentID in sessionID, or nil if not found.
func (b *MemoryBroker) lookupHandler(sessionID, agentID string) Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if agents, ok := b.subscribers[sessionID]; ok {
		return agents[agentID]
	}
	return nil
}

// logEvent persists a message_delivered event to the store if one is configured.
func (b *MemoryBroker) logEvent(msg Message) {
	if b.st == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = b.st.LogEvent(store.SessionEvent{
		SessionID: msg.SessionID,
		Type:      "message_delivered",
		Data:      string(data),
	})
}
